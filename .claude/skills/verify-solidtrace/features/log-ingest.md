# Log ingest

Log ingest is how Logs get into a Project. A shipper, an application, a Sentry SDK or an OpenTelemetry Collector sends them to EventStore's ingest port with the Project's key. EventStore stores the Logs of each request as one Log batch. Then Log processing moves them into ClickHouse. They show in the Logs explorer. The Project's Ingest counts rise by the request and its Logs.

## Sub-features

- `logs-ingest-json`: `POST /api/<project id>/logs` takes a JSON array, a JSON object or NDJSON, with the key as a Bearer token or as `sentry_key`.
- `logs-ingest-key-only`: `POST /api/logs` takes the same bodies, and the key alone says which Project.
- `logs-ingest-envelope`: `POST /api/<project id>/envelope` stores the log items of a Sentry envelope as Logs.
- `logs-ingest-otlp`: `POST /v1/logs` takes an OTLP/HTTP request, as protobuf (gzip) or as JSON. The Project is the key's Project. A bad key gives 401. Another content type gives 415. A body that does not decode gives 400.
- `logs-ingest-collector`: a real OpenTelemetry Collector exports to `/v1/logs` in both encodings, through `bin/otel-dev`. The flow `logs-otlp.spec.ts` posts the two fixtures of a real Collector.
- `logs-ingest-counts`: each request and each Log show in the Project's Ingest counts and in the Overview's Received number.

## How to get to it (user POV)

- A shipper or an application: `POST /api/<project id>/logs` with `Authorization: Bearer <project key>` and an NDJSON, JSON array or JSON object body.
- A shipper that has only the key: `POST /api/logs` with `Authorization: Bearer <project key>`.
- A Sentry SDK: its envelope goes to `POST /api/<project id>/envelope`. The log items in it are Logs. The key is in `X-Sentry-Auth`, in `Authorization` or in `?sentry_key=`.
- An OpenTelemetry Collector or SDK: the exporter `otlp_http` with the endpoint set to the Ingest URL, and the header `Authorization: Bearer <project key>`. It posts to `<Ingest URL>/v1/logs`.
- The result, in the Logs explorer at `/<org>/projects/<project>/logs`, and in the Overview at `/<org>/overview`.

## Driving it with verify

Preconditions:

- The baseline stack is up from this checkout's own script (see the Gotchas), and `"$V" doctor` is all `ok`. In the commands, `4510` and `4511` are the ingest and Console ports of slot 1.
- Docker runs. The images `otel/opentelemetry-collector:0.165.0` and `telemetrygen` of `docker-compose.dev.yml` are there, or Docker can pull them. No container of the `otel` profile runs: `docker ps` shows no `solidtrace-otel-*`.
- Set the shell once, from the checkout's root:

  ```bash
  ID=$("$V" run -- bin/rails runner 'print Project.find_by!(slug: "solid-trace-project").id')
  CH() { curl -s 'http://127.0.0.1:4201/?user=solidtrace' --data-binary "$1"; }
  FIX="$(git rev-parse --show-toplevel)/event_store/ingest/normalise/testdata/otlp"
  CH "SELECT sum(requests), sum(logs_received) FROM solidtrace_verify_s1.log_ingest_counts"
  ```

  Write down the two numbers, for `logs-ingest-counts`. The probes below add to them.

