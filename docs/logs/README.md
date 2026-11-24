# Logs docs

- [The Query language](query-language.md): how to write a Query that finds and shapes a Project's Logs.
- [SQL queries and the `logs`, `events` and `spans` tables](sql.md): SQL mode in the Logs explorer, on instances that turn it on.
- [Logs in a bucket](object-store.md): moving older Logs to S3, GCS or MinIO, and setting it up.
- [Sending Logs and metrics with OpenTelemetry](opentelemetry.md): the OTLP/HTTP routes `POST /v1/logs` and `POST /v1/metrics`, their keys, answers and errors, how a metric data point is stored as a Log, and the settings for a Collector or an SDK.
