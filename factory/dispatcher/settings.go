package dispatcher

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"

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
	if err := d.store.saveSettings(p.organization.id, project.id, project.slug, save, connectionID, p, d.now().UTC()); err != nil {
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
	slug := r.PathValue("project")
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
// that SolidTrace does not have or that has no Factory settings. The
// settings of each Project that SolidTrace no longer has are marked
// orphaned. It answers the request itself when it refuses.
func (d *Dispatcher) triggerSettings(w http.ResponseWriter, r *http.Request, p person, slug string) (storedSettings, bool) {
	projects, err := d.listProjects(r.Context(), p.organization)
	if err != nil {
		d.solidTraceFailure(w, p.organization, err)
		return storedSettings{}, false
	}
	all, err := d.store.allSettings(p.organization.id)
	if err != nil {
		d.serverError(w, "read the Factory settings", err)
		return storedSettings{}, false
	}
	marked, err := d.markOrphans(p.organization, projects, all)
	if err != nil {
		d.serverError(w, "mark the Factory settings orphaned", err)
		return storedSettings{}, false
	}
	i := slices.IndexFunc(projects, func(sp solidTraceProject) bool { return sp.slug == slug })
	if i < 0 {
		d.warn("a trigger names a Project that SolidTrace does not have: the Run is refused",
			p.warnArgs(slug)...)
		message := noProject(slug, p.organization.slug) + ": the Run is refused"
		if slices.Contains(marked, slug) {
			message += ", and its Factory settings are marked orphaned"
		}
		writeProblem(w, http.StatusUnprocessableEntity, message)
		return storedSettings{}, false
	}
	settings, err := d.store.settings(p.organization.id, projects[i].id)
	if errors.Is(err, errNotFound) {
		d.warn("a trigger names a Project that has no Factory settings: the Run is refused",
			p.warnArgs(slug)...)
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

	branch := strings.TrimSpace(s.BaseBranch)
	if branch != "" && (!baseBranchPattern.MatchString(branch) || strings.Contains(branch, "..") ||
		strings.Contains(branch, "//") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".lock")) {
		return "the base branch must be a branch name, such as main or release/2.0, not " + quote(s.BaseBranch)
	}
	s.BaseBranch = branch
	s.Connection = strings.TrimSpace(s.Connection)
	return ""
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (d *Dispatcher) listConnections(w http.ResponseWriter, r *http.Request, p person) {
	connections, err := d.store.connections(p.organization.id)
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
	case save.Kind != dispatcherapi.ForgeBitbucket:
		writeProblem(w, http.StatusBadRequest, "the kind of a connection must be "+dispatcherapi.ForgeBitbucket+
			" (Bitbucket Cloud), not "+quote(save.Kind))
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
	connections, err := d.store.connections(p.organization.id)
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
