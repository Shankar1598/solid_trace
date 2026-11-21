package dispatcher

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/linear"
)

// Linear connections (W3). An Admin installs a Linear OAuth app with
// actor=app through the CLI ('solidtrace connections add linear'), so
// that the factory's status changes and comments show as the app. The
// CLI gives the approval's code; the Dispatcher exchanges it, keeps the
// app's client secret and the tokens sealed in the connection's token,
// and refreshes the access token (24 hours) with the rotating refresh
// token. A connection is of one team of the workspace: the issues of
// other teams are not its tickets. A ticket is a Linear issue; its
// routing field is a label group, and its value is the name of the
// issue's label in that group.

// linearBackstop is how often the timer reconciles a Linear connection.
const linearBackstop = 5 * time.Minute

// linearRefreshBefore is how long before its expiry the Dispatcher
// refreshes an access token.
const linearRefreshBefore = 5 * time.Minute

// linearCredentials are what a Linear connection's sealed token holds.
type linearCredentials struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	// App is the app's own Linear user: the actor of what the factory
	// does on Linear.
	App linear.User `json:"app"`
	// Team is the connection's team.
	Team linear.Team `json:"team"`
}

func (c linearCredentials) app() linear.App {
	return linear.App{ClientID: c.ClientID, ClientSecret: c.ClientSecret}
}

func (c *linearCredentials) use(t linear.Tokens) {
	c.AccessToken, c.RefreshToken, c.ExpiresAt = t.AccessToken, t.RefreshToken, t.ExpiresAt
}

