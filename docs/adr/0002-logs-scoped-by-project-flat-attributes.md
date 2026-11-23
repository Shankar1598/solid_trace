# Logs are scoped by Project, with a flat dotted Attribute namespace

Logs belong to a Project and use its existing keys, rather than to a separate container like Axiom's Datasets, because Sentry SDKs already send logs to a Project's DSN and a second container would add keys, settings and a mapping for no gain; Logs inside a Project are told apart by their `service` field. Every Attribute lives in one flat namespace of dotted names: nested JSON objects are flattened, `{"http.method": …}` and `{"http": {"method": …}}` are the same Attribute, and OTLP resource, scope and record attributes share that namespace without prefixes. This keeps filter chips and queries free of path syntax, at the cost of losing which nesting form or OTLP layer a value came from. Both choices are baked into stored data and are expensive to change later.

## Amended: Queries reach into JSON values with a path (2026-11-04)

Storage stays flat: nested objects are still flattened to dotted names, and nothing stored changes. But Queries now have KQL's path syntax, `.name`, `["name"]` and `[0]`, for values that are JSON ([Query functions 03](../../.scratch/query-functions/issues/03-parse-json.md)). It reaches into a dynamic value only: the value `parse_json(…)` reads, usually from `message`, as in `parse_json(message).result.count`, and an Attribute that holds an array or object, which storage keeps whole as JSON text, as in `ids[0]`. A dotted name on its own, such as `tags.store_id`, stays one Attribute's name: `.name` reaches in only after a value that isn't a name, such as a call or `[…]`, so a Query that compiled before means what it did. Filter chips don't use the syntax and still name flat Attributes, so chips and most queries still need no path, the cost this record rejected nested objects for.

## Amended: How an OTLP log record becomes a Log (2026-11-23)

Log ingest takes OTLP log records ([OTLP Log ingest](../../.scratch/otlp-ingest/plan.md)), and normalises them by the rules of the JSON and Sentry routes: the same limits, the same `original.<field>` rule and the same flattening. OTLP adds these rules of its own: an attribute named as a built-in field is renamed to `original.<name>`; `severity_number` 1 to 24 maps to a Level, and with no `severity_text` a value out of that range is kept as `original.level`; a double that JSON can't hold (NaN, an infinity) is a string; and bytes are a base64 string. The resource, scope and record attributes are Attributes in the one flat namespace, with no prefix. When two layers give the same name, the record wins over the scope, and the scope wins over the resource. Each layer is flattened by itself first, so a record's `{"http": {"method": …}}` wins over a resource's `http.method`. In one layer, a protocol field wins over an attribute of the same name, in the dotted or the nested form: the scope's `name` and `version` win over a scope attribute `otel.scope.name` or `otel.scope.version`, and the record's `event_name` wins over a record attribute `event.name`, which OpenTelemetry deprecates for the field. `service` and `environment` come from the highest layer that has one of their names, and in that layer from the first name in the table. A `kvlist` value is flattened to dotted names, and an array is one value. The other parts of a record become these Fields:

| OTLP | Log |
| --- | --- |
| `time_unix_nano`, else `observed_time_unix_nano` | `time`. A time out of the time window is kept as `original.time`, and the Log gets the receipt time. |
| `severity_number` 1 to 24 | `level`: 1 to 4 `trace`, 5 to 8 `debug`, 9 to 12 `info`, 13 to 16 `warn`, 17 to 20 `error`, 21 to 24 `fatal` |
| `severity_text`, when `severity_number` isn't 1 to 24 | `level`, by the Level spellings of the other routes. An unknown text is kept as `original.level`. With no `severity_text`, a `severity_number` out of the range is kept as `original.level`. |
| `body`, a string | `message` |
| `body` of another type (map, number, bool, array, bytes) | `original.message`, and `message` is empty. Bytes are a base64 string. |
| `trace_id`, `span_id` | `trace_id`, `span_id`, in lower-case hex. An all-zero id is no value. |
| `service.name` | `service`, from the highest layer that has it. The attribute is consumed. |
| `deployment.environment.name`, else `deployment.environment` | `environment`, from the highest layer that has either name, and in that layer from the first name. The name used is consumed, and the other stays an Attribute. |
| scope `name`, `version` | `otel.scope.name`, `otel.scope.version`. They win over a scope attribute of the same name. |
| `event_name` | `event.name`. It wins over a record attribute of the same name. |
| an int64 | a number, or its exact digits as a string when a DOUBLE can't hold it |
| a double that JSON can't hold (NaN, an infinity) | a string |
| bytes | a base64 string |
| an attribute named as a built-in field (`message`, `level`, …), or `solidtrace.*` | `original.<name>`. The rename wins over a sender's own `original.<name>`, as the JSON route's `original.<field>` does. |
| `flags`, `dropped_attributes_count`, `schema_url`, and `severity_text` when `severity_number` gives the Level | not kept |

The names in this table are stored, so they are expensive to change, as the namespace is.

## Considered Options

- **Datasets** (Organization-level or inside a Project): rejected; `service` already separates sources within a Project.
- **Prefixed OTLP layers** (`resource.host.name`, `attributes.http.method`): rejected; the same field would have different names depending on the shipper.
- **Keeping nested objects whole** and addressing them with a path syntax: rejected; every chip and query would need that syntax.

Source: [What is a Log?](../../.scratch/logs/issues/07-log-domain-model.md).
