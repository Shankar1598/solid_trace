# Sending Logs with OpenTelemetry

An OpenTelemetry Collector or SDK sends Logs to EventStore over OTLP/HTTP. Each request goes to one Project. The Logs show in the Logs explorer a few seconds later.

The Logs explorer of a Project with no Logs has two OpenTelemetry tabs in its Get started panel: one for a Collector and one for an SDK. Each tab shows the settings below with the Ingest URL and the Project's key filled in.

## The route

```
POST <Ingest URL>/v1/logs
```

- Send Logs only. EventStore does not take traces and metrics.
- Use OTLP over HTTP. EventStore does not take gRPC, so an exporter must use `http/protobuf` or `http/json`, not `grpc`.
- The Project is the Project of the key.

## Authentication

Send the Project's key in one of these ways:

- `Authorization: Bearer <key>`.
- `X-Sentry-Auth: Sentry sentry_key=<key>`.
- `Authorization: Sentry sentry_key=<key>`.
- `?sentry_key=<key>` in the URL.

A request with no key, or an unknown key, gets `401`.

## What to send

| Content-Type | Body |
| --- | --- |
| `application/x-protobuf` | An `ExportLogsServiceRequest` in protobuf. This is the default of a Collector and of an SDK. |
| `application/json` | The same request in OTLP JSON. Trace ids and span ids are hex. |

EventStore ignores parameters, such as `; charset=utf-8`. Any other Content-Type gets `415`.

The body can be compressed. Set `Content-Encoding` to `gzip`, `deflate`, `br` or `zstd`. A Collector uses `gzip` unless you change it. A body can use one coding only.

The limits are the same as for the other Log routes:

- 10 MiB as sent.
- 50 MiB after decoding.

A body over a limit gets `413`. One request is stored as one Log batch, so keep each request well below the limits.

## The answer

A `200` means the Logs are stored. The body is an `ExportLogsServiceResponse` in the encoding of the request.

When a Log is over 1 MB, EventStore does not store it. The response then has `partial_success`:

- `rejected_log_records` is the number of Logs not stored.
- `error_message` says why.

EventStore stores the other Logs of the request. A Log that has a field cut to a limit is stored, so it is not rejected.

## Errors

| Status | When | A Collector |
| --- | --- | --- |
| `400` | The body does not decode as its Content-Type or Content-Encoding. | Drops the data. |
| `401` | There is no key, or the key is not known. | Drops the data. |
| `413` | The body is over a limit. | Drops the data. |
| `415` | The Content-Type or the Content-Encoding is not one of the above. | Drops the data. |
| `500` | An internal error, for example the lookup of the key fails. | Drops the data. |
| `503` | EventStore is overloaded or shuts down. The answer has `Retry-After: 5`. | Sends the request again after the wait. |

The OTLP/HTTP specification makes a sender retry `429`, `502`, `503` and `504`, and drop the data on any other error. So a sender does not retry a `500` or a `4xx` other than `429`. A Collector 0.165.0 does this. A test showed it for `400`, `401`, `415` and `503`. The `500` follows from the specification and was not tested. The Collector logs a warning when a `200` has a `partial_success`, and it continues to send.

A request that a Collector sends again after a timeout can store its Logs twice. This is true for every Log route.

## Ingest counts

Each request that EventStore reads adds to the Project's Ingest counts:

- One request.
- One Log received for each log record.
- One unreadable Log for each Log that was not stored.
- The bytes, after decoding and as sent.

A body that does not decode as OTLP counts as one request with its bytes and no Logs. A request that EventStore turns away as overloaded is counted as overloaded.

A request that EventStore refuses before it reads the body is not counted. These are `401`, `413`, `415`, and a body that does not decompress.

## Field mapping

The record, its scope and its resource give the Attributes of a Log, in one flat namespace with no prefix. `severity_number` gives the Level, `body` gives the message, and `service.name` gives the service. The table of the [ADR 0002 amendment](../adr/0002-logs-scoped-by-project-flat-attributes.md#amended-how-an-otlp-log-record-becomes-a-log-2026-11-23) lists every rule. It is the one place that has the mapping.

## Settings

### A Collector

Use the `otlp_http` exporter. The older name `otlphttp` still works, but the Collector replaced it. Set `endpoint` to the Ingest URL. The Collector adds `/v1/logs` to it.

This is the config file of the Collector tab. It runs as it is:

```yaml
receivers:
  otlp:
    protocols:
      grpc: {}
      http: {}

exporters:
  otlp_http:
    endpoint: https://ingest.example.com
    headers:
      Authorization: "Bearer <key>"

service:
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [otlp_http]
```

The receivers take OTLP on port 4317 (gRPC) and port 4318 (HTTP). The Collector sends to EventStore over HTTP only. To send JSON, add `encoding: json` to the exporter.

### An SDK

This is the text of the SDK tab. Set these environment variables in the process of the application:

```
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://ingest.example.com/v1/logs
OTEL_EXPORTER_OTLP_LOGS_HEADERS=Authorization=Bearer%20<key>
OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf
```

The header value has `%20` for the space, as the OpenTelemetry specification asks.

Use `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`, not `OTEL_EXPORTER_OTLP_ENDPOINT`. The second variable also sets the endpoint of traces and metrics, which EventStore does not take. An SDK uses the first one as it is, so it has the `/v1/logs` path in it.

## Related

- [The Query language](query-language.md): how to find these Logs.
- [ADR 0002](../adr/0002-logs-scoped-by-project-flat-attributes.md): why the Attributes share one flat namespace.