// linearTracker is the Tracker of Linear connections.
type linearTracker struct {
	d   *Dispatcher
	api linear.API
	// locks holds one lock for each connection, so that one refresh at a
	// time uses its refresh token: a used refresh token stops.
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newLinearTracker(d *Dispatcher, api linear.API) *linearTracker {
	return &linearTracker{d: d, api: api, locks: map[string]*sync.Mutex{}}
}

func (l *linearTracker) Kind() string { return dispatcherapi.TrackerLinear }

func (l *linearTracker) Signature(header http.Header, body []byte) ([]byte, []byte, error) {
	signature, err := linear.Signature(header)
	return body, signature, err
}

// Delivery refuses a webhook whose webhookTimestamp is absent or more
// than 60 seconds from now, either way.
func (l *linearTracker) Delivery(header http.Header, body []byte, now time.Time) (Delivery, error) {
	w, err := linear.ParseWebhook(body)
	if err != nil {
		return Delivery{}, err
	}
	if age := now.Sub(w.Timestamp); w.Timestamp.IsZero() || age > linear.Window || age < -linear.Window {
		return Delivery{}, ErrStaleDelivery
	}
	return Delivery{ID: header.Get(linear.DeliveryHeader), Entity: w.Issue}, nil
}

func (l *linearTracker) Backstop(WebhookSource) time.Duration { return linearBackstop }

func (l *linearTracker) Defaults() dispatcherapi.TrackerSettings {
	return dispatcherapi.TrackerSettings{ReadyStatus: defaultReadyStatus, RoutingField: "Project"}
}

func (l *linearTracker) Ticket(ctx context.Context, c TrackerConnection, id string) (Ticket, error) {
	var issue linear.Issue
	team := ""
	err := l.with(ctx, c.WebhookSource, func(credentials linearCredentials) (err error) {
		team = credentials.Team.ID
		issue, err = l.api.Issue(ctx, credentials.AccessToken, id)
		return err
	})
	// An issue of another team is not a ticket of the connection, though
	// the app can read it.
	if errors.Is(err, linear.ErrNotFound) || (err == nil && issue.Team != team) {
		return Ticket{}, ErrNoTicket
	}
	return linearTicket(issue, c.Settings.RoutingField), err
}

func (l *linearTracker) ReadyTickets(ctx context.Context, c TrackerConnection) ([]Ticket, error) {
	var issues []linear.Issue
	err := l.with(ctx, c.WebhookSource, func(credentials linearCredentials) (err error) {
		issues, err = l.api.IssuesInStatus(ctx, credentials.AccessToken, credentials.Team.ID, c.Settings.ReadyStatus)
		return err
	})
	tickets := make([]Ticket, 0, len(issues))
	for _, issue := range issues {
		tickets = append(tickets, linearTicket(issue, c.Settings.RoutingField))
	}
	return tickets, err
}

func (l *linearTracker) SetStatus(ctx context.Context, c TrackerConnection, id, status string) error {
	return l.with(ctx, c.WebhookSource, func(credentials linearCredentials) error {
		return l.api.SetStatus(ctx, credentials.AccessToken, id, status)
	})
}

func (l *linearTracker) Comment(ctx context.Context, c TrackerConnection, id, body string) error {
	return l.with(ctx, c.WebhookSource, func(credentials linearCredentials) error {
		return l.api.Comment(ctx, credentials.AccessToken, id, body)
	})
}

// Comments lists the comments on an issue of the connection's team. Only
// a Linear user who is not an app is an insider: a member, or a guest
// whom the team invited. The app's own user (the connection's identity),
// another app, a bot actor (an integration) and an external user are
// not.
func (l *linearTracker) Comments(ctx context.Context, c TrackerConnection, id string) ([]TicketComment, error) {
	var comments []linear.Comment
	var team, issueTeam, self string
	err := l.with(ctx, c.WebhookSource, func(credentials linearCredentials) (err error) {
		team, self = credentials.Team.ID, credentials.App.ID
		issueTeam, comments, err = l.api.Comments(ctx, credentials.AccessToken, id)
		return err
	})
	if errors.Is(err, linear.ErrNotFound) || (err == nil && issueTeam != team) {
		return nil, ErrNoTicket
	}
	if err != nil {
		return nil, err
	}
	list := make([]TicketComment, 0, len(comments))
	for _, lc := range comments {
		comment := TicketComment{ID: lc.ID, Text: lc.Body, CreatedAt: lc.CreatedAt}
		if !lc.EditedAt.IsZero() {
			comment.Edit = lc.EditedAt.UTC().Format(time.RFC3339Nano)
		}
		if lc.User != nil {
			comment.By = TrackerUser{ID: lc.User.ID, Name: lc.User.Name}
		}
		switch {
		case lc.External != "":
			comment.By.Name = cmp.Or(comment.By.Name, lc.External)
			comment.Refusal = "an external user wrote it"
		case lc.Bot != "":
			comment.By.Name = cmp.Or(comment.By.Name, lc.Bot)
			comment.Refusal = "a bot or an integration wrote it"
		case lc.User == nil:
			comment.Refusal = "Linear gives no author"
		case self != "" && lc.User.ID == self:
			comment.Refusal = "the connection's own identity wrote it"
		case lc.User.App:
			comment.Refusal = "an app wrote it"
		default:
			comment.Insider = true
		}
		list = append(list, comment)
	}
	return list, nil
}

// linearTicket is the ticket of an issue. Its routing value is the name
// of its label in the label group field.
func linearTicket(issue linear.Issue, field string) Ticket {
	t := Ticket{ID: issue.ID, URL: issue.URL, Title: issue.Title, Body: issue.Description, Status: issue.Status}
	for _, label := range issue.Labels {
		if label.Group != "" && label.Group == field {
			t.Route = label.Name
			break
		}
	}
	if change := issue.LastStatusChange; change.Status == issue.Status {
		t.StatusBy = TrackerUser{ID: change.By.ID, Name: change.By.Name}
	}
	// The edit is the ticket's content and the time of its status, not
	// Linear's updatedAt: the Dispatcher's own comment changes updatedAt,
	// and must not make a new edit (one "no route" comment for each edit).
	sum := sha256.Sum256([]byte(strings.Join([]string{t.Title, t.Body, t.Status, t.Route,
		issue.LastStatusChange.At.UTC().Format(time.RFC3339Nano)}, "\x00")))
	t.Edit = hex.EncodeToString(sum[:16])
	return t
}

func (l *linearTracker) lock(id string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks[id] == nil {
		l.locks[id] = &sync.Mutex{}
	}
	return l.locks[id]
}

// with runs call with the credentials of the connection, with a valid
// access token. It
// refreshes the token first when it expires in less than
// linearRefreshBefore, and one time more when Linear refuses it (401).
func (l *linearTracker) with(ctx context.Context, source WebhookSource, call func(linearCredentials) error) error {
	lock := l.lock(source.ID)
	lock.Lock()
	defer lock.Unlock()
	credentials, err := l.d.store.linearCredentials(source.ID)
	if err != nil {
		return err
	}
	refreshed := false
	if !l.d.now().Add(linearRefreshBefore).Before(credentials.ExpiresAt) {
		if credentials, err = l.refresh(ctx, source, credentials); err != nil {
			return err
		}
		refreshed = true
	}
	err = call(credentials)
	if !errors.Is(err, linear.ErrUnauthorized) || refreshed {
		return err
	}
	if credentials, err = l.refresh(ctx, source, credentials); err != nil {
		return err
	}
	return call(credentials)
}

// refresh gets new tokens and saves them at once: the old refresh token
// no longer works.
func (l *linearTracker) refresh(ctx context.Context, source WebhookSource, c linearCredentials) (linearCredentials, error) {
	tokens, err := l.api.Refresh(ctx, c.app(), c.RefreshToken, l.d.now().UTC())
	if err != nil {
		return c, fmt.Errorf("refresh the Linear tokens of the connection %s (if Linear refuses the refresh token, "+
			"an Admin installs the app again with 'solidtrace connections add linear %s'): %w", source.Name, source.Name, err)
	}
	c.use(tokens)
	if err := l.d.store.saveLinearCredentials(source.ID, c); err != nil {
		return c, fmt.Errorf("save the refreshed Linear tokens of the connection %s: %w", source.Name, err)
	}
	l.d.log.Info("the Dispatcher refreshed the Linear tokens of a connection", "connection", source.Name,
		"expires_at", tokens.ExpiresAt.Format(time.RFC3339))
	return c, nil
}

// installLinear makes the Organization's Linear connection with the
// request's {name} from the code of an approval, or replaces its tokens.
func (d *Dispatcher) installLinear(w http.ResponseWriter, r *http.Request, p person) {
	name := r.PathValue("name")
	if !connectionPattern.MatchString(name) {
		writeProblem(w, http.StatusBadRequest, "a connection's name has lower-case letters, digits, '.', '_' and '-' "+
			"(at most 63), not "+quote(name))
		return
	}
	var install dispatcherapi.InstallLinear
	if !decode(w, r, &install, "the body must be a JSON object with client_id, client_secret, code, redirect_uri "+
		"and code_verifier") {
		return
	}
	for _, v := range []string{install.ClientID, install.ClientSecret, install.Code, install.RedirectURI} {
		if strings.TrimSpace(v) == "" || len(v) > maxToken {
			writeProblem(w, http.StatusBadRequest, "an install of Linear needs client_id, client_secret, code and "+
				"redirect_uri (each at most 8 KiB): 'solidtrace connections add linear' gives them")
			return
		}
	}
	tracker, ok := d.trackers[dispatcherapi.TrackerLinear].(*linearTracker)
	if !ok {
		d.serverError(w, "install Linear", errors.New("the Dispatcher's linear kind is not its own Linear tracker"))
		return
	}
	app := linear.App{ClientID: install.ClientID, ClientSecret: strings.TrimSpace(install.ClientSecret)}
	tokens, err := tracker.api.Exchange(r.Context(), app, install.Code, install.RedirectURI, install.CodeVerifier, d.now().UTC())
	var refused *linear.OAuthError
	if errors.As(err, &refused) {
		writeProblem(w, http.StatusUnprocessableEntity, "Linear refused the approval ("+refused.Error()+
			"): check the client ID, the client secret and the app's callback URL, and run the command again")
		return
	}
	if err != nil {
		d.log.Warn("could not exchange a Linear approval", "connection", name, "error", err)
		writeProblem(w, http.StatusBadGateway, "the Dispatcher could not reach Linear's token endpoint")
		return
	}
	appUser, err := tracker.api.Viewer(r.Context(), tokens.AccessToken)
	if err != nil {
		d.log.Warn("could not read the Linear app's user", "connection", name, "error", err)
		writeProblem(w, http.StatusBadGateway, "the Dispatcher could not read the Linear app's user")
		return
	}
	team, problem, err := linearTeam(r.Context(), tracker.api, tokens.AccessToken, strings.TrimSpace(install.Team))
	if err != nil {
		d.log.Warn("could not read the Linear app's teams", "connection", name, "error", err)
		writeProblem(w, http.StatusBadGateway, "the Dispatcher could not read the teams of the Linear app")
		return
	}
	if problem != "" {
		writeProblem(w, http.StatusUnprocessableEntity, problem)
		return
	}
	credentials := linearCredentials{ClientID: app.ClientID, ClientSecret: app.ClientSecret, App: appUser, Team: team}
	credentials.use(tokens)
	sealed, err := json.Marshal(credentials)
	if err != nil {
		d.serverError(w, "save the connection", err)
		return
	}
	made, err := d.store.saveConnection(p.organization.id, name, dispatcherapi.TrackerLinear, string(sealed), p, d.now().UTC())
	if err != nil {
		d.serverError(w, "save the connection", err)
		return
	}
	d.log.Info("an Admin installed the Linear app as a connection", "organization", p.organization.slug,
		"connection", name, "made", made, "app", appUser.Name, "team", team.Key, "by", p.name)
	d.writeConnection(w, p.organization.id, name)
}

// linearTeam is the team of an install: the team with the key (in any
// case), or the one team that the app reads when key is "". problem says
// why there is none, for the Admin.
func linearTeam(ctx context.Context, api linear.API, token, key string) (linear.Team, string, error) {
	teams, err := api.Teams(ctx, token)
	if err != nil {
		return linear.Team{}, "", err
	}
	keys := make([]string, 0, len(teams))
	for _, t := range teams {
		if key != "" && strings.EqualFold(t.Key, key) {
			return t, "", nil
		}
		keys = append(keys, t.Key)
	}
	switch {
	case len(teams) == 0:
		return linear.Team{}, "the Linear app can read no team of the workspace", nil
	case key != "":
		return linear.Team{}, "the Linear app cannot read the team " + quote(key) + ": give one of " +
			strings.Join(keys, ", ") + " with --team", nil
	case len(teams) > 1:
		return linear.Team{}, "the Linear app can read the teams " + strings.Join(keys, ", ") +
			": give the connection's team with --team KEY", nil
	}
	return teams[0], "", nil
}

// linearCredentials opens the credentials of the Linear connection id.
func (s *store) linearCredentials(id string) (linearCredentials, error) {
	var c linearCredentials
	var sealed []byte
	err := s.db.QueryRow(`SELECT token FROM connections WHERE id = ? AND kind = ?`, id, dispatcherapi.TrackerLinear).Scan(&sealed)
	if err != nil {
		return c, err
	}
	token, err := s.sealer.open(connectionPurpose(id), sealed)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(token, &c); err != nil || c.Team.ID == "" {
		return c, errors.New("the token of a Linear connection is not its credentials with its team: install the app again")
	}
	return c, nil
}

// saveLinearCredentials replaces the credentials of the Linear connection
// id.
func (s *store) saveLinearCredentials(id string, c linearCredentials) error {
	token, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE connections SET token = ? WHERE id = ? AND kind = ?`,
		s.sealer.seal(connectionPurpose(id), token), id, dispatcherapi.TrackerLinear)
	return err
}
