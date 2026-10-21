# 04: Text matches run last within an and

**What to build:** When the Query compiler writes an `and`, conditions with no text match come first and those with one after, each group in its written order. That covers an `and` the member wrote and one made by pushing a negation through an `or`. A Query's leading `where` and `search` stages are ordered the same way, a `search` counting as a text match. `or` isn't reordered. So `message contains "x" and rare_key == "y"` matches `message` only on the Logs where `rare_key` is `y`, whichever order it is written in, and the answers don't change. See the [spec](../spec.md)'s Implementation Decisions.

It waits for 03 because both change the same condition writer. Measure the gain with 03's matching in place: if the probe shows little left to gain, say so in the Comments before building it.

**Blocked by:** 01, 03.

**Status:** wontfix

- [ ] The same Query, written with its conditions in each order, lists the same Logs (root tests through the Query route). The conditions are:
  - a text match on `message`;
  - a condition on an Attribute only in the maps;
  - one on a promoted Attribute;
  - one on a key promoted partway through a day.
- [ ] Several `where` and `search` stages give the same Logs as before
- [ ] Negated `or`s with a text match give the same Logs as before
- [ ] The existing Query, Chart and Monitor root tests pass
- [ ] The probe's text-match and map Attribute timings in both orders, before and after, are in this ticket's Comments

## Comments

### 2026-10-21 little left to gain; not built

Measured before building, with 03's matching in place, as the ticket asks. The probe of ticket 01 (300,000 Logs, 2 threads, ms, best of 3; Logs found in brackets) now also times a table run, `… | summarize count()` through `ScanLogTable` without the bin cache, which ticket 02 doesn't touch. Two runs of this branch, back to back with nothing reordered, so each order below is run as written:

| Read | Hot table, run 1 | Hot table, run 2 | Log file, run 1 | Log file, run 2 |
|---|---|---|---|---|
| Log rows, `message contains "zebrafish" and shop == "s500"` | 158 (12) | 123 (12) | 349 (12) | 266 (12) |
| Log rows, `shop == "s500" and message contains "zebrafish"` | 146 (12) | 127 (12) | 370 (12) | 252 (12) |
| Table, text match first | 97 (12) | 88 (12) | 283 (12) | 199 (12) |
| Table, text match last | 118 (12) | 86 (12) | 300 (12) | 192 (12) |
| Table, `search "zebrafish" \| where shop == "s500"` | 293 (12) | 261 (12) | 298 (12) | 191 (12) |
| Table, `where shop == "s500" \| search "zebrafish"` | 325 (12) | 266 (12) | 256 (12) | 193 (12) |
| Table, `where shop == "s500"` alone | 41 (300) | 42 (300) | 65 (300) | 55 (300) |
| Log rows, `message contains "zebrafish"` alone | 167 (12) | 133 (12) | 386 (12) | 248 (12) |

- In every path, both orders cost the same within the run-to-run noise (about 20%), with no direction. A text match and a map Attribute condition cost about what the text match costs alone, whichever is written first, and the `search` stage likewise.
- Scratch check, not kept: one `count(*)` straight over a hot table of the same 300,000 Logs. DuckDB keeps both conditions in the scan's filters, in the order written. With `ILIKE` the order mattered, as the spec found: 553 ms with the text match first, 76 ms with it last. With 03's `contains(lower(message), …)`, it is 82 ms first and 68 ms last (map condition alone 34 ms, text match alone 86 ms). 03 made the match itself cheap, and most of what is left is reading `message` at all, which the order doesn't avoid. So at best the order saves about 15% of a direct scan of the hot table, and nothing that shows through the Query route's Log rows or table runs.
- So the reorder isn't built. The probe keeps its table runs in both orders, so the gain can be checked again after 02, or after a DuckDB upgrade.

### 2026-10-21 on top of 02: still little to gain

The same probe, rerun twice back to back after merging 02 (`feature/log-read-speed` at 6e775b0), still with nothing reordered. The two written orders stand in for the reorder off and on. 300,000 Logs, 2 threads, ms, best of 3; Logs found in brackets, run 1 / run 2.

| Read | Hot table | Log file |
|---|---|---|
| Log rows, `message contains "zebrafish" and shop == "s500"` | 143 / 142 (12) | 377 / 406 (12) |
| Log rows, `shop == "s500" and message contains "zebrafish"` | 143 / 142 (12) | 404 / 377 (12) |
| Table, text match first | 106 / 86 (12) | 213 / 203 (12) |
| Table, text match last | 84 / 87 (12) | 196 / 227 (12) |
| Table, `search "zebrafish" \| where shop == "s500"` | 263 / 258 (12) | 194 / 201 (12) |
| Table, `where shop == "s500" \| search "zebrafish"` | 265 / 257 (12) | 201 / 203 (12) |
| Table, `where shop == "s500"` alone | 40 / 37 (300) | 54 / 58 (300) |
| Log rows, `message contains "zebrafish"` alone | 142 / 139 (12) | 375 / 356 (12) |
| Log rows, newest page, no Query | 46 / 48 (200) | 93 / 97 (200) |

- 02 shows in the newest page (about 320 to 400 ms before), but not in the order of conditions: each pair still costs the same, within noise, in Log rows and in tables, for the hot table and Log files.
- A text match with a map condition costs what the text match costs alone, whichever is written first. So the reorder still isn't built.

**2026-10-21, closed as `wontfix` with the user.** On top of 02 and 03, both written orders cost the same through the Query route, in Log rows and table runs, in the hot table and Log files (figures above). The reorder wasn't built. The probe keeps timing both orders, so a DuckDB upgrade can recheck it.
