package dispatcher

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/dispatcherapi"
)

// The Factory settings and the connections. Each member of the
// Organization reads them; only an Admin saves them. Factory settings are
// keyed by the SolidTrace Project id, which the Dispatcher asks SolidTrace
// for at each call, so they follow a Project whose slug changes.

// listSettings gives the Factory settings of each Project of the
// Organization, with the slugs that SolidTrace gives now.
func (d *Dispatcher) listSettings(w http.ResponseWriter, r *http.Request, p person) {
	projects, err := d.listProjects(r.Context(), p.organization)
	if err != nil {
		d.solidTraceFailure(w, p.organization, err)
		return
	}
	all, err := d.store.allSettings(p.organization.id)
	if err != nil {
		d.serverError(w, "list the Factory settings", err)
		return
	}
	if _, err := d.markOrphans(p.organization, projects, all); err != nil {
		d.serverError(w, "mark the Factory settings orphaned", err)
		return
	}
	list := dispatcherapi.ProjectList{Projects: []dispatcherapi.FactorySettings{}}
	for _, s := range all {
		if slug, ok := slugOf(projects, s.projectID); ok && slug != s.Project {
			if err := d.store.followSlug(s.projectID, slug); err != nil {
				d.serverError(w, "list the Factory settings", err)
				return
			}
			s.Project = slug
		}
		list.Projects = append(list.Projects, s.FactorySettings)
	}
	writeJSON(w, http.StatusOK, list)
}

func (d *Dispatcher) getSettings(w http.ResponseWriter, r *http.Request, p person) {
	project, ok := d.project(w, r, p)
	if !ok {
		return
	}
	settings, err := d.store.settings(p.organization.id, project.id)
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusNotFound, noSettings(project.slug))
		return
	}
	if err != nil {
		d.serverError(w, "read the Factory settings", err)
		return
	}
	settings.Project = project.slug
	writeJSON(w, http.StatusOK, settings.FactorySettings)
}

func (d *Dispatcher) saveSettings(w http.ResponseWriter, r *http.Request, p person) {
	var save dispatcherapi.SaveFactorySettings
	if !decode(w, r, &save, "the body must be a JSON object with repository, folder, base_branch and connection") {
		return
	}
	if problem := cleanSettings(&save); problem != "" {
		writeProblem(w, http.StatusBadRequest, problem)
		return
	}
	project, ok := d.project(w, r, p)
	if !ok {
		return
	}
	var connectionID string
	if save.Connection != "" {
		id, err := d.store.connectionID(p.organization.id, save.Connection)
		if errors.Is(err, errNotFound) {
			writeProblem(w, http.StatusUnprocessableEntity, "no connection "+save.Connection+" in the Organization "+
				p.organization.slug+": 'solidtrace connections list' lists them")
			return
		}
		if err != nil {
			d.serverError(w, "save the Factory settings", err)
			return
		}
		connectionID = id
	}
	routes, ok := d.routes(w, p, save.Routes)
	if !ok {
		return
	}
	err := d.store.saveSettings(p.organization.id, project.id, project.slug, save, connectionID, routes, p, d.now().UTC())
	var taken *routeTaken
	if errors.As(err, &taken) {
		writeProblem(w, http.StatusConflict, taken.Error())
		return
	}
	if err != nil {
		d.serverError(w, "save the Factory settings", err)
		return
	}
	settings, err := d.store.settings(p.organization.id, project.id)
	if err != nil {
		d.serverError(w, "read the Factory settings", err)
		return
	}
	writeJSON(w, http.StatusOK, settings.FactorySettings)
}

// project finds the Project of the request's {project} slug at
// SolidTrace. It answers the request itself when it finds none.
func (d *Dispatcher) project(w http.ResponseWriter, r *http.Request, p person) (solidTraceProject, bool) {
	return d.projectBySlug(w, r, p, r.PathValue("project"))
}

