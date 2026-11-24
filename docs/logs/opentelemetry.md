# Sending Logs and metrics with OpenTelemetry

An OpenTelemetry Collector or SDK sends Logs and metrics to EventStore over OTLP/HTTP. Each request goes to one Project. The Logs show in the Logs explorer a few seconds later. EventStore stores each metric data point as one Log, so metrics show there too.

The Logs explorer of a Project with no Logs has two OpenTelemetry tabs in its Get started panel: one for a Collector and one for an SDK. Each tab shows the settings below, for Logs and metrics, with the Ingest URL and the Project's key filled in.

## The route

```
POST <Ingest URL>/v1/logs
POST <Ingest URL>/v1/metrics
```

- Send Logs to `/v1/logs` and metrics to `/v1/metrics`. EventStore does not take traces.
- Use OTLP over HTTP. EventStore does not take gRPC, so an exporter must use `http/protobuf` or `http/json`, not `grpc`.
- The Project is the Project of the key.
- Both routes take the same keys, encodings and limits, and give the same statuses. The texts of the `400` answers differ by signal. Both routes take every metric type and refuse none.

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
| `application/x-protobuf` | An `ExportLogsServiceRequest` or an `ExportMetricsServiceRequest` in protobuf. This is the default of a Collector and of an SDK. |
| `application/json` | The same request in OTLP JSON. Trace ids and span ids are hex. |

EventStore ignores parameters, such as `; charset=utf-8`. Any other Content-Type gets `415`.

The body can be compressed. Set `Content-Encoding` to `gzip`, `deflate`, `br` or `zstd`. A Collector uses `gzip` unless you change it. A body can use one coding only.

The limits are the same as for the other Log routes. A metrics request is a Log batch, so they apply to it as they do to Logs:

- 10 MiB as sent.
- 50 MiB after decoding.

A body over a limit gets `413`. One request is stored as one Log batch, so keep each request well below the limits.

## The answer

A `200` means the Logs are stored. The body is an `ExportLogsServiceResponse` (or an `ExportMetricsServiceResponse`, for metrics) in the encoding of the request.

When a Log is over 1 MB, EventStore does not store it. The response then has `partial_success`:

- `rejected_log_records` is the number of Logs not stored.
- `error_message` says why.

EventStore stores the other Logs of the request. A Log that has a field cut to a limit is stored, so it is not rejected.

For metrics, the 1 MB limit is on the Log that a data point makes, so it is rare. The response then has `partial_success` with `rejected_data_points`, the number of data points not stored, and an `error_message`. The other data points of the request are stored.

## Errors

| Status | When | A Collector |
| --- | --- | --- |
| `400` | The body does not decode as its Content-Type or Content-Encoding. | Drops the data. |
| `401` | There is no key, or the key is not known. | Drops the data. |
| `403` | The install refuses plain HTTP that a load balancer forwarded (`ingest_refuse_forwarded_http`). Use an `https://` URL. | Drops the data. |
| `413` | The body is over a limit. | Drops the data. |
| `415` | The Content-Type or the Content-Encoding is not one of the above. | Drops the data. |
| `500` | An internal error, for example the lookup of the key fails. | Drops the data. |
| `503` | EventStore is overloaded or shuts down. The answer has `Retry-After: 5`. | Sends the request again after the wait. |

The OTLP/HTTP specification makes a sender retry `429`, `502`, `503` and `504`, and drop the data on any other error. So a sender does not retry a `500` or a `4xx` other than `429`. A Collector 0.165.0 does this. A test showed it for `400`, `401`, `415` and `503`. The `500` follows from the specification and was not tested. The Collector logs a warning when a `200` has a `partial_success`, and it continues to send. The Collector does the same for `rejected_data_points` in a metrics response. This was not tested for metrics.

A request that a Collector sends again after a timeout can store its Logs twice. This is true for every Log route.

## Ingest counts

Each request that EventStore reads adds to the Project's Ingest counts:

- One request.
- One Log received for each log record, or for each metric data point. A metric data point is a Log, so it counts as one.
- One unreadable Log for each Log that was not stored.
- The bytes, after decoding and as sent.

