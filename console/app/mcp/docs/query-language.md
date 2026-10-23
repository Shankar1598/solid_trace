# The Query language

A Query finds and shapes a Project's Logs. You pick the Project and the time range in the Logs explorer's bar, not in the Query text. A Query can narrow the time range, but it can't widen it or read another Project's Logs.

A Query is a list of stages joined by `|`. Each stage takes the rows from the stage before it:

```
where level == "error"
| summarize count() by service, bin(time, 5m)
| sort by count_ desc
```

You can start a Query with `logs |`, but you don't have to. A dataset name in brackets, such as `['my-dataset'] |`, means the same thing, so queries pasted from other tools often work as they are. `//` starts a comment that runs to the end of the line.

## Fields and values

A **Field** is a built-in field or an Attribute.

- **Built-in fields:** `time`, `level`, `message`, `service`, `environment`, `trace_id` and `span_id`. `time` is a datetime and the others are strings. `_time` is another name for `time`.
- **Attributes:** any other name, such as `status` or `http.method`. A dotted name is one Attribute, because nested names are flattened when Logs arrive. Quote a name that isn't a plain word: `['user agent']`.

An Attribute's values keep their own types, so one Log can hold `status` as the number `200` and another as the string `"200"`. In a comparison, the value you write decides which of these it reads:

- `status == 200` matches only the number 200.
- `status == "200"` matches only the string "200".

To match either, convert the value first, for example `toint(status) == 200` or `tostring(status) == "200"`.

**Values:**

- Strings in single or double quotes. Backslash escapes `\n`, `\t`, `\\` and quotes.
- Numbers such as `200`, `-1.5` and `1e3`.
- `true` and `false`.
- Timespans: a number and a unit, such as `30s`, `5m`, `1h`, `2d` or `100ms`.
- Datetimes, such as `datetime(2026-10-02T10:00:00Z)` or `datetime(2026-10-02)`. A datetime without a time zone is in UTC.

## Filtering: `where` and `search`

`where <condition>` keeps the rows the condition is true for. Combine conditions with `and` and `or`, and negate one with `not(…)`.

| Operator | Meaning |
|---|---|
| `==`, `!=` | Equal and not equal. Values of different types are never equal: `level == 5` matches no Log. |
| `<`, `<=`, `>`, `>=` | Order comparisons of numbers, strings or datetimes. |
| `=~`, `!~` | Equal or not equal, ignoring case. |
| `contains`, `!contains` | Contains a substring, ignoring case. On an array, the substring is looked for in the array's JSON text: `tags contains "db"` matches `["api", "db"]`. |
| `contains_cs`, `!contains_cs` | Contains a substring, with case, arrays alike. |
| `startswith`, `!startswith` | Starts with, ignoring case. |
| `startswith_cs`, `!startswith_cs` | Starts with, with case. |
| `matches regex` | Matches a regular expression (RE2 syntax), with case. |
| `in (…)`, `!in (…)` | Equal to any value in the list, or to none of them. |
| `isempty(x)`, `isnotempty(x)` | Missing or an empty string, or not. `isnull` and `isnotnull` mean the same. |

**Missing Fields.** A test of a Field a Log doesn't have is false, and its negation is true. So `!=`, `!in`, `!contains` and `not(…)` keep Logs that don't have the Field. Hiding `user == "bob"` never hides Logs without a `user`.

**Time.** `where time > ago(1h)`, `where time >= datetime(2026-10-02T10:00:00Z)` and `where time < now() - 30m` narrow the time range in the bar. A condition on time can only narrow that range, never widen it.

`search "text"` keeps the Logs whose `message` or any string Attribute contains the text, ignoring case. It doesn't look in arrays; use `where tags contains "text"` for those. This is what the Builder's search box does. `search` must come before any stage other than `where` and `search`.

## Shaping: stages that return a table

A Query that ends with only `where`, `search`, `take` and `sort by time desc` stages returns Logs, newest first, a page at a time. Any other stage makes the Query return a table. The histogram above the table still counts the Logs that pass the Query's leading filters.

| Stage | What it does |
|---|---|
| `project a, b, name = expr` | Keeps only these columns. |
| `extend name = expr, …` | Adds columns to each row. A column with an existing name replaces that column. |
| `summarize agg, … by expr, …` | Makes one row per distinct `by` value, with the aggregates. You can leave out the aggregates (`summarize by service`) or the `by` part. |
| `sort by expr [asc\|desc] [nulls first\|last], …` | Sorts the rows. `order by` means the same. The default order is descending, with missing values last when descending and first when ascending. |
| `take n` | Keeps the first n rows. For Logs these are the newest n. `limit` means the same. |
| `top n by expr [asc\|desc]` | Sorts the rows and keeps the first n. |
| `count` | Returns one row with one column, `Count`: the number of rows. |
| `distinct a, b` | Returns the distinct combinations of the columns. |

