package dispatcher

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Webhooks (W0, W2). A connection of a kind that sends webhooks (a
// webhook source) has a webhook URL, /webhooks/<kind>/<connection id>. A
// webhook is a hint only: when it is verified, the Dispatcher queues
// "reconcile this connection, this entity" and answers 200 at once. The
// reconcile step reads the current state from the connection's API and
// acts on it. A timer runs the same step for the whole connection as a
// backstop, and the Dispatcher reconciles each connection when it
// starts.

// WebhookKind is one kind of connection that sends webhooks, such as
// Notion, Linear or Bitbucket.
type WebhookKind interface {
	// Kind is the kind's name: the kind of its connections and the
	// <kind> of their webhook URLs.
	Kind() string
	// Signature gives the bytes that the sender signs with HMAC-SHA256
	// and the signature that the delivery carries, from the headers and
	// the raw body. It must not parse the body: the Dispatcher parses
	// nothing before it verifies the signature.
	Signature(header http.Header, body []byte) (message, signature []byte, err error)
	// Delivery parses a verified delivery. ErrStaleDelivery refuses a
	// delivery whose time is outside the kind's window (401); another
	// error refuses a body that the kind cannot read (400).
	Delivery(header http.Header, body []byte, now time.Time) (Delivery, error)
	// Reconcile reads the current state of the entity ("" is each
	// entity of the connection) from the connection's API and acts on
	// it. It gives the entities where it found a change and acted, in the
	// form of Delivery's Entity. It must be safe to run again.
	Reconcile(ctx context.Context, source WebhookSource, entity string) (changed []string, err error)
	// Backstop is how often the timer reconciles the connection.
	Backstop(source WebhookSource) time.Duration
}

// Delivery is what a verified webhook says.
type Delivery struct {
	// ID is the delivery id for the dedupe, such as Linear-Delivery,
	// Notion's event id or Bitbucket's request UUID.
	ID string
	// Entity is the entity to reconcile, such as a ticket or a PR ("" is
	// the whole connection).
	Entity string
}

// WebhookSource is one connection of a WebhookKind.
type WebhookSource struct {
	ID, Name, Kind string
	// Token is the connection's token (in Reconcile only).
	Token string
	// LastWebhook is the time of its last verified webhook (zero is
	// none).
	LastWebhook    time.Time
	organizationID string
}

// ErrStaleDelivery is a delivery whose time is outside its kind's window.
var ErrStaleDelivery = errors.New("the delivery's time is outside the window")

// The limits of the webhook handler.
const (
	// MaxWebhookBody is the largest webhook body (413 above it).
	MaxWebhookBody = 1 << 20
	// WebhookRateLimit is the most verified webhooks that a connection
	// can send in a minute (429 above it).
	WebhookRateLimit = 120
	// maxDeliveryID is the longest delivery id.
	maxDeliveryID = 256
	// reconcileTimeout is the longest time of one reconcile step.
	reconcileTimeout = 5 * time.Minute
	// defaultReconcileTick is how often the timer looks for a due
	// backstop.
	defaultReconcileTick = time.Second
)

// webhookPart is the <kind> or the <connection id> of a webhook URL.
var webhookPart = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

func webhookPath(kind, id string) string { return "/webhooks/" + kind + "/" + id }

// WebhookHandler serves the webhook listener: POST /webhooks/<kind>/<id>
// only. Each other path and method gets 404, so the listener serves no
// part of the API.
func (d *Dispatcher) WebhookHandler() http.Handler { return http.HandlerFunc(d.webhook) }

func (d *Dispatcher) webhook(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if r.Method != http.MethodPost || len(parts) != 4 || parts[0] != "" || parts[1] != "webhooks" ||
		!webhookPart.MatchString(parts[2]) || !webhookPart.MatchString(parts[3]) {
		writeProblem(w, http.StatusNotFound, "no such path")
		return
	}
	kind := d.kinds[parts[2]]
	if kind == nil {
		writeProblem(w, http.StatusNotFound, "no such path")
		return
	}
	source, secret, err := d.store.webhookSource(parts[3])
	if errors.Is(err, errNotFound) || (err == nil && source.Kind != kind.Kind()) {
		writeProblem(w, http.StatusNotFound, "no such path")
		return
	}
	if err != nil {
		d.serverError(w, "read the connection", err)
		return
	}
	if r.ContentLength > MaxWebhookBody {
		writeProblem(w, http.StatusRequestEntityTooLarge, "a webhook body is at most 1 MiB")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWebhookBody))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeProblem(w, http.StatusRequestEntityTooLarge, "a webhook body is at most 1 MiB")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "the Dispatcher could not read the body")
		return
	}
	if !verified(kind, secret, r.Header, body) {
		writeProblem(w, http.StatusUnauthorized, "the webhook has no valid signature")
		return
	}
	now := d.now().UTC()
	if wait, ok := d.webhookLimits.take(source.ID, now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Round(time.Second)/time.Second)))
		writeProblem(w, http.StatusTooManyRequests, "this connection sent more than "+
			strconv.Itoa(WebhookRateLimit)+" webhooks in a minute")
		return
	}
	delivery, err := kind.Delivery(r.Header, body, now)
	if errors.Is(err, ErrStaleDelivery) {
		writeProblem(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil || delivery.ID == "" || len(delivery.ID) > maxDeliveryID {
		writeProblem(w, http.StatusBadRequest, "the Dispatcher could not read the webhook's delivery id and entity")
		return
	}
	fresh, err := d.store.keepDelivery(source.ID, delivery.ID, now)
	if err != nil {
		d.serverError(w, "keep the webhook", err)
		return
	}
	if fresh {
		d.reconciles.add(reconcileJob{source: source.ID, entity: delivery.Entity, announced: true}, now)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"queued": fresh})
}

