# 06: Traces docs, and Console traces itself

Spec: [../spec.md](../spec.md)

**What to build:** the recipe for sending traces and linking Logs, proven on SolidTrace's own Console.

- **A Traces docs page**, linked from the README and the MCP docs, covers:
  - turning tracing on with a sample rate;
  - sending to sentry.io and SolidTrace at once through a custom transport in Ruby and Go, and why the same envelope works for both;
  - putting the trace id on Log lines through request and Sidekiq middleware tagging `trace=<id>`, plus lograge's custom options;
  - a Log transform that moves the tag into the trace id;
  - what an unsampled trace shows.
- **Console traces itself:** the install writes a Meta Project DSN and a sample rate (default 5%). Console sends its Transactions and Events to the Meta Project, its Log lines carry the trace tag, and the Meta Project's seeded Log transform extracts it. The changelog says Console's Issues now go there too.

**Blocked by:** 03

**Status:** resolved

- [x] Both transports deliver one envelope to two local EventStores (`event_store/traces_docs_test.go`: the Ruby and Go senders in `docs/traces/` send through their DualTransport to the test EventStore on two addresses, each with its own Project's DSN; both Projects store the Transaction and the Event's Issue)
- [x] The documented Log transform passes a preview on sample request and job lines (`console/config/trace_tag_log_transform.vrl`, shown as-is in `docs/traces.md` and seeded into the Meta Project)
- [x] A Console request on the dev stack appears as a trace with its Logs. Checked by hand, not on the dev stack (its EventStore wasn't running and the stack is the main checkout's): Console in production mode at a 100% rate sent its envelopes to a listener; those envelopes and its stdout, shaped as Vector ships it, went into the test EventStore with the seeded transform, and each request's trace answered its Spans and its Logs (and the 500's Issue)
- [x] The install script writes the DSN and sample rate. There was no test of `deploy/install`; `console/test/deploy/install_test.rb` is new, and runs the install's new `write_env` from a sourced copy