- **JSON route.** `curl -s -X POST http://localhost:4510/api/$ID/logs -H 'Content-Type: application/json' -H 'Authorization: Bearer testkey123' --data-binary '[{"level":"warn","message":"ingest probe json","service":"ingest-probe"}]'`. Pass: `{"accepted":1,"dropped":0,"truncated":0}`.
- **NDJSON.** `curl -s -X POST http://localhost:4510/api/$ID/logs -H 'Content-Type: application/x-ndjson' -H 'Authorization: Bearer testkey123' --data-binary $'{"message":"ingest probe ndjson 1","service":"ingest-probe"}\n{"message":"ingest probe ndjson 2","service":"ingest-probe"}'`. Pass: `{"accepted":2,"dropped":0,"truncated":0}`.
- **Key only.** `curl -s -X POST http://localhost:4510/api/logs -H 'Content-Type: application/json' -H 'Authorization: Bearer testkey123' --data-binary '{"message":"ingest probe key only","service":"ingest-probe"}'`. Pass: `{"accepted":1,"dropped":0,"truncated":0}`. Without the header, the answer is 401.
- **Sentry envelope.** `printf '%s\n%s\n%s\n' '{"sent_at":"2026-11-24T00:00:00Z"}' '{"type":"log","item_count":1,"content_type":"application/vnd.sentry.items.log+json"}' "{\"items\":[{\"timestamp\":$(date +%s),\"level\":\"info\",\"body\":\"ingest probe envelope\"}]}" | curl -s -X POST "http://localhost:4510/api/$ID/envelope?sentry_key=testkey123" --data-binary @-`. Pass: `OK`. The Log has no service, so find it by its message.
- **OTLP protobuf, as a Collector sends it.** `gzip -c "$FIX/collector.pb" | curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:4510/v1/logs -H 'Content-Type: application/x-protobuf' -H 'Content-Encoding: gzip' -H 'Authorization: Bearer testkey123' --data-binary @-`. Pass: `200`. The request has 3 Logs of service `checkout`.
- **OTLP JSON.** `curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:4510/v1/logs -H 'Content-Type: application/json' -H 'Authorization: Bearer testkey123' --data-binary @"$FIX/collector.json"`. Pass: `200`, again with 3 Logs.
- **In the explorer.** Wait for Log processing. Then run `"$V" snap --path "/solid-trace-org/projects/solid-trace-project/logs?range=1h&q=%5B'solid-trace-project'%5D%0A%7C%20where%20service%20%3D%3D%20%22ingest-probe%22" --name logs-ingest --wait-text 'ingest probe ndjson 2'`. Pass: exit 0, and `logs-ingest.aria.txt` has `ingest probe json`, both NDJSON messages and `ingest probe key only`. For the OTLP Logs, use `service%20%3D%3D%20%22checkout%22`, `--name logs-ingest-otlp` and `--wait-text 'order placed'`, so the first proof stays. Use `from=2026-11-22T00:00:00Z&to=$(date -u -d '+1 hour' +%FT%TZ)` in place of `range=1h`. The fixtures keep their own time, 2026-11-23, until it is older than the Project's Retention.
- **Stored.** `CH "SELECT service, count() FROM solidtrace_verify_s1.logs WHERE service IN ('ingest-probe', 'checkout') GROUP BY service ORDER BY service"`. Pass: `checkout` has 6 (3 for each OTLP request) and `ingest-probe` has 4. `CH "SELECT count() FROM solidtrace_verify_s1.logs WHERE message = 'ingest probe envelope'"` gives `1`.
- **Ingest counts.** `CH "SELECT sum(requests), sum(logs_received) FROM solidtrace_verify_s1.log_ingest_counts"` once more. Pass: the sums are the numbers from the start, plus 6 requests and 11 Logs. The six requests above carry 1, 2, 1, 1, 3 and 3 Logs.
- **Refusals on `/v1/logs`.** The OTLP JSON call with no `Authorization` header gives `401`. With `-H 'Content-Type: text/plain'` and the key it gives `415`. With the protobuf body and `Content-Type: application/json` it gives `400`. The `400` adds one request to the Ingest counts, because EventStore read its body. The `401` and the `415` add nothing. So run the refusals after you read the counts.
- **A real Collector.** `bin/otel-dev 50 http://127.0.0.1:4510 testkey123`, from the checkout's root. It starts the `otel` profile (Collector and `telemetrygen`) and sends 50 Logs through the Collector. It waits until each exporter has sent them, and it stops its containers. Pass: exit 0 and the lines `protobuf with gzip: 50 Logs` and `JSON, no gzip:      50 Logs`. Then `"$V" snap --path "/solid-trace-org/projects/solid-trace-project/logs?range=15m&q=%5B'solid-trace-project'%5D%0A%7C%20where%20service%20%3D%3D%20%22telemetrygen%22" --name logs-collector --wait-text 'the message'`. Pass: exit 0, and the summary line in `logs-collector.aria.txt` says `100 Logs` (50 for each exporter). Run it on a stack with no earlier Collector run. Else the count is a multiple of 100. After the run, `docker ps` shows no `solidtrace-otel-*`.
- **A Collector Log's Fields.** `CH "SELECT service, environment, level, message, attributes FROM solidtrace_verify_s1.logs WHERE service = 'telemetrygen' LIMIT 1 FORMAT Vertical"`. Pass: `environment` is `dev`. The `telemetrygen` command in the compose file sets it, as the resource attribute `deployment.environment.name`. The core Collector image has no resource processor, so another sender to this Collector gets no environment. `level` is `info`, `message` is `the message`, and the Attributes hold `app` and no `service.name`.
- **In the inspector, and the Overview.** `"$V" flow -- e2e/flows/logs-otlp.spec.ts`. Pass: both tests pass. The trace shows the two posts and the explorer with 6 more `checkout` Logs. It shows a Log in the inspector with `environment` staging, `trace_id` and six Attributes. It shows the Overview with the Received Logs and requests up.
- **Proof.** Keep the `snap` PNG and ARIA files, the trace of the `logs-otlp` flow, the `bin/otel-dev` output and the ClickHouse reads.

