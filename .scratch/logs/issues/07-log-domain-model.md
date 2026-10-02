# 07: What is a Log?

Type: grilling
Status: resolved
Blocked by: 01, 02
Map: [SolidTrace Logs](../map.md)

## Question

What is the domain model for logs: which fields every Log has (time, level, message, service, environment, trace and span ids …) versus free-form attributes; how nested JSON and arrays are addressed; whether Logs are scoped by Project or by a new concept such as Axiom's Dataset; and which glossary terms enter `CONTEXT.md`.

Input from [How should the Logs explorer be laid out?](06-logs-explorer-layout.md) (2026-10-02): the chosen layout shows each log's level, `service` and message on every line and puts summary numbers (error rate, p95 duration) beside the histogram. Which of these are built-in fields of a Log, and which are only conventions, is this ticket's call.

## Answer

Resolved 2026-10-02 (grilling). Glossary terms added to `CONTEXT.md`: **Log**, **Attribute**, **Field**, **Level**, **Service**. The Project scoping and flat Attribute namespace are recorded in [ADR 0002](../../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md).

- **Scope:** a Log belongs to a Project and is submitted with the Project's existing keys. There is no Dataset concept. Logs are never grouped into Issues. Logs within one Project are told apart by `service`. Axiom-style ingest maps its dataset name to a Project.
- **Built-in fields (seven):** `time`, `level`, `message`, `service`, `environment`, `trace_id`, `span_id`. Every other value is an Attribute. `host`, `release` and `duration_ms` are only conventions. The explorer's summary shows p95 duration only when `duration_ms` exists; otherwise it shows the log count and per-level counts.
- **`time`:** the Log's own timestamp, or the time SolidTrace received it if it has none. There is no separate received time. A timestamp older than the Project's retention, or more than about an hour in the future, is replaced with the receipt time. Whether that window is configurable is left to the spec.
- **`level`:** one of `trace`, `debug`, `info`, `warn`, `error`, `fatal`. Common spellings map case-insensitively (`warning`→`warn`, `err`→`error`, `critical`/`crit`/`panic`/`emergency`→`fatal`, `notice`→`info`), and OTLP severity numbers map by band. A missing or unrecognised level leaves the Log with no level. There is no default.
- **Reserved names:** built-in field names always mean the built-in. A sent value that cannot fill its built-in field (an unrecognised level, a replaced time, a malformed trace id) is kept as the Attribute `original.<field>`, so nothing sent is lost.
- **`message`:** always a string, possibly empty. A structured body (an OTLP map, a JSON log) is flattened into Attributes. Which incoming keys count as the message (`msg`, `message`, `log` …) is a mapping rule for [Which ingest sources ship in v1, with what auth and limits?](08-v1-ingest-sources-auth-limits.md).
- **Nested objects:** flattened to dotted names. `{"http.method": …}` and `{"http": {"method": …}}` name the same Attribute; if both appear in one Log, the literal dotted key wins.
- **Arrays:** one value holding a list, queryable with "contains". Elements get no names of their own, and objects inside arrays are not flattened.
- **Types:** each value keeps its own type (string, number, boolean, list, nested object inside a list). One Attribute can be a number in some Logs and a string in others, and a numeric comparison matches only the numeric values.
- **OTLP and Sentry attributes:** resource, scope and record attributes share one flat namespace with no prefixes; on a clash the record's own attribute wins. `service.name` fills `service`, `deployment.environment` fills `environment`. Sentry's `sentry.*` keys stay Attributes under their own names.
- **Identity:** each Log gets an id at ingest, used only to open or link to it. No deduplication in v1: a retried batch can store duplicate Logs. Duplicate detection is v2 fog.