**Column names.** A column you don't name is named after the Field it reads, such as `service` or `time` for `bin(time, 5m)`. An aggregate you don't name is named after its function and Field, such as `count_`, `avg_duration` or `percentile_duration_95`. When two columns would get the same name, name one of them yourself.

**Row order.** A table you haven't sorted comes back sorted by its columns, left to right. A table of Logs you haven't sorted, such as one from `extend`, comes back newest first.

**Tables of Logs.** When a table's rows are still Logs, as after `top` or `extend`, its columns are the built-in fields, then the Attributes the Query names, then the columns you added.

**Size.** A run returns at most 10,000 rows of a table. It says when there were more.

## Aggregates (in `summarize` only)

| Function | Result |
|---|---|
| `count()` | The number of rows. |
| `countif(condition)` | The number of rows the condition is true for. |
| `dcount(x)` | **An estimate of** the number of distinct values of x. |
| `sum(x)`, `avg(x)` | The sum and the mean of a number. |
| `min(x)`, `max(x)` | The least and the greatest value: of a number, or of a string or a datetime such as `time`. |
| `percentile(x, p)` | **An estimate of** the p-th percentile of a number, for p from 0 to 100, such as `percentile(duration, 95)`. |
| `rate()` | The rows per second, averaged over the time bin: the rows' count over the seconds of the bin that lie in the range. With no time bin, over the whole range. |
| `peak_rate()` | The rows in the busiest whole second of the time bin, or of the range with no time bin. The bin must be a whole number of seconds. |

`dcount` and `percentile` are estimates, not exact counts. `dcount` uses HyperLogLog, which is typically within a few percent of the true count. Small counts are usually exact, but large ones can be off in either direction. `percentile` uses a T-Digest sketch, which is accurate near the middle of the data and less accurate in the extreme tails, such as above the 99.9th percentile. Don't use either one where you need an exact figure. For an exact distinct count, use `summarize by x | count`.

Aggregates read an Attribute as a number, except `dcount`, which counts distinct values of any type.

### Requests per second

Filter to the requests first, then chart both rates:

```
where path == "/v2/search"
| summarize rate(), peak_rate() by bin_auto(time), shop
```

The time bin is the first `by` column that bins the Logs' `time`. `rate()` by a time column that isn't such a bin, such as a `time` that `extend` changes, is an error. `rate()` divides each bin's count by its seconds, the empty ones too, so a burst inside the bin is averaged out. A bin that the range's start or end cuts is divided by its seconds in the range, so it still reads a true rate. Only a `where` on `time` before any other stage narrows the range, so put it first: over an hour, `extend x = 1 | where time > ago(10m) | summarize rate()` divides by the whole hour and reads 6 times too low.

`peak_rate()` counts the Logs in each second of the bin, as `bin(time, 1s)` puts them, and keeps the busiest. It reads each Log's own time, so it is the true busiest second at any bin size: over a month, `bin_auto(time)` makes 12-hour bins, and each one's `peak_rate_` is still the busiest single second of those 12 hours. Within groups, rates add up to the whole and peak rates don't, as groups peak in different seconds.

Both take no arguments. They are not the same as `summarize n = count() by bin(time, 1s) | summarize avg(n), max(n) by bin_auto(time)`: there, seconds with no Logs have no row, so `avg(n)` is too high.

## Scalar functions

| Function | Result |
|---|---|
| `bin(time, 5m)` | `time` rounded down to a multiple of the timespan, counted from 1970-01-01 UTC. |
| `bin(x, 10)` | A number rounded down to a multiple of 10. `floor` means the same. |
| `bin_auto(time)` | `time` rounded down to a bin size chosen from the bar's time range, giving at most about 100 bins. |
| `ago(1h)`, `now()` | The time 1 hour before the run, and the time of the run. |
| `tostring(x)` | x as a string. A whole number has no decimal point. |
| `toint(x)`, `tolong(x)` | x as a whole number, rounded toward zero. A string that isn't a number gives no value. |
| `todouble(x)`, `toreal(x)` | x as a number. |
| `tolower(s)`, `toupper(s)`, `strlen(s)` | A string in lower case, in upper case, and its length. |