// projectBySlug finds the Project with slug at SolidTrace. It answers the
// request itself when it finds none.
func (d *Dispatcher) projectBySlug(w http.ResponseWriter, r *http.Request, p person, slug string) (solidTraceProject, bool) {
	projects, err := d.listProjects(r.Context(), p.organization)
	if err != nil {
		d.solidTraceFailure(w, p.organization, err)
		return solidTraceProject{}, false
	}
	i := slices.IndexFunc(projects, func(sp solidTraceProject) bool { return sp.slug == slug })
	if i < 0 {
		writeProblem(w, http.StatusNotFound, noProject(slug, p.organization.slug))
		return solidTraceProject{}, false
	}
	if err := d.store.followSlug(projects[i].id, slug); err != nil {
		d.serverError(w, "read the Factory settings", err)
		return solidTraceProject{}, false
	}
	return projects[i], true
}

// triggerSettings finds the Factory settings of the Project that a
// trigger names. It refuses the trigger, with a warning, for a Project
// that SolidTrace does not have or that has no Factory settings, and with
// 403 for a Project that is not in the starter's list. The settings of
// each Project that SolidTrace no longer has are marked orphaned. It
// answers the request itself when it refuses.
func (d *Dispatcher) triggerSettings(w http.ResponseWriter, r *http.Request, s starter, slug string) (storedSettings, bool) {
	projects, err := d.listProjects(r.Context(), s.organization)
	if err != nil {
		d.solidTraceFailure(w, s.organization, err)
		return storedSettings{}, false
	}
	all, err := d.store.allSettings(s.organization.id)
	if err != nil {
		d.serverError(w, "read the Factory settings", err)
		return storedSettings{}, false
	}
	marked, err := d.markOrphans(s.organization, projects, all)
	if err != nil {
		d.serverError(w, "mark the Factory settings orphaned", err)
		return storedSettings{}, false
	}
	i := slices.IndexFunc(projects, func(sp solidTraceProject) bool { return sp.slug == slug })
	if i < 0 {
		d.warn("a trigger names a Project that SolidTrace does not have: the Run is refused",
			s.warnArgs(slug)...)
		message := noProject(slug, s.organization.slug) + ": the Run is refused"
		if slices.Contains(marked, slug) {
			message += ", and its Factory settings are marked orphaned"
		}
		writeProblem(w, http.StatusUnprocessableEntity, message)
		return storedSettings{}, false
	}
	if s.projects != nil && !slices.Contains(s.projects, projects[i].id) {
		writeProblem(w, http.StatusForbidden, "the "+s.kind+" "+s.name+" cannot start Runs in the Project "+slug)
		return storedSettings{}, false
	}
	settings, err := d.store.settings(s.organization.id, projects[i].id)
	if errors.Is(err, errNotFound) {
		d.warn("a trigger names a Project that has no Factory settings: the Run is refused",
			s.warnArgs(slug)...)
		writeProblem(w, http.StatusUnprocessableEntity, noSettings(slug)+": the Run is refused")
		return storedSettings{}, false
	}
	if err == nil {
		err = d.store.followSlug(settings.projectID, slug)
	}
	if err != nil {
		d.serverError(w, "read the Factory settings", err)
		return storedSettings{}, false
	}
	settings.Project = slug
	return settings, true
}

// markOrphans marks the Factory settings of each Project that SolidTrace
// no longer has as orphaned, with a warning Log, and sets Orphaned in all.
// It returns the slugs that it marked now.
func (d *Dispatcher) markOrphans(org organization, projects []solidTraceProject, all []storedSettings) ([]string, error) {
	var gone []string
	for i, s := range all {
		if _, ok := slugOf(projects, s.projectID); !ok {
			gone = append(gone, s.projectID)
			all[i].Orphaned = true
		}
	}
	marked, err := d.store.orphan(org.id, gone, d.now().UTC())
	if err != nil {
		return nil, err
	}
	for _, m := range marked {
		d.warn("SolidTrace no longer has the Project: its Factory settings are marked orphaned",
			"organization", org.slug, "factory.project", m)
	}
	return marked, nil
}

