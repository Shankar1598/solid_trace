package dispatcher

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Trackers. A tracker connection (Notion, Linear) sends webhooks as each
// webhook source does, but the Dispatcher owns its reconcile step: the
// kind reads the tickets, and the Dispatcher routes each ticket in the
// ready status to a Project and starts its Run.

// Tracker is one kind of tracker connection, such as Notion or Linear.
// Kind, Signature, Delivery and Backstop are as in WebhookKind; a
// Delivery's Entity is a ticket id ("" is the whole connection).
type Tracker interface {
	Kind() string
	Signature(header http.Header, body []byte) (message, signature []byte, err error)
	Delivery(header http.Header, body []byte, now time.Time) (Delivery, error)
	Backstop(source WebhookSource) time.Duration
	// Defaults are the settings of a connection that sets no value.
	Defaults() dispatcherapi.TrackerSettings
	// Ticket reads one ticket of the connection. ErrNoTicket is a ticket
	// that the connection does not have (or no longer has).
	Ticket(ctx context.Context, c TrackerConnection, id string) (Ticket, error)
	// ReadyTickets lists the connection's tickets in its ready status.
	ReadyTickets(ctx context.Context, c TrackerConnection) ([]Ticket, error)
	// SetStatus sets a ticket's status.
	SetStatus(ctx context.Context, c TrackerConnection, id, status string) error
	// Comment posts a comment of plain text on a ticket, as the
	// connection's own identity.
	Comment(ctx context.Context, c TrackerConnection, id, body string) error
}

// TrackerConnection is one connection of a Tracker, with its settings
// (each empty value set to the kind's default).
type TrackerConnection struct {
	WebhookSource
	Settings dispatcherapi.TrackerSettings
}

// Ticket is a ticket as the tracker gives it.
type Ticket struct {
	ID string
	// URL is the ticket's page in the tracker.
	URL         string
	Title, Body string
	Status      string
	// Route is the value of the connection's routing field ("" is none).
	Route string
	// Edit names the ticket's current edit: it changes at each change of
	// the ticket, such as Linear's updatedAt. An edit starts at most one
	// Run, and gets at most one "no route" comment.
	Edit string
	// StatusBy is the tracker user who last set the status.
	StatusBy TrackerUser
}

// TrackerUser is a user of a tracker.
type TrackerUser struct{ ID, Name string }

// ErrNoTicket is a ticket that a tracker connection does not have.
var ErrNoTicket = errors.New("the tracker has no such ticket")

// defaultReadyStatus is the ready status of a kind that gives none.
const defaultReadyStatus = "Ready for agent"

// trackerSettings fills each empty value of saved with the kind's
// default.
func trackerSettings(t Tracker, saved dispatcherapi.TrackerSettings) dispatcherapi.TrackerSettings {
	defaults := t.Defaults()
	return dispatcherapi.TrackerSettings{
		ReadyStatus:  cmp.Or(saved.ReadyStatus, defaults.ReadyStatus, defaultReadyStatus),
		RoutingField: cmp.Or(saved.RoutingField, defaults.RoutingField),
	}
}

// trackerKind is a Tracker as a WebhookKind, with the Dispatcher's own
// reconcile step.
type trackerKind struct {
	Tracker
	d *Dispatcher
}

func (k trackerKind) Reconcile(ctx context.Context, source WebhookSource, entity string) ([]string, error) {
	return k.d.reconcileTickets(ctx, k.Tracker, source, entity)
}

// reconcileTickets is the reconcile step of a tracker connection: it reads
// the ticket (or, for "", each ticket in the ready status) and acts on
// each ready ticket. It gives the tickets where it acted.
func (d *Dispatcher) reconcileTickets(ctx context.Context, t Tracker, source WebhookSource, entity string) ([]string, error) {
	saved, err := d.store.trackerSettings(source.ID)
	if err != nil {
		return nil, err
	}
	c := TrackerConnection{WebhookSource: source, Settings: trackerSettings(t, saved)}
	var tickets []Ticket
	if entity == "" {
		tickets, err = t.ReadyTickets(ctx, c)
	} else {
		var ticket Ticket
		ticket, err = t.Ticket(ctx, c, entity)
		tickets = []Ticket{ticket}
	}
	if errors.Is(err, ErrNoTicket) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	step := ticketStep{d: d, t: t, c: c}
	var acted []string
	for _, ticket := range tickets {
		if ticket.Status != c.Settings.ReadyStatus {
			continue
		}
		ok, err := step.ready(ctx, ticket)
		if err != nil {
			return acted, err
		}
		if ok {
			acted = append(acted, ticket.ID)
		}
	}
	return acted, nil
}

// ticketStep is one reconcile step of a tracker connection.
type ticketStep struct {
	d *Dispatcher
	t Tracker
	c TrackerConnection
	// org and projects are the Dispatcher's Organization and its
	// Projects at SolidTrace, read at the step's first need.
	org      organization
	projects []solidTraceProject
}

