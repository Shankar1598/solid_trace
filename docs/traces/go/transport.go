package main

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
)

// DualTransport sends each event, Transaction and Log to SolidTrace as well as
// to the DSN in the client's options, sentry.io's: the same item, in an
// envelope with each one's own key in its auth header, which is all
// SolidTrace finds the Project by. With SolidTraceDSN empty, only sentry.io
// gets them.
type DualTransport struct {
	// SolidTraceDSN is the Project's DSN in SolidTrace.
	SolidTraceDSN string

	primary, solidtrace *sentry.HTTPTransport
}

func (t *DualTransport) Configure(options sentry.ClientOptions) {
	t.primary = sentry.NewHTTPTransport()
	t.primary.Configure(options)
	options.Dsn = t.SolidTraceDSN
	t.solidtrace = sentry.NewHTTPTransport()
	t.solidtrace.Configure(options)
}

func (t *DualTransport) SendEvent(event *sentry.Event) {
	t.primary.SendEvent(event)
	t.solidtrace.SendEvent(event)
}

func (t *DualTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.FlushWithContext(ctx)
}

func (t *DualTransport) FlushWithContext(ctx context.Context) bool {
	primary := t.primary.FlushWithContext(ctx)
	return t.solidtrace.FlushWithContext(ctx) && primary
}

func (t *DualTransport) Close() {
	t.primary.Close()
	t.solidtrace.Close()
}