func slugOf(projects []solidTraceProject, id string) (string, bool) {
	for _, p := range projects {
		if p.id == id {
			return p.slug, true
		}
	}
	return "", false
}

func noProject(slug, organization string) string {
	return "SolidTrace has no Project " + slug + " in the Organization " + organization
}

func noSettings(slug string) string {
	return "the Project " + slug + " has no Factory settings: an Admin sets them with " +
		"'solidtrace projects set " + slug + " --repository REPOSITORY'"
}

var (
	baseBranchPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]{0,254}$`)
	// connectionPattern is a connection's name.
	connectionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
)

// cleanSettings puts the values in their stored form, or says what is
// wrong.
func cleanSettings(s *dispatcherapi.SaveFactorySettings) string {
	repository := strings.TrimSpace(s.Repository)
	if repository == "" {
		return "the Factory settings need a repository, such as bitbucket.org/workspace/name"
	}
	remote := repository
	if !strings.Contains(remote, "://") && !strings.Contains(remote, ":") {
		remote = "https://" + remote
	}
	name, ok := capture.Normalise(remote)
	if !ok || strings.Count(name, "/") < 2 {
		return "the repository must be host/workspace/name or its clone URL, not " + quote(repository)
	}
	s.Repository = name

	folder := strings.Trim(strings.TrimSpace(s.Folder), "/")
	if folder != "" {
		for _, part := range strings.Split(folder, "/") {
			if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\\x00\n\r\t") {
				return "the folder must be a path in the repository, such as apps/shop, not " + quote(s.Folder)
			}
		}
	}
	s.Folder = folder

	branch, ok := cleanBranch(s.BaseBranch)
	if !ok {
		return "the base branch must be a branch name, such as main or release/2.0, not " + quote(s.BaseBranch)
	}
	s.BaseBranch = branch
	s.Connection = strings.TrimSpace(s.Connection)
	if s.Routes != nil {
		seen := map[dispatcherapi.Route]bool{}
		for i, r := range *s.Routes {
			r = dispatcherapi.Route{Connection: strings.TrimSpace(r.Connection), Match: strings.TrimSpace(r.Match)}
			switch {
			case r.Connection == "":
				return "a route needs the name of a tracker connection"
			case utf8.RuneCountInString(r.Match) > maxMatch || strings.ContainsFunc(r.Match, unicode.IsControl):
				return "a route's match value is one line of at most 200 characters, not " + quote(r.Match)
			case seen[r]:
				return "the route (" + r.Connection + ", " + quote(r.Match) + ") is in the list two times"
			}
			seen[r] = true
			(*s.Routes)[i] = r
		}
	}
	return ""
}

// validBranch is true for a branch name that a Run can start from.
func validBranch(branch string) bool {
	return baseBranchPattern.MatchString(branch) && !strings.Contains(branch, "..") &&
		!strings.Contains(branch, "//") && !strings.HasSuffix(branch, "/") && !strings.HasSuffix(branch, ".lock")
}

// cleanBranch gives a base branch in its stored form ("" is none), and
// false when it is not a branch name.
func cleanBranch(branch string) (string, bool) {
	branch = strings.TrimSpace(branch)
	if branch != "" && !validBranch(branch) {
		return "", false
	}
	return branch, true
}

// maxMatch is the most characters of a route's match value.
const maxMatch = 200

// routes finds the tracker connection of each route (nil for nil: the
// save keeps the routes). It answers the request itself when a route names
// no tracker connection of the Organization.
func (d *Dispatcher) routes(w http.ResponseWriter, p person, routes *[]dispatcherapi.Route) ([]storedRoute, bool) {
	if routes == nil {
		return nil, true
	}
	stored := []storedRoute{}
	for _, r := range *routes {
		id, kind, err := d.store.connectionKind(p.organization.id, r.Connection)
		switch {
		case errors.Is(err, errNotFound):
			writeProblem(w, http.StatusUnprocessableEntity, "no connection "+r.Connection+" in the Organization "+
				p.organization.slug+": 'solidtrace connections list' lists them")
			return nil, false
		case err != nil:
			d.serverError(w, "save the Factory settings", err)
			return nil, false
		case d.trackers[kind] == nil:
			writeProblem(w, http.StatusUnprocessableEntity, "a route needs a tracker connection, and "+r.Connection+
				" is a "+kind+" connection")
			return nil, false
		}
		stored = append(stored, storedRoute{r, id})
	}
	return stored, true
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (d *Dispatcher) listConnections(w http.ResponseWriter, r *http.Request, p person) {
	connections, err := d.connections(p.organization.id)
	if err != nil {
		d.serverError(w, "list the connections", err)
		return
	}
	writeJSON(w, http.StatusOK, dispatcherapi.Connections{Connections: connections})
}

// maxToken is the longest token that a connection keeps.
const maxToken = 8 << 10

// saveConnection makes the Organization's connection with the request's
// {name}, or replaces its token.
func (d *Dispatcher) saveConnection(w http.ResponseWriter, r *http.Request, p person) {
	name := r.PathValue("name")
	if !connectionPattern.MatchString(name) {
		writeProblem(w, http.StatusBadRequest, "a connection's name has lower-case letters, digits, '.', '_' and '-' "+
			"(at most 63), not "+quote(name))
		return
	}
	var save dispatcherapi.SaveConnection
	if !decode(w, r, &save, "the body must be a JSON object with kind and token") {
		return
	}
	save.Token = strings.TrimSpace(save.Token)
	switch {
	case save.Kind != dispatcherapi.ForgeBitbucket && d.kinds[save.Kind] == nil:
		writeProblem(w, http.StatusBadRequest, "the kind of a connection must be "+
			strings.Join(d.connectionKinds(), " or ")+", not "+quote(save.Kind))
		return
	case save.Token == "" || len(save.Token) > maxToken:
		writeProblem(w, http.StatusBadRequest, "a connection needs its token (at most 8 KiB)")
		return
	}
	made, err := d.store.saveConnection(p.organization.id, name, save.Kind, save.Token, p, d.now().UTC())
	if err != nil {
		d.serverError(w, "save the connection", err)
		return
	}
	d.log.Info("an Admin saved a connection", "organization", p.organization.slug, "connection", name,
		"kind", save.Kind, "made", made, "by", p.name)
	d.writeConnection(w, p.organization.id, name)
}

// connectionKinds are the kinds that a connection can have.
func (d *Dispatcher) connectionKinds() []string {
	kinds := []string{dispatcherapi.ForgeBitbucket + " (Bitbucket Cloud)"}
	for _, kind := range slices.Sorted(maps.Keys(d.kinds)) {
		if kind != dispatcherapi.ForgeBitbucket {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// connections reads the Organization's connections, with the webhook URL
// of each connection whose kind sends webhooks.
func (d *Dispatcher) connections(organizationID string) ([]dispatcherapi.Connection, error) {
	stored, err := d.store.connections(organizationID)
	connections := make([]dispatcherapi.Connection, 0, len(stored))
	for _, c := range stored {
		if _, ok := d.kinds[c.Kind]; ok && d.config.WebhookURL != "" {
			c.WebhookURL = d.config.WebhookURL + webhookPath(c.Kind, c.id)
		}
		if t := d.trackers[c.Kind]; t != nil {
			settings := trackerSettings(t, c.tracker)
			c.Tracker = &settings
		}
		connections = append(connections, c.Connection)
	}
	return connections, err
}

// writeConnection answers with the Organization's connection with name.
func (d *Dispatcher) writeConnection(w http.ResponseWriter, organizationID, name string) {
	connections, err := d.connections(organizationID)
	if err != nil {
		d.serverError(w, "read the connection", err)
		return
	}
	for _, c := range connections {
		if c.Name == name {
			writeJSON(w, http.StatusOK, c)
			return
		}
	}
	d.serverError(w, "read the connection", errNotFound)
}

// maxWebhookSecret is the longest webhook secret that a connection keeps.
const maxWebhookSecret = 1 << 10

// saveWebhookSecret makes or replaces the webhook secret of the
// Organization's connection with the request's {name}: the secret that
// the connection's service signs each webhook with.
func (d *Dispatcher) saveWebhookSecret(w http.ResponseWriter, r *http.Request, p person) {
	name := r.PathValue("name")
	var save dispatcherapi.SaveWebhookSecret
	if !decode(w, r, &save, "the body must be a JSON object with secret") {
		return
	}
	save.Secret = strings.TrimSpace(save.Secret)
	if save.Secret == "" || len(save.Secret) > maxWebhookSecret {
		writeProblem(w, http.StatusBadRequest, "a webhook secret has 1 to 1024 characters")
		return
	}
	kind, _, err := d.store.connection(p.organization.id, name)
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no connection "+name+" in the Organization "+p.organization.slug+
			": 'solidtrace connections list' lists them")
		return
	case err != nil:
		d.serverError(w, "read the connection", err)
		return
	case d.kinds[kind] == nil:
		writeProblem(w, http.StatusUnprocessableEntity, "a "+kind+" connection gets no webhooks from this Dispatcher")
		return
	}
	if err := d.store.saveWebhookSecret(p.organization.id, name, save.Secret); err != nil {
		d.serverError(w, "save the webhook secret", err)
		return
	}
	d.log.Info("an Admin saved a webhook secret", "organization", p.organization.slug, "connection", name, "by", p.name)
	d.writeConnection(w, p.organization.id, name)
}

// maxTrackerSetting is the most characters of a tracker setting.
const maxTrackerSetting = 200

// saveTrackerSettings replaces the settings of the Organization's tracker
// connection with the request's {name}. An empty value is the kind's
// default.
func (d *Dispatcher) saveTrackerSettings(w http.ResponseWriter, r *http.Request, p person) {
	name := r.PathValue("name")
	var save dispatcherapi.TrackerSettings
	if !decode(w, r, &save, "the body must be a JSON object with ready_status, routing_field and database") {
		return
	}
	save.ReadyStatus, save.RoutingField = strings.TrimSpace(save.ReadyStatus), strings.TrimSpace(save.RoutingField)
	save.Database = strings.TrimSpace(save.Database)
	for _, v := range []string{save.ReadyStatus, save.RoutingField, save.Database} {
		if utf8.RuneCountInString(v) > maxTrackerSetting || strings.ContainsFunc(v, unicode.IsControl) {
			writeProblem(w, http.StatusBadRequest, "a tracker setting is one line of at most 200 characters, not "+quote(v))
			return
		}
	}
	_, kind, err := d.store.connectionKind(p.organization.id, name)
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no connection "+name+" in the Organization "+p.organization.slug+
			": 'solidtrace connections list' lists them")
		return
	case err != nil:
		d.serverError(w, "read the connection", err)
		return
	case d.trackers[kind] == nil:
		writeProblem(w, http.StatusUnprocessableEntity, "a "+kind+" connection is not a tracker connection")
		return
	}
	if check, ok := d.trackers[kind].(settingsCheck); ok {
		if save, err = check.checkSettings(save); err != nil {
			writeProblem(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if save.Database != "" {
		writeProblem(w, http.StatusBadRequest, "a "+kind+" connection has no database")
		return
	}
	if err := d.store.saveTrackerSettings(p.organization.id, name, save); err != nil {
		d.serverError(w, "save the tracker settings", err)
		return
	}
	d.log.Info("an Admin saved the tracker settings of a connection", "organization", p.organization.slug,
		"connection", name, "by", p.name)
	d.writeConnection(w, p.organization.id, name)
}

// decode reads the request's JSON body into v, with no unknown field. It
// answers 400 with problem when it cannot.
func decode(w http.ResponseWriter, r *http.Request, v any, problem string) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		writeProblem(w, http.StatusBadRequest, problem)
		return false
	}
	return true
}
