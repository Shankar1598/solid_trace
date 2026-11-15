// Package faketracker is the fake tracker of Seam 3: a tracker kind
// "faketracker" that keeps its tickets and comments in memory. A test
// adds tickets and changes them as a tracker user would, sends the
// webhook that announces a change, and reads the comments that the
// Dispatcher posted. Its webhooks are signed and delivered as the fake
// source's (fakesource.Sign, fakesource.SignatureHeader and
// fakesource.DeliveryHeader). The Notion and Linear kinds implement the
// same dispatcher.Tracker against their own APIs.
package faketracker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/fakesource"
)

// Kind is the connection kind of the fake tracker.
const Kind = "faketracker"

// The fake tracker's defaults.
const (
	ReadyStatus  = "Ready for agent"
	RoutingField = "Project"
)

// Backstop is how often the Dispatcher's timer reconciles a fake tracker
// connection.
const Backstop = 5 * time.Minute

// URL is the start of each ticket's URL: URL/<connection>/<id>.
const URL = "https://tracker.example.com"

// Body is a delivery's body: the ticket whose change it announces ("" is
// the whole connection).
type Body struct {
	Ticket string `json:"ticket"`
}

// User is a tracker user.
type User struct{ ID, Name string }

// Ticket is one ticket of a connection.
type Ticket struct {
	ID, Title, Body, Status string
	// Route is the value of the routing field.
	Route string
}

// Comment is a comment that the Dispatcher posted.
type Comment struct{ Ticket, Body string }

type ticket struct {
	Ticket
	edit     int
	statusBy User
}

// Tracker is the fake tracker's API, for all its connections together.
type Tracker struct {
	mu       sync.Mutex
	tickets  map[string]map[string]*ticket
	comments map[string][]Comment
	// refused are the tickets (connection/id) whose comments fail.
	refused map[string]bool
	// statuses are the statuses that the Dispatcher set on each ticket
	// (connection/id), oldest first.
	statuses map[string][]string
	edits    int
}

// New is a tracker with no tickets.
func New() *Tracker {
	return &Tracker{tickets: map[string]map[string]*ticket{}, comments: map[string][]Comment{},
		refused: map[string]bool{}, statuses: map[string][]string{}}
}

// Put makes or replaces a ticket of the connection, as the user. It is a
// new edit.
func (t *Tracker) Put(connection string, tk Ticket, by User) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tickets[connection] == nil {
		t.tickets[connection] = map[string]*ticket{}
	}
	t.edits++
	t.tickets[connection][tk.ID] = &ticket{Ticket: tk, edit: t.edits, statusBy: by}
}

// Delete deletes a ticket of the connection, as the user. The tracker
// then answers dispatcher.ErrNoTicket for it.
func (t *Tracker) Delete(connection, id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.tickets[connection], id)
}

// SetStatus sets a ticket's status as the user. It is a new edit.
func (t *Tracker) SetStatus(connection, id, status string, by User) {
	t.change(connection, id, func(tk *ticket) { tk.Status, tk.statusBy = status, by })
}

// SetRoute sets a ticket's routing field. It is a new edit.
func (t *Tracker) SetRoute(connection, id, value string) {
	t.change(connection, id, func(tk *ticket) { tk.Route = value })
}

func (t *Tracker) change(connection, id string, f func(*ticket)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk := t.tickets[connection][id]
	if tk == nil {
		panic("faketracker: no ticket " + id + " in " + connection)
	}
	f(tk)
	t.edits++
	tk.edit = t.edits
}

// Status is a ticket's status now.
func (t *Tracker) Status(connection, id string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tk := t.tickets[connection][id]; tk != nil {
		return tk.Status
	}
	return ""
}

// RefuseComments makes each comment on the ticket fail, as a token
// without the capability to comment does.
func (t *Tracker) RefuseComments(connection, id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.refused[connection+"/"+id] = true
}

// AcceptComments makes the comments on the ticket work again.
func (t *Tracker) AcceptComments(connection, id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.refused, connection+"/"+id)
}

// Statuses are the statuses that the Dispatcher set on the ticket,
// oldest first.
func (t *Tracker) Statuses(connection, id string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.statuses[connection+"/"+id])
}

// Comments are the comments that the Dispatcher posted on the
// connection's tickets, oldest first.
func (t *Tracker) Comments(connection string) []Comment {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.comments[connection])
}

// TicketURL is the URL of a ticket.
func TicketURL(connection, id string) string { return URL + "/" + connection + "/" + id }