// verified checks the delivery's HMAC-SHA256 signature with the
// connection's secret, in constant time. A connection with no secret
// verifies nothing.
func verified(kind WebhookKind, secret []byte, header http.Header, body []byte) bool {
	if len(secret) == 0 {
		return false
	}
	message, signature, err := kind.Signature(header, body)
	if err != nil || len(signature) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(message)
	return hmac.Equal(mac.Sum(nil), signature)
}

// webhookLimits counts each connection's verified webhooks in the
// current minute.
type webhookLimits struct {
	mu      sync.Mutex
	windows map[string]webhookWindow
}

type webhookWindow struct {
	start time.Time
	count int
}

// take counts one webhook of the connection at now. It is false, with
// the wait until the next minute, when the connection is at its limit.
func (l *webhookLimits) take(connectionID string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.windows[connectionID]
	if now.Sub(window.start) >= time.Minute || now.Before(window.start) {
		window = webhookWindow{start: now}
	}
	if window.count >= WebhookRateLimit {
		return max(window.start.Add(time.Minute).Sub(now), time.Second), false
	}
	window.count++
	l.windows[connectionID] = window
	return 0, true
}

// reconcileJob is one queued reconcile step.
type reconcileJob struct {
	source, entity string
	// announced is true when a webhook queued the step; a step that the
	// timer queued (a backstop) warns of each change that no webhook
	// announced.
	announced bool
	// backstop is true for the timer's step, which warns; the step at
	// start does not.
	backstop bool
	queuedAt time.Time
}

func (j reconcileJob) key() string { return j.source + "\x00" + j.entity }

// reconcileQueue is the queue of reconcile steps. It holds each
// (connection, entity) one time: a step that is queued again before it
// runs runs one time.
type reconcileQueue struct {
	mu      sync.Mutex
	order   []string
	pending map[string]reconcileJob
	// announcedAt is the time of the last webhook of each (connection,
	// entity), for the warning of the backstop.
	announcedAt map[string]time.Time
	// reconciledAt is the queuedAt of each connection's last step of the
	// whole connection that did not fail.
	reconciledAt map[string]time.Time
	wake         chan struct{}
}

func newReconcileQueue() *reconcileQueue {
	return &reconcileQueue{pending: map[string]reconcileJob{}, announcedAt: map[string]time.Time{},
		reconciledAt: map[string]time.Time{}, wake: make(chan struct{}, 1)}
}

