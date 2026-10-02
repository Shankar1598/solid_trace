# 03: Delivery outcomes: failed, retried, skipped, cleaned up

**What to build:** Every outbox row ends in a recorded outcome that reflects what happened, never a false `sent`.

- **Adapters report outcomes:** the Slack, PagerDuty and email adapters report success or failure instead of rescuing errors to nil.
  - For Slack and PagerDuty, a non-2xx response, a connection error or a timeout is a failure.
  - For email, a successful hand-off to the mailer queue is a success.
  - The duplicated HTTP transport and URL helpers in the Slack and PagerDuty adapters merge into one helper that only the adapters use.
- **Failure and retries:** a failed delivery is recorded as `failed` with the provider's error, and every delivery attempt increments `attempts`. Later ticks pick up failed rows again until they reach the cap of 5; at the cap they stay `failed` and are no longer picked up. A failed batch message marks every row in the batch as failed.
- **Skipped rows:** a missing or inactive Integration, a missing Issue, or a provider with no webhook URL, routing key or recipients is recorded as `skipped` with that reason. Skipped rows are never retried.
- **Cleanup:** the maintenance job deletes `sent` and `skipped` rows older than one day, and `failed` rows at the attempt cap older than seven days.

See spec: "Delivery outcome", "Outcome states on the outbox row", "Bounded retries", "Outbox cleanup" (user stories 10–17, 24, 33).

**Blocked by:** 02: Integration notification module owns the new-Issue path

**Status:** ready-for-agent

- [ ] Slack 500, connection refused and timeout are each recorded as `failed` with the error, then retried by later ticks until `attempts` reaches 5
- [ ] At the attempt cap a row stays `failed` and is not picked up again
- [ ] A failed batch message marks every row in the batch `failed`
- [ ] A missing Integration, an inactive Integration, a missing Issue and an unconfigured provider are each recorded as `skipped` with a reason and never retried
- [ ] Provider adapter tests gain success and failure cases for 2xx, non-2xx, connection error and timeout, per provider
- [ ] Slack and PagerDuty share one internal HTTP/URL helper
- [ ] Cleanup removes `sent`/`skipped` rows older than 1 day and capped `failed` rows older than 7 days, and keeps everything else
- [ ] Tests go through the module's interface and the cleanup job; external systems are faked only with WebMock and ActionMailer test deliveries