Arithmetic: `+`, `-`, `*`, `/` and `%` work on numbers. You can add a timespan to a datetime or subtract one from it, as in `now() - 1h`.

Any other function is an error, and the Query box shows its line and column.

## Charts

When a Query returns a table, the Logs explorer draws it as a chart above the table, if it can:

- **Over time:** a table grouped by a time bin, such as `summarize count() by bin_auto(time), service`. The bin column is the x axis. Each number column is a series, and the string or bool `by` columns split them, so this draws a line per Service.
- **Bars:** a `summarize` with no time bin, such as `summarize count() by service`. The first string column names the bars, and any other string columns split them. The 50 largest bars are drawn. With more than 8 bars, or long names, they lie on their side.
- **A number:** a table of one row and one number, such as `summarize count()` or `count`.

Any other table shows on its own: one with no rows, with a second datetime column, or with two rows for the same bin and group.

A chart draws at most 8 series, the ones with the largest totals, and the legend says how many more there are. Click a legend entry to hide or show its series. Columns that measure different things, such as a count and an average, get a panel each over the same time axis; `rate` and `peak_rate` share one, in Logs per second. Bars and areas stack only for `count`, `countif`, `sum` and `rate`, whose values add up, and only when that column has its panel alone. So `summarize rate(), peak_rate() by bin_auto(time), shop` draws each shop's rate unstacked, in the panel it shares with `peak_rate`.

**Time bins.** `bin(time, 5m)` makes bins of the size you give. `bin_auto(time)` picks a size from the time range, giving at most about 100 bins. It reads the range after the Query's `where` stages narrow it. Bins start at multiples of their size from 1970-01-01 UTC, so the first and last bins may cover only part of the range. The chart fades them, and its tooltip says "partial bin". The chart says its bin size beside the legend, such as "1m bins", and its tooltip gives each bin's span, such as 19:02–19:03, so a count over 5-minute bins doesn't read as one per minute. A new Summarize in the Builder starts with `by bin_auto(time)`, so one click charts a count.

**Empty bins.** A bin with no Logs has no row in the table. EventStore tells the chart whether it may fill those bins (`fill_gaps`). It may when nothing after the last `summarize` drops or adds rows (no `where`, `take`, `top`, `count`, `distinct` or second `summarize` after it) and the table wasn't cut at 10,000 rows. Then an empty bin counts as 0 for `count`, `countif`, `dcount`, `sum`, `rate` and `peak_rate`, and as a gap for the other aggregates, which have no value without Logs. Otherwise every missing value is a gap.

**The table under a chart over time.** It lists one row per group, the `by` columns other than the time bin, with each aggregate worked out over the whole range: an `avg` or a `dcount` there is exact, not made from the bins. The rows go largest first by the first aggregate, each with its series' colour when it has one, so the table reads as the legend. The Table form of the switch shows the Query's own rows, one per bin and group.

**The chart switch.** The switch over the chart picks Line, Area, Bars or Table for a chart over time, Bars or Table for bars, and Number or Table for a number. Your pick goes in the URL as `viz`, so a link or a reload keeps it. While a table over time shows, the histogram strip is hidden.

**Zoom.** Drag across two or more bins of a chart over time to narrow the time range to them. A click does nothing.

**Add to dashboard.** Beside the switch, Add to dashboard saves the Project, the Query that ran and the chart's form to a new or existing Dashboard. It is there for a Query's table, not for Logs or for SQL. Create monitor, beside it, starts a Monitor from the same Query and range: see [Monitors](monitors.md).

## Reading less

Every Project's Logs are sorted by `service`, then by the `file` Attribute, then by time.

- **Filter on `service` and `file`** with `==` or `in`, such as `where file == "sidekiq.log"`, to read only those Logs, whatever the range. `service` skips most. `file` skips well too, alone, when the Project has few Services.
- **Filter on an Attribute rather than `message`.** Each Attribute is kept apart from the others, so a condition on one reads it alone. `message` is the widest Field: `where job_status == "fail"` reads far less than `where message contains "INFO: fail:"`. Pair a `message` match with a `service` or `file` condition.
- **Use `where`, not `search`**, which reads `message` and every string Attribute.
- **Keep the range short.** A read skips the days outside it, and a Dashboard refresh reads again only the bins of hours that haven't ended or have new Logs.

In SQL, the same holds: a condition on `service` and `file` reads less, and `attributes.name.:String` reads that Attribute alone.

## Not in the language

`has`, `join`, `mv-expand`, `parse`, `evaluate`, `project-away` and series and geo functions aren't supported.