// Delivery is the headers and the body of a delivery of the fake
// tracker that announces a change of the ticket, signed with secret.
func Delivery(secret, delivery, ticketID string) (http.Header, []byte) {
	body, _ := json.Marshal(Body{Ticket: ticketID})
	h := http.Header{}
	h.Set(fakesource.SignatureHeader, fakesource.Sign(secret, body))
	h.Set(fakesource.DeliveryHeader, delivery)
	h.Set("Content-Type", "application/json")
	return h, body
}

// Tracker is the fake tracker as a kind of the Dispatcher's trackers.
func (t *Tracker) Tracker() dispatcher.Tracker { return kind{t} }

type kind struct{ t *Tracker }

func (kind) Kind() string { return Kind }

func (kind) Signature(header http.Header, body []byte) ([]byte, []byte, error) {
	return fakesource.New().WebhookKind().Signature(header, body)
}

func (kind) Delivery(header http.Header, body []byte, _ time.Time) (dispatcher.Delivery, error) {
	var b Body
	if err := json.Unmarshal(body, &b); err != nil {
		return dispatcher.Delivery{}, err
	}
	return dispatcher.Delivery{ID: header.Get(fakesource.DeliveryHeader), Entity: b.Ticket}, nil
}

func (kind) Backstop(dispatcher.WebhookSource) time.Duration { return Backstop }

func (kind) Defaults() dispatcherapi.TrackerSettings {
	return dispatcherapi.TrackerSettings{ReadyStatus: ReadyStatus, RoutingField: RoutingField}
}

// token checks that the Dispatcher sends the connection's token, as a
// real tracker's API would.
func token(c dispatcher.TrackerConnection) error {
	if c.Token != "fake-token-"+c.Name {
		return errors.New("faketracker: the token of " + c.Name + " is " + strconv.Quote(c.Token))
	}
	return nil
}

func (k kind) Ticket(_ context.Context, c dispatcher.TrackerConnection, id string) (dispatcher.Ticket, error) {
	if err := token(c); err != nil {
		return dispatcher.Ticket{}, err
	}
	k.t.mu.Lock()
	defer k.t.mu.Unlock()
	tk := k.t.tickets[c.Name][id]
	if tk == nil {
		return dispatcher.Ticket{}, dispatcher.ErrNoTicket
	}
	return tk.public(c.Name), nil
}

func (k kind) ReadyTickets(_ context.Context, c dispatcher.TrackerConnection) ([]dispatcher.Ticket, error) {
	if err := token(c); err != nil {
		return nil, err
	}
	k.t.mu.Lock()
	defer k.t.mu.Unlock()
	var ready []dispatcher.Ticket
	for _, tk := range k.t.tickets[c.Name] {
		if tk.Status == c.Settings.ReadyStatus {
			ready = append(ready, tk.public(c.Name))
		}
	}
	slices.SortFunc(ready, func(a, b dispatcher.Ticket) int { return strings.Compare(a.ID, b.ID) })
	return ready, nil
}

func (k kind) SetStatus(_ context.Context, c dispatcher.TrackerConnection, id, status string) error {
	if err := token(c); err != nil {
		return err
	}
	k.t.mu.Lock()
	defer k.t.mu.Unlock()
	tk := k.t.tickets[c.Name][id]
	if tk == nil {
		return dispatcher.ErrNoTicket
	}
	tk.Status = status
	k.t.edits++
	tk.edit = k.t.edits
	k.t.statuses[c.Name+"/"+id] = append(k.t.statuses[c.Name+"/"+id], status)
	return nil
}

func (k kind) Comment(_ context.Context, c dispatcher.TrackerConnection, id, body string) error {
	if err := token(c); err != nil {
		return err
	}
	k.t.mu.Lock()
	defer k.t.mu.Unlock()
	if k.t.tickets[c.Name][id] == nil {
		return dispatcher.ErrNoTicket
	}
	if k.t.refused[c.Name+"/"+id] {
		return errors.New("faketracker: the token of " + c.Name + " cannot comment")
	}
	k.t.comments[c.Name] = append(k.t.comments[c.Name], Comment{Ticket: id, Body: body})
	return nil
}

func (tk *ticket) public(connection string) dispatcher.Ticket {
	return dispatcher.Ticket{ID: tk.ID, URL: TicketURL(connection, tk.ID), Title: tk.Title, Body: tk.Body,
		Status: tk.Status, Route: tk.Route, Edit: strconv.Itoa(tk.edit),
		StatusBy: dispatcher.TrackerUser{ID: tk.statusBy.ID, Name: tk.statusBy.Name}}
}