## Gotchas

- **Use this checkout's own script.** `bin/verify` builds the checkout that it lives in. A copy from another checkout gives a stack of the wrong code. For example, a stack from `main` has no `/v1/logs` and answers 404. In a worktree, set `V="$(git rev-parse --show-toplevel)/.claude/skills/verify-solidtrace/bin/verify"` there. Check `tree` in the output of `"$V" doctor`.
- **A fresh worktree is not ready.** Before `"$V" up`, run `mise trust` in the worktree and `bun install` in `console/`. Then do one of two things. Run `vrl/build` in `event_store/`, which takes minutes. Or copy the built VRL library from the main checkout: `cp -r <main>/event_store/vrl/lib event_store/vrl/`.
- **A Collector retries a 503, and drops 401, 400 and 415.** An overloaded ingest answers 503 with `Retry-After: 5`. The Collector sends the same batch again after 5 s, so no Log is lost. A refusal (401, 400, 415) is final. The Collector logs `Exporting failed. Dropping data.` and does not retry. When a Log is not sent, `bin/otel-dev` exits 1 and prints those Collector log lines.
- **200 with a partial success is a warning, not a failure.** When EventStore drops Logs over 1 MB, the answer is 200 with `partial_success`. The Collector logs `Partial success response`. It does not send the data again.
- **The key goes in the exporter's headers.** `endpoint` is the bare Ingest URL with no path. The Collector adds `/v1/logs`. `headers: Authorization: Bearer <key>` sends the key as written.
- **One Collector at a time.** `bin/otel-dev` uses fixed host ports and the container names `solidtrace-otel-collector` and `solidtrace-otel-telemetrygen`. It refuses to start when one of them exists. Stop a left-over container by name with `docker rm -f`. Never run `docker compose down` on `docker-compose.dev.yml`, because that project also holds the shared ClickHouse.
- **Use 127.0.0.1 in the ingest URL for the Collector.** The Collector shares the host's network. `bin/otel-dev` changes `localhost` to `127.0.0.1` itself.
- **The fixtures keep their time.** The Logs of `collector.pb` and `collector.json` have the time 2026-11-23. A range of `1h` does not show them. Each run of the recipe or of `logs-otlp.spec.ts` adds 6 more `checkout` Logs. The flow counts the change, not the total.
- **The Received number is by arrival.** The Overview counts Logs by the second that they arrived. The counts are written about once a second. Reload until the number changes. Do not use a fixed sleep. Log processing stores the Logs about as often.
