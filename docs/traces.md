# Traces

SolidTrace keeps the Transactions that Sentry's SDKs send, with their Spans, and links each one to the Logs and Issues that share its trace id. A Project's **Performance** page lists its Transactions by the time they take, a Transaction's page shows where that time goes, and a trace's page shows one request across the Organization's Projects: its Spans as a waterfall, its Logs and its Issues. Over [MCP](mcp.md), `list_transactions`, `get_transaction` and `get_trace` answer the same, and SQL queries can read the `spans` table.

This page shows how to turn tracing on, how to send to sentry.io and SolidTrace at once while you move over, and how to put the trace id on your Logs. The words used here (Trace, Span, Transaction) are in the [glossary](../GLOSSARY.md).

## Turning tracing on

Point the SDK at the Project's DSN, from its setup page in Console, and give it a sample rate: the share of requests and jobs it traces. 1–5% is plenty for a busy app. Sampling stays in the SDK, and SolidTrace keeps every Transaction it receives. Performance scales throughput back up by each Transaction's sample rate, and labels it an estimate.

**Ruby** (sentry-ruby and sentry-rails 5.x or 6.x), in `config/initializers/sentry.rb`:

```ruby
Sentry.init do |config|
  config.dsn = ENV["SENTRY_DSN"]
  config.traces_sample_rate = 0.05
end
```

sentry-rails traces each request and Active Job, with Spans for SQL, views, caches and HTTP calls; sentry-sidekiq traces each Sidekiq job.

**Go** (sentry-go):

```go
err := sentry.Init(sentry.ClientOptions{
	Dsn:              os.Getenv("SENTRY_DSN"),
	EnableTracing:    true,
	TracesSampleRate: 0.05,
})
```

Wrap your handlers with `sentryhttp` (or the integration for your router), which starts a Transaction for each request.

A Transaction should be named by its route or job, such as `OrdersController#show` or `GET /orders/{id}`. One named by its raw URL, such as `/orders/123`, is counted in one row, **Unparameterised**, so that each id doesn't make a row of its own.

## Sending to sentry.io and SolidTrace at once

While you move an app over, it can send everything to both: one SDK, with a transport that sends each envelope twice. Keep sentry.io's DSN as the SDK's and give the transport SolidTrace's.

The same envelope works for both because SolidTrace finds the Project by the key in the request's auth header alone. The envelope itself names sentry.io's DSN and key, and SolidTrace doesn't read them. Sampling happens once, in the SDK, so both get the same Transactions.

**Ruby.** Add the transport, and set `SOLIDTRACE_DSN` to the Project's DSN in SolidTrace:

```ruby
require "sentry-ruby"

# Sends each envelope to SolidTrace as well as to the DSN in Sentry's config,
# sentry.io's: the same bytes, each with its own key in the auth header, which
# is all SolidTrace finds the Project by. SOLIDTRACE_DSN is the Project's DSN
# in SolidTrace; unset, only sentry.io gets them. When SolidTrace fails, the
# SDK logs a warning, and sentry.io still gets the envelope.
class DualTransport < Sentry::HTTPTransport
  def initialize(configuration, dsn: nil)
    super(configuration)
    if dsn
      # The copy that sends to SolidTrace.
      @dsn = Sentry::DSN.new(dsn)
    elsif ENV["SOLIDTRACE_DSN"].to_s != ""
      @solidtrace = DualTransport.new(configuration, dsn: ENV["SOLIDTRACE_DSN"])
    end
  end

  def send_data(data, *)
    super(data)
  ensure
    begin
      @solidtrace&.send_data(data)
    rescue Sentry::ExternalError => e
      log_warn("SolidTrace didn't take the envelope: #{e.message}")
    end
  end
end
```

```ruby
Sentry.init do |config|
  config.dsn = ENV["SENTRY_DSN"] # sentry.io's
  config.traces_sample_rate = 0.05
  config.transport.transport_class = DualTransport
end
```

**Go.** Add the transport, and give it SolidTrace's DSN:

```go
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
```

```go
err := sentry.Init(sentry.ClientOptions{
	Dsn:              os.Getenv("SENTRY_DSN"), // sentry.io's
	EnableTracing:    true,
	TracesSampleRate: 0.05,
	Transport:        &DualTransport{SolidTraceDSN: os.Getenv("SOLIDTRACE_DSN")},
})
```

Both transports are tested against EventStore: [`docs/traces/`](traces/) has them with a sender each, which `event_store/traces_docs_test.go` runs. Once SolidTrace has proved itself, drop the transport and make SolidTrace's DSN the SDK's.

Inside the same VPC as SolidTrace, use the DSN with its host swapped for the machine's internal IP and port 4000, over `http`, as `deploy/install` says when it finishes.

## Putting the trace id on Logs

A trace's page shows the Logs whose trace id is the trace's. Sentry's SDK knows the trace id of the request or job it is in, sampled or not, so each line it logs can carry it. The recipe below tags lines `trace=<id>` without changing their format, so your shippers don't change, and a Log transform moves the tag into the Log's trace id when SolidTrace receives it.

A JSON line can instead carry a `trace_id` field: SolidTrace reads it as the Log's trace id, with no Log transform.

**Requests.** Rack middleware that tags each line a request logs. It goes after Sentry's middleware, which starts the request's Transaction:

```ruby
# config/initializers/trace_log_tag.rb
# Tags each line a request logs [trace=<id>], with its Sentry trace id.
class TraceLogTag
  def initialize(app)
    @app = app
  end

  def call(env)
    trace_id = Sentry.get_current_scope&.get_span&.trace_id
    return @app.call(env) unless trace_id

    Rails.logger.tagged("trace=#{trace_id}") { @app.call(env) }
  end
end

Rails.application.config.middleware.insert_after Sentry::Rails::CaptureExceptions, TraceLogTag
```

A line then reads `[trace=6d2f0e4a9b3d4e6f8a7b6c5d4e3f2b10] Completed 200 OK in 41ms`, after any tags of your own.

**Sidekiq jobs.** Server middleware does the same for a job. sentry-sidekiq puts its own middleware first, so this one, added after it, runs inside the job's Transaction. It also adds the trace to Sidekiq's context, so Sidekiq's own lines carry `trace=<id>` among their other `key=value` pairs:

```ruby
# config/initializers/sidekiq.rb
class SidekiqTraceLogTag
  include Sidekiq::ServerMiddleware

  def call(_job, _payload, _queue)
    trace_id = Sentry.get_current_scope&.get_span&.trace_id
    return yield unless trace_id

    Sidekiq::Context.with(trace: trace_id) do
      Rails.logger.tagged("trace=#{trace_id}") { yield }
    end
  end
end

Sidekiq.configure_server do |config|
  config.server_middleware { |chain| chain.add SidekiqTraceLogTag }
end
```

**lograge.** When lograge writes through `Rails.logger`, its line is inside the request middleware's tag already. With a logger of its own, add the trace to its custom options. The `key_value` formatter then writes `trace=<id>` on the line; with the `json` formatter, name it `trace_id` instead, which SolidTrace reads with no Log transform:

```ruby
config.lograge.custom_options = lambda do |_event|
  { trace: Sentry.get_current_scope&.get_span&.trace_id }.compact
end
```

## The Log transform

This Log transform ([ADR 0012](adr/0012-log-transform-runs-vrl-through-cgo.md)) moves the tag into the Log's trace id, and takes it out of the message. It reads both forms above: `[trace=<id>]` on a tagged line and `trace=<id>` on a `key=value` line. A Log whose trace id is already set is left as it is. Set it on the Project's **Log transform** page, and preview it on the Project's recent Logs before saving; or have an agent do it with `preview_log_transform` and `update_log_transform`.

```coffee
# Moves a trace=<id> tag on a line into the Log's trace id, and out of its
# message: a tagged Rails line's [trace=<id>], and a key=value line's
# trace=<id>, such as lograge's or Sidekiq's. A Log whose trace id is already
# set, or whose line has no tag, is left as it is.
if !exists(.trace_id) && is_string(.message) {
  tag, err = parse_regex(.message, r'\[?\btrace=(?P<id>[0-9a-f]{32})\]? ?')
  if err == null {
    .trace_id = tag.id
    .message = strip_whitespace(replace(string!(.message), r'\[?\btrace=[0-9a-f]{32}\]? ?', "", count: 1))
  }
}
```

If the Log transform already does other work, add these lines to it. A Log transform applies to the Logs processed after it is saved; Logs already stored keep their messages.

## A trace that wasn't sampled

At a 5% sample rate, 95 requests in 100 send no Transaction. They still have a trace id: their Issues' Events carry it, and with the middleware above so do their Logs. So a link from such an Issue or Log still opens the trace's page. It shows the trace's Logs and Issues, read around the time of the Log or Event it was opened from, with a note that the request wasn't sampled and so has no Spans. `get_trace` answers the same.

## SolidTrace traces itself

An install traces its own Console into the Meta Project, the Project in Console's "SolidTrace" Organization that already receives SolidTrace's own Logs ([ADR 0011](adr/0011-meta-organization-and-vector-reads-docker-logs.md)). `deploy/install` writes two settings to `deploy/.env` the first time it runs, and keeps them after:

- `SENTRY_DSN`: the Meta Project's DSN on this machine, made from its key. Console's Issues go there too.
- `SENTRY_TRACES_SAMPLE_RATE`: the share of Console's requests and jobs it traces, `0.05` to start. Set it to `0` to trace none.

Restart Console after changing either: `sudo docker compose -f deploy/docker-compose.yml up -d console jobs`. Settings → Instance shows both.

Two instance settings on Settings → Instance, both on to start, turn these off with no restart, from the next Event or request on (another Console process within 10 seconds). `update_instance_settings` changes them too:

- **Send Console's Issues**: off, Console sends no Events, whatever DSN it has.
- **Trace Console**: off, Console traces nothing, even a request whose caller's trace was sampled.

Console never traces its job that reads EventStore's messages, which runs every second, or health checks (`/up`, `/health`). It keeps the sampling decision of a trace that reaches it already sampled, or not. A job that notifies the Meta Organization's Integrations, about its Issues or Monitors, sends no Event when it fails, only its log line: that Event would be a Meta Project Issue, whose notification would fail again, and so on.

Console's text lines carry `[trace=<id>]` and its JSON lines, such as its line for each request, a `trace_id` field. The Meta Project is seeded with the Log transform above, so its Logs have their trace ids.
