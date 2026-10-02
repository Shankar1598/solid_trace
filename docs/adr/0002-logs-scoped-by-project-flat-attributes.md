# Logs are scoped by Project, with a flat dotted Attribute namespace

Logs belong to a Project and use its existing keys, rather than to a separate container like Axiom's Datasets, because Sentry SDKs already send logs to a Project's DSN and a second container would add keys, settings and a mapping for no gain; Logs inside a Project are told apart by their `service` field. Every Attribute lives in one flat namespace of dotted names: nested JSON objects are flattened, `{"http.method": …}` and `{"http": {"method": …}}` are the same Attribute, and OTLP resource, scope and record attributes share that namespace without prefixes. This keeps filter chips and queries free of path syntax, at the cost of losing which nesting form or OTLP layer a value came from. Both choices are baked into stored data and are expensive to change later.

## Considered Options

- **Datasets** (Organization-level or inside a Project): rejected; `service` already separates sources within a Project.
- **Prefixed OTLP layers** (`resource.host.name`, `attributes.http.method`): rejected; the same field would have different names depending on the shipper.
- **Keeping nested objects whole** and addressing them with a path syntax: rejected; every chip and query would need that syntax.

Source: [What is a Log?](../../.scratch/logs/issues/07-log-domain-model.md).
