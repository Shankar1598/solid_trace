// Package fakesource is the fake webhook source of Seam 3: a connection
// kind "fake" whose webhooks a test signs and sends, and whose API state
// (the changes that the Dispatcher has not acted on) a test sets. The
// tracker and forge kinds (Notion, Linear, Bitbucket) follow the same
// contract with their own signature and delivery headers.
package fakesource

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/factory/dispatcher"
)

// Kind is the connection kind of the fake source.
const Kind = "fake"

// The headers of a delivery. The signature is "sha256=" and the hex
// HMAC-SHA256 of the raw body, as Bitbucket's.
const (
	SignatureHeader = "Fake-Signature"
	DeliveryHeader  = "Fake-Delivery"
)

// Backstop is how often the Dispatcher's timer reconciles a fake
// connection.
const Backstop = 5 * time.Minute

// Window is the age at which a delivery's timestamp is too old, as
// Linear's webhookTimestamp.
const Window = 60 * time.Second

// Body is a delivery's body. Entity "" announces a change of the whole
// source. Timestamp (Unix ms, 0 is none) must be within Window of the
// Dispatcher's clock.
type Body struct {
	Entity    string `json:"entity"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Padding   string `json:"padding,omitempty"`
}

// Reconcile is one reconcile step that the Dispatcher ran.
type Reconcile struct {
	Connection string
	Entity     string
	// Acted are the changes that the step found and acted on.
	Acted []string
}

// Source is the fake source's API, for all fake connections together.
type Source struct {
	mu         sync.Mutex
	changes    []string
	reconciles []Reconcile
	held       chan struct{}
	changed    chan struct{}
}

// New is a source with no changes.
func New() *Source { return &Source{changed: make(chan struct{})} }

// Change makes a change of entity at the source, which the next reconcile
// step of entity (or of the whole source) finds.
func (s *Source) Change(entity string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(s.changes, entity) {
		s.changes = append(s.changes, entity)
	}
}

// Reconciles are the reconcile steps so far, oldest first.
func (s *Source) Reconciles() []Reconcile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reconciles)
}

// WaitFor waits up to 10 s for a reconcile step that matches, and gives
// it.
func (s *Source) WaitFor(match func(Reconcile) bool) (Reconcile, bool) {
	deadline := time.After(10 * time.Second)
	for {
		s.mu.Lock()
		changed := s.changed
		for _, r := range s.reconciles {
			if match(r) {
				s.mu.Unlock()
				return r, true
			}
		}
		s.mu.Unlock()
		select {
		case <-changed:
		case <-deadline:
			return Reconcile{}, false
		}
	}
}

// Hold makes each reconcile step wait until release.
func (s *Source) Hold() (release func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := make(chan struct{})
	s.held = held
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			if s.held == held {
				s.held = nil
			}
			s.mu.Unlock()
			close(held)
		})
	}
}

// Sign is the signature header's value for body with secret.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// WebhookKind is the fake source as a kind of the Dispatcher's webhooks.
func (s *Source) WebhookKind() dispatcher.WebhookKind { return kind{s} }

type kind struct{ s *Source }

func (kind) Kind() string { return Kind }

func (kind) Signature(header http.Header, body []byte) ([]byte, []byte, error) {
	value, ok := strings.CutPrefix(header.Get(SignatureHeader), "sha256=")
	if !ok {
		return nil, nil, errors.New("no sha256 signature")
	}
	signature, err := hex.DecodeString(value)
	return body, signature, err
}

func (kind) Delivery(header http.Header, body []byte, now time.Time) (dispatcher.Delivery, error) {
	var b Body
	if err := json.Unmarshal(body, &b); err != nil {
		return dispatcher.Delivery{}, err
	}
	if b.Timestamp != 0 {
		if age := now.Sub(time.UnixMilli(b.Timestamp)); age > Window || age < -Window {
			return dispatcher.Delivery{}, dispatcher.ErrStaleDelivery
		}
	}
	return dispatcher.Delivery{ID: header.Get(DeliveryHeader), Entity: b.Entity}, nil
}

func (k kind) Reconcile(ctx context.Context, source dispatcher.WebhookSource, entity string) ([]string, error) {
	s := k.s
	s.mu.Lock()
	held := s.held
	s.mu.Unlock()
	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var acted, left []string
	for _, c := range s.changes {
		if entity == "" || c == entity {
			acted = append(acted, c)
		} else {
			left = append(left, c)
		}
	}
	s.changes = left
	s.reconciles = append(s.reconciles, Reconcile{Connection: source.Name, Entity: entity, Acted: acted})
	close(s.changed)
	s.changed = make(chan struct{})
	return acted, nil
}

func (kind) Backstop(dispatcher.WebhookSource) time.Duration { return Backstop }