// ready acts on a ticket in the ready status: it starts a Run on its
// route's Project, or says that no route matches it. It does nothing for
// a ticket that has an Active Run or a Run of its edit, and says a refusal
// one time for each edit. It is true when it acted.
func (s *ticketStep) ready(ctx context.Context, ticket Ticket) (bool, error) {
	d, c := s.d, s.c
	if seen, err := d.store.ticketRun(c.ID, ticket); err != nil || seen {
		return false, err
	}
	value := strings.TrimSpace(ticket.Route)
	projectID, err := d.store.route(c.ID, value)
	if errors.Is(err, errNotFound) {
		return s.noRoute(ctx, ticket, value)
	}
	if err != nil {
		return false, err
	}
	if s.projects == nil {
		if s.org, err = d.tokens.organization(ctx); err != nil {
			return false, err
		}
		if s.org.id != c.organizationID {
			return false, errors.New("the connection is of another Organization than the Dispatcher's token")
		}
		if s.projects, err = d.listProjects(ctx, s.org); err != nil {
			return false, err
		}
	}
	slug, ok := slugOf(s.projects, projectID)
	if !ok {
		return s.orphaned(ticket, projectID)
	}
	if err := d.store.followSlug(projectID, slug); err != nil {
		return false, err
	}
	settings, err := d.store.settings(s.org.id, projectID)
	if err != nil {
		return false, err
	}
	prompt := strings.TrimSpace(ticket.Title)
	if body := strings.TrimSpace(ticket.Body); body != "" {
		prompt += "\n\n" + body
	}
	run := storedRun{
		Run: dispatcherapi.Run{
			ID:           newRunID(),
			State:        dispatcherapi.StateQueued,
			Project:      slug,
			Prompt:       prompt,
			Organization: s.org.slug,
			StartedBy: dispatcherapi.Person{Kind: dispatcherapi.ActorTracker, ID: ticket.StatusBy.ID,
				Name: ticket.StatusBy.Name},
			Link:      ticket.URL,
			CreatedAt: d.now().UTC().Truncate(time.Microsecond),
		},
		organizationID:     s.org.id,
		projectID:          projectID,
		repository:         settings.Repository,
		folder:             settings.Folder,
		baseBranch:         settings.BaseBranch,
		trigger:            triggerTracker,
		ticketConnectionID: c.ID,
		ticketID:           ticket.ID,
		ticketEdit:         ticket.Edit,
	}
	inserted, err := d.store.insertTicketRun(run)
	if err != nil || !inserted {
		return false, err
	}
	d.log.Info("a ready ticket started a Run", "connection", c.Name, "ticket", ticket.ID, "run", run.ID,
		"project", slug, "by", ticket.StatusBy.Name)
	d.wakeDispatch()
	return true, nil
}

// orphaned refuses the Run of a ticket whose route names a Project that
// SolidTrace no longer has, with a warning Log, one time for each edit of
// the ticket. It marks the Project's Factory settings orphaned.
func (s *ticketStep) orphaned(ticket Ticket, projectID string) (bool, error) {
	d := s.d
	if noticed, err := d.store.ticketNoticed(s.c.ID, ticket); err != nil || noticed {
		return false, err
	}
	marked, err := d.store.orphan(s.org.id, []string{projectID}, d.now().UTC())
	if err != nil {
		return false, err
	}
	if err := d.store.noticeTicket(s.c.ID, ticket, d.now().UTC()); err != nil {
		return false, err
	}
	message := "the route of a ready ticket names a Project that SolidTrace no longer has: the Run is refused"
	if len(marked) > 0 {
		message += ", and its Factory settings are marked orphaned"
	}
	d.warn(message, s.ticketArgs(ticket, "factory.project", strings.Join(marked, ", "))...)
	return true, nil
}

// noRoute posts the comment on a ready ticket that no route matches, and
// writes a warning Log, one time for each edit of the ticket.
func (s *ticketStep) noRoute(ctx context.Context, ticket Ticket, value string) (bool, error) {
	d, c := s.d, s.c
	if noticed, err := d.store.ticketNoticed(c.ID, ticket); err != nil || noticed {
		return false, err
	}
	values, err := d.store.matchValues(c.ID)
	if err != nil {
		return false, err
	}
	comment := "SolidTrace Factory: no Project takes this ticket, so no Run started. "
	if len(values) > 0 {
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = quote(v)
		}
		comment += "Set its " + quote(c.Settings.RoutingField) + " field to one of: " + strings.Join(quoted, ", ") + "."
	} else {
		comment += "An Admin adds a route of the tracker connection " + c.Name + " to a Project's Factory settings."
	}
	if err := s.t.Comment(ctx, c, ticket.ID, comment); err != nil {
		return false, err
	}
	if err := d.store.noticeTicket(c.ID, ticket, d.now().UTC()); err != nil {
		return false, err
	}
	d.warn("a ready ticket matches no route: no Run started", s.ticketArgs(ticket,
		"factory.routing_field", c.Settings.RoutingField, "factory.routing_value", value)...)
	return true, nil
}

// ticketArgs are the Attributes of a warning about a ticket, and more.
func (s *ticketStep) ticketArgs(ticket Ticket, more ...string) []string {
	return append([]string{"organization", s.c.organizationSlug, "factory.connection", s.c.Name,
		"factory.connection_kind", s.c.Kind, "factory.ticket", ticket.ID, "factory.ticket_url", ticket.URL,
		"actor.kind", actorTracker, "actor.id", ticket.StatusBy.ID, "actor.name", ticket.StatusBy.Name}, more...)
}
