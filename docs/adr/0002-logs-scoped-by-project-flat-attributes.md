# Logs are scoped by Project, with a flat dotted Attribute namespace

Logs belong to a Project and use its existing keys, rather than to a separate container like Axiom's Datasets, because Sentry SDKs already send logs to a Project's DSN and a second container would add keys, settings and a mapping for no gain; Logs inside a Project are told apart by their `service` field. Every Attribute lives in one flat namespace of dotted names: nested JSON objects are flattened, `{"http.method": …}` and `{"http": {"method": …}}` are the same Attribute, and OTLP resource, scope and record attributes share that namespace without prefixes. This keeps filter chips and queries free of path syntax, at the cost of losing which nesting form or OTLP layer a value came from. Both choices are baked into stored data and are expensive to change later.

## Amended: Queries reach into JSON values with a path (2026-11-04)

Storage stays flat: nested objects are still flattened to dotted names, and nothing stored changes. But Queries now have KQL's path syntax, `.name`, `["name"]` and `[0]`, for values that are JSON ([Query functions 03](../../.scratch/query-functions/issues/03-parse-json.md)). It reaches into a dynamic value only: the value `parse_json(…)` reads, usually from `message`, as in `parse_json(message).result.count`, and an Attribute that holds an array or object, which storage keeps whole as JSON text, as in `ids[0]`. A dotted name on its own, such as `tags.store_id`, stays one Attribute's name: `.name` reaches in only after a value that isn't a name, such as a call or `[…]`, so a Query that compiled before means what it did. Filter chips don't use the syntax and still name flat Attributes, so chips and most queries still need no path, the cost this record rejected nested objects for.

## Considered Options

- **Datasets** (Organization-level or inside a Project): rejected; `service` already separates sources within a Project.
- **Prefixed OTLP layers** (`resource.host.name`, `attributes.http.method`): rejected; the same field would have different names depending on the shipper.
- **Keeping nested objects whole** and addressing them with a path syntax: rejected; every chip and query would need that syntax.

Source: [What is a Log?](../../.scratch/logs/issues/07-log-domain-model.md).