A body that does not decode as OTLP counts as one request with its bytes and no Logs. A request that EventStore turns away as overloaded is counted as overloaded.

A request that EventStore refuses before it reads the body is not counted. These are `401`, `403`, `413`, `415`, and a body that does not decompress.

## Metrics

EventStore has no metric store. Each metric data point is stored as one Log, so the Logs explorer, Dashboards and Monitors read metrics as they read Logs. [ADR 0002](../adr/0002-logs-scoped-by-project-flat-attributes.md#amended-how-an-otlp-metric-data-point-becomes-a-log-2026-11-24) has the table of every rule. It is the one place that has the mapping.

- `message` is the metric name. `time` is the time of the data point. `level` is `info`.
- `metric.value` is the number of a sum or a gauge. It is always a double. A histogram or a summary has `metric.count` and `metric.sum` in its place, and a histogram also has `metric.min` and `metric.max`. An exponential histogram is the same as a histogram. A histogram has `metric.sum`, `metric.min` and `metric.max` only when the sender sends them. A data point with no recorded value has no values. EventStore does not keep the buckets.
- `metric.type`, `metric.unit`, `metric.temporality` and `metric.start_time` say what kind of metric it is. The resource, scope and data point attributes are Attributes of the Log, as they are for a log record.

This Query adds up one metric for each value of an Attribute:

```
where message == "<metric name>"
| summarize sum(metric.value) by <attribute>
```

**Use delta temporality.** A delta value is the change since the last export, so a `sum` adds the values up correctly. EventStore stores a cumulative value as it comes: each value is the total since `metric.start_time`. A `sum` of cumulative values counts each part of the total again on each export, so the result is too large. For a cumulative metric, read the last value of each series. The SDK tab sets `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=delta`. A Collector that gets cumulative metrics can change them to delta with the `cumulativetodelta` processor. This processor is in the contrib Collector (`otelcol-contrib`), not in the core image that the Get started tab uses. It was not tested here. With the delta preference, an SDK still sends up-down counters as cumulative, so a `sum` of those is wrong. Use the last value.

## Field mapping

The record, its scope and its resource give the Attributes of a Log, in one flat namespace with no prefix. `severity_number` gives the Level, `body` gives the message, and `service.name` gives the service. The table of the [ADR 0002 amendment](../adr/0002-logs-scoped-by-project-flat-attributes.md#amended-how-an-otlp-log-record-becomes-a-log-2026-11-23) lists every rule. It is the one place that has the mapping.

## Settings

### A Collector

Use the `otlp_http` exporter. The older name `otlphttp` still works, but the Collector replaced it. Set `endpoint` to the Ingest URL. The Collector adds `/v1/logs` to it for Logs, and `/v1/metrics` for metrics.

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
    metrics:
      receivers: [otlp]
      exporters: [otlp_http]
```

The receivers take OTLP on port 4317 (gRPC) and port 4318 (HTTP), on 127.0.0.1 only. To take OTLP from other hosts or containers, set an `endpoint` such as `0.0.0.0:4318` on the protocol. The Collector sends to EventStore over HTTP only. To send JSON, add `encoding: json` to the exporter.

### An SDK

This is the text of the SDK tab. Set these environment variables in the process of the application:

```
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://ingest.example.com/v1/logs
OTEL_EXPORTER_OTLP_LOGS_HEADERS=Authorization=Bearer%20<key>
OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=https://ingest.example.com/v1/metrics
OTEL_EXPORTER_OTLP_METRICS_HEADERS=Authorization=Bearer%20<key>
OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=delta
```

The header value has `%20` for the space, as the OpenTelemetry specification asks.

Use `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` and `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, not `OTEL_EXPORTER_OTLP_ENDPOINT`. `OTEL_EXPORTER_OTLP_ENDPOINT` also sets the endpoint of traces, which EventStore does not take. An SDK uses the first two as they are, so they have the `/v1/logs` and `/v1/metrics` paths in them.

## Related

- [The Query language](query-language.md): how to find these Logs.
- [ADR 0002](../adr/0002-logs-scoped-by-project-flat-attributes.md): why the Attributes share one flat namespace.
