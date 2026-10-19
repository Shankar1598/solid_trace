# 03: Vector ships SolidTrace's logs

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md), decisions 1, 2 and 8
**Blocked by:** [01](01-key-only-log-ingest.md), [02](02-seed-the-meta-organization.md)

**What to build:** a Vector service on VM installs that sends every SolidTrace container's output to the Meta Organization's Project.

- [ ] A `vector` service in `deploy/docker-compose.yml`: a pinned `timberio/vector` image at least two weeks old, the host network, `/var/run/docker.sock` read-only, its config from `deploy/vector/`, always on.
- [ ] `deploy/vector/vector.yaml`:
  - a `docker_logs` source limited to this Compose project's containers, without Vector's own;
  - a transform that parses JSON lines and logfmt lines, and the Level prefix of Rails' lines, into time, level and message, sets `service` to the Compose service name and `container` to the container's name;
  - an `http` sink to `http://127.0.0.1:4000/api/logs`, NDJSON or a JSON array, with the key from `deploy/.env` as a bearer token, retrying on 503.
- [ ] EventStore: no colour when its output is not a terminal.
- [ ] Console: production log lines start with their Level (`INFO [request-id] message`).
- [ ] `deploy/install` and the README mention the Meta Organization and where SolidTrace's own Logs are.
- [ ] Checked: `vector validate` on the config; `docker compose config`; sample lines of each Service through the transform (`vector tap` or a unit test in the config) come out with the right fields; Logs from a running stack reach the Project.
