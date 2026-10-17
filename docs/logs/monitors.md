# Monitors

A Monitor runs a Query on one of your Organization's Projects every few minutes and tells its Integrations what it finds. Any member of the Organization can make, change, turn off, run and delete Monitors, from Monitors in the sidebar or from Create monitor beside a chart in the Logs explorer.

There are two kinds:

- A **Threshold monitor** compares a number with a threshold, such as "more than 50 errors in the last 5 minutes". It is **firing** while the number passes the threshold and **ok** otherwise.
- A **Match monitor** sends a message when new Logs match its Query, such as "any Log from billing with level fatal".

## Threshold monitors

The Query must end in a `summarize` with one number per row, or in `count`:

```
where level == "error"
| summarize count() by service
```

The number can be any aggregate, or arithmetic over aggregates such as an error rate. Saving checks the Query with EventStore and runs it once, so a Query that doesn't give one number per row, or that EventStore can't run, is refused with the reason. A Query grouped by a time bin is refused too: each bin would become a group that fires and resolves as the bins roll over.

You pick:

- **Run every:** 1 to 1,440 minutes.
- **Over the last:** the range each run reads, 1 minute to 7 days.
- **The operator and threshold:** `>`, `>=`, `<` or `<=` a number.
- **Notify by group:** see Groups, below.
- **Alert on no data:** see No data, below.

The New page shows the Query over the last 24 hours with the threshold drawn as a line, and the groups that would fire now.

### Windows and the 60 s lag

A run at time T reads the Logs from T minus the range minus 60 seconds up to T minus 60 seconds. Logs reach the explorer a few seconds after they arrive, once Log processing has handled them. The 60 seconds keep a run from reading the newest Logs before they are all there, which would make a count look low.

### Groups

Each row of the table is a **group**, named by its columns other than the number, such as `service=api, level=error`. With **notify by group** on, each group fires and resolves on its own, and each gets its own message. With it off, all rows make one group, and the worst number decides: the largest for `>` and `>=`, the smallest for `<` and `<=`.

A group moves from ok to firing, and back:

- **ok to firing** sends a firing message.
- **firing to ok** sends a resolved message.
- A group that stays firing sends nothing more, however many runs it stays firing for.
- A firing group with no row in a run is back to ok, and sends a resolved message that says so.

### No data

When the table has no rows, nothing fires: no Logs means no number to compare. Turn on **alert on no data** to have an empty table fire the Monitor instead, as one group. It resolves when the table has rows again. With notify by group on, a group that loses its row is ok; only a wholly empty table counts as no data.

### Too many groups

EventStore answers at most 10,000 rows. A run whose table was cut there can't compare every group, and each run would see a different part. That run is an error, and it changes no state. Group by fewer columns, or filter the Logs before the `summarize`.

## Match monitors

A Match monitor's Query returns Logs: filters only, with no `summarize`. Saving it starts it from the Logs arriving after the save. Each run then reads the Logs that arrived since the last run, in the order they were stored, so a Log that arrives late, with an old time, is still read once.

When Logs match, one message lists the newest 10, says how many more there are, and links to them in the explorer.

### The re-arm after timeouts

A run stops after 30 seconds. A Match monitor that falls far behind, after an EventStore outage say, may need longer than that to read what arrived since its last run. After 3 such timeouts in a row it starts again from the Logs arriving now and skips the ones in between. That run shows as an error that says Logs were skipped, and the next message says when. Any successful run starts the count again.

Turning a Match monitor off and on again also starts it from the Logs arriving now, so it doesn't report what arrived while it was off.

## Runs

A scheduler checks every minute and runs each Monitor that is on and due. A Monitor runs within a minute of being saved, then every interval. One run of a Monitor goes at a time: Run now, on the Monitor's page, runs it at once and shows what it found, unless a scheduled run is going right then.

When EventStore is down, times out or refuses the Query, the run is an error: it changes no state, sends nothing, and the Monitor runs again at the next tick. A firing Monitor whose last run failed still shows as Firing, with the failure beside it.

A Monitor's page shows each group's state and its last 100 runs.

**Turning a Monitor off** stops its runs. **Deleting** it removes it and its runs. Either one resolves each firing group, so a PagerDuty incident it opened is closed. Changing a Threshold monitor into a Match monitor does the same. Turned back on, a group still past the threshold fires again.

**Unticking an Integration** on a Monitor's form sends that Integration a resolve for each firing group, so its PagerDuty incident is closed too. The groups stay firing for the Integrations still ticked.

## Notifications

A Monitor tells the Integrations you pick on its form. Messages go through the same outbox as Issue notifications: a message reaches an Integration about 10 seconds after the run, and each Integration gets at most one message a minute, so a resolve can follow its firing message by about a minute. The messages about one group go in order. If one fails, the ones after it wait, so a resolve never arrives before its firing message. A failed message is retried every minute for 6 hours.

Every message links to the explorer with the Monitor's Query: over the range the run read, or for a Match message over the Logs it found.

### Slack

A Block Kit message: the title ("Firing: Checkout errors (service=api)" or "Resolved: …"), what the run saw ("count_ was 120 over the last 15 minutes, above the threshold of 100"), the Project, group, value, threshold and range, and a button to the explorer. A Match message lists the Logs.

### PagerDuty

Firing sends a `trigger` event and resolved sends a `resolve` event, with the same dedup key, so the resolve closes the incident the trigger opened:

- `solid-trace-monitor-<id>` for a Monitor with one group
- `solid-trace-monitor-<id>-<group>` for a group, where the group is its columns as JSON, or a SHA-256 of them when the key would pass PagerDuty's 255 characters

A Match message is a `trigger` on `solid-trace-monitor-<id>`, so repeated matches join one open incident. A Match monitor never resolves it: resolve it in PagerDuty.

Events go to PagerDuty's Events API v2, `https://events.pagerduty.com/v2/enqueue`. Set `PAGERDUTY_EVENTS_URL` to send them elsewhere, such as a local stub in development. Plain `http` works there too.

### Email

An email to the Integration's recipients, with the subject "[SolidTrace] " and the message's title, and the same text, fields and link.

## Known limits

- **No data during a processing backlog.** A run reads Logs once Log processing has handled them. When processing falls more than a minute behind, the newest Logs aren't readable yet, so a count looks low and **alert on no data** can fire while Logs are still arriving. It resolves once processing catches up.
- **A turned-off Integration hears nothing.** Turning an Integration off stops every message to it, resolves too, so a PagerDuty incident a Monitor opened stays open: resolve it in PagerDuty. An Integration ticked while a group is firing gets no firing message for it, only the resolve.
- **A Match link may miss a late Log.** The link in a Match message starts a minute before the run's starting point, or at the oldest Log it lists if that is older. A Log stored late, with a time older than that and not among the 10 listed, falls outside the link, though the message counts it.
