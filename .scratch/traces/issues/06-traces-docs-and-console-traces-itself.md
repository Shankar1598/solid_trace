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

**Status:** ready-for-agent

- [ ] Both transports deliver one envelope to two local EventStores
- [ ] The documented Log transform passes a preview on sample request and job lines
- [ ] A Console request on the dev stack appears as a trace with its Logs
- [ ] The install script writes the DSN and sample rate (its existing test)