func (q *reconcileQueue) add(job reconcileJob, now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := job.key()
	if job.announced {
		q.announcedAt[key] = now
	}
	if queued, ok := q.pending[key]; ok {
		queued.backstop = queued.backstop && job.backstop
		q.pending[key] = queued
	} else {
		job.queuedAt = now
		q.pending[key] = job
		q.order = append(q.order, key)
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// next takes the oldest queued step.
func (q *reconcileQueue) next() (reconcileJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.order) == 0 {
		return reconcileJob{}, false
	}
	key := q.order[0]
	q.order = q.order[1:]
	job := q.pending[key]
	delete(q.pending, key)
	return job, true
}

// reconciled records a step of the whole connection that did not fail,
// and gives the changes that it found and that no webhook announced
// since the connection's previous such step. A webhook whose own step
// failed announced its change all the same: the webhooks work. It
// forgets announcements older than a day.
func (q *reconcileQueue) reconciled(job reconcileJob, changed []string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	for key, at := range q.announcedAt {
		if job.queuedAt.Sub(at) > 24*time.Hour {
			delete(q.announcedAt, key)
		}
	}
	since, ok := q.reconciledAt[job.source]
	if !ok || since.After(job.queuedAt) {
		since = job.queuedAt
	}
	if job.queuedAt.After(q.reconciledAt[job.source]) {
		q.reconciledAt[job.source] = job.queuedAt
	}
	if whole, ok := q.announcedAt[job.source+"\x00"]; ok && !whole.Before(since) {
		return nil
	}
	var left []string
	for _, entity := range changed {
		at, ok := q.announcedAt[job.source+"\x00"+entity]
		if !ok || at.Before(since) {
			left = append(left, entity)
		}
	}
	return left
}

// reconcileLoop runs the queued reconcile steps, one at a time.
func (d *Dispatcher) reconcileLoop(ctx context.Context) {
	for {
		for {
			job, ok := d.reconciles.next()
			if !ok {
				break
			}
			d.reconcile(ctx, job)
			if ctx.Err() != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-d.reconciles.wake:
		}
	}
}

func (d *Dispatcher) reconcile(ctx context.Context, job reconcileJob) {
	source, err := d.store.withToken(job.source)
	if errors.Is(err, errNotFound) {
		return
	}
	if err != nil {
		d.log.Error("could not read a connection to reconcile", "connection.id", job.source, "error", err)
		return
	}
	kind := d.kinds[source.Kind]
	if kind == nil {
		return
	}
	stepCtx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	changed, err := kind.Reconcile(stepCtx, source, job.entity)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			d.log.Warn("a reconcile step failed; the next backstop runs it again", "connection", source.Name,
				"kind", source.Kind, "entity", job.entity, "error", err)
		}
		return
	}
	if job.entity != "" {
		return
	}
	missed := d.reconciles.reconciled(job, changed)
	if job.backstop && len(missed) > 0 {
		last := "never"
		if !source.LastWebhook.IsZero() {
			last = source.LastWebhook.Format(time.RFC3339)
		}
		d.warn("the backstop of the connection "+source.Name+" found a change that no webhook announced "+
			"(last webhook: "+last+")",
			"factory.connection", source.Name, "factory.connection_kind", source.Kind,
			"factory.entities", strings.Join(missed, ", "), "factory.last_webhook_at", last)
	}
}

// backstopLoop queues a reconcile step of each connection at once at
// start, and then each time its kind's backstop interval passes.
func (d *Dispatcher) backstopLoop(ctx context.Context, tick time.Duration) {
	due := map[string]time.Time{}
	for {
		d.queueDue(due)
		select {
		case <-ctx.Done():
			return
		case <-time.After(tick):
		}
	}
}

func (d *Dispatcher) queueDue(due map[string]time.Time) {
	sources, err := d.store.webhookSources()
	if err != nil {
		d.log.Error("could not read the connections for the backstop", "error", err)
		return
	}
	now := d.now().UTC()
	for _, source := range sources {
		kind := d.kinds[source.Kind]
		if kind == nil {
			continue
		}
		at, seen := due[source.ID]
		if seen && now.Before(at) {
			continue
		}
		due[source.ID] = now.Add(cmp.Or(kind.Backstop(source), 5*time.Minute))
		d.reconciles.add(reconcileJob{source: source.ID, backstop: seen}, now)
	}
}
