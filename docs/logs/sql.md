# SQL queries and the `logs` table

On an instance whose operator has turned SQL queries on, the Logs explorer has a SQL mode. In it you write a [ClickHouse SQL](https://clickhouse.com/docs/sql-reference) SELECT, and it runs as you wrote it. SQL mode opens with the SQL of your current Query, so you can start from something that works.

- SQL runs only when you press Run (or Ctrl+Enter). Changing the time range waits for Run too.
- The explorer's URL holds your SQL, so you can share it. Opening a link with SQL never runs it: whoever opens the link presses Run.
- SQL can't be turned back into a Query. Leaving SQL mode drops your SQL and returns to the Query it started from. If you changed the SQL, the explorer asks first.
- The histogram and the Builder's chips are hidden in SQL mode.

## What SQL can read

Each Project's SQL runs as that Project's own read-only database user. The database, not a check of your SQL's text, decides what it can read:

- the `logs` table, which shows the Project's Logs of the time range in the explorer's bar, and no others;
- the `events` table, which shows the Project's Events of that range, and no others.

The database refuses anything else, whatever the SQL says: another Project's rows, the other tables that EventStore keeps, the server's system tables and `information_schema`, files, URLs, the bucket, other servers, and the table functions that read them (`file`, `url`, `s3`, `remote` and the rest). It refuses any statement that changes something, such as `INSERT`, `ALTER`, `DROP` or `CREATE`, and any change to a setting but the range. The refusal is the database's own message, such as "Not enough privileges" or "Cannot modify … in readonly mode".

Logs sent before the switch to ClickHouse, which are still read from their old Log files, aren't in `logs`. Queries read them until Retention deletes them.

### The range

The range is a convenience, not a limit. `logs` and `events` read it from two settings, `SQL_from` and `SQL_to`, which the explorer sets from its bar. Your SQL may set them itself, in UTC, to read more of the Project:

```sql
SELECT toStartOfDay(time) AS day, count() AS logs
FROM logs
GROUP BY day
ORDER BY day
SETTINGS SQL_from = '2026-10-01 00:00:00', SQL_to = '2026-10-08 00:00:00'
```

It still reads only the Project's rows.

### Limits

A query stops after 10 seconds, or when it needs more than 1 GiB of memory, and uses at most half of the server's CPUs. The limits are the database user's, so a query can't raise them with `SETTINGS`. They keep a slow query from holding up ingest or other members' runs for long. The explorer and the MCP server may ask for a shorter time; EventStore stops SQL at 10 seconds whatever it is asked.

SQL queries are on by default, and the operator can turn them off (`bin/rails instance_settings:set SQL_QUERIES_ENABLED=false`). With them on, any member can read all of their own Organization's Projects' Logs and Events, and a slow query can slow down ingest for up to 10 seconds. See [ADR 0003](../adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md).

## The `logs` table

```sql
SELECT service, count() AS logs
FROM logs
WHERE level = 'error'
GROUP BY service
ORDER BY logs DESC
```

| Column | Type | Holds |
| --- | --- | --- |
| `project_id` | `UInt32` | The Project's id. It is the same in every row. |
| `id` | `UUID` | The Log's id. |
| `time` | `DateTime64(6, 'UTC')` | The Log's time. |
| `level` | `LowCardinality(Nullable(String))` | `trace`, `debug`, `info`, `warn`, `error`, `fatal`, or NULL when the Log has no Level. |
| `service` | `LowCardinality(String)` | The Service, or `''`. |
| `environment` | `LowCardinality(Nullable(String))` | The environment, or NULL. |
| `message` | `String` | The message. |
| `trace_id`, `span_id` | `Nullable(String)` | The trace and span ids, or NULL. |
| `batch` | `UUID` | The Log batch the Log arrived in. |
| `file` | `LowCardinality(String)` | The `file` Attribute when it is a string, else `''`. The Attribute is in `attributes` too. |
| `attributes` | `JSON` | Every string, number and true/false Attribute, at its name. |
| `attrs_json` | `Map(LowCardinality(String), String)` | Array and object Attributes, as JSON text. |
| `search_text` | `String` | Every string Attribute's value, joined by `\0`. `SELECT *` leaves it out; name it to read it. |

Rows are stored sorted by Project, Service, `file` and time, so a condition on `service` or `file` reads only those Logs.

## Attributes

Read an Attribute from `attributes` by its name, with the type you want: `.:String`, `.:Float64` (every number) or `.:Bool`. A dotted name such as `http.method` is a path, as nested names are flattened when Logs arrive. A value is NULL when the Log doesn't have that Attribute with that type:

```sql
SELECT time, message, attributes.http.status.:Float64 AS status
FROM logs
WHERE attributes.http.method.:String = 'POST'
ORDER BY time DESC
LIMIT 100
```

Quote a name that isn't a plain identifier with backticks: ``attributes.`content-type`.:String``. An Attribute keeps the type of each value, so one Log can hold `status` as a number and another as a string. To read either, use `coalesce(attributes.status.:String, toString(attributes.status.:Float64))`. Compare and aggregate the typed reads, not `attributes.status` itself: a path that holds more than one type can't be compared with a value. A whole number too large to hold exactly is kept as a string.

Read an array or object Attribute's JSON text from `attrs_json`, with ClickHouse's JSON functions, such as `JSONExtractString(attrs_json['tags'], 1)`. `mapKeys(attrs_json)` lists those names, and `JSONAllPaths(attributes)` the rest.

## The `events` table

The `events` table holds the Project's Events of the range. Join it with `logs` to see Logs beside Events.

| Column | Type | Holds |
| --- | --- | --- |
| `uuid` | `UUID` | The Event's id. |
| `project_id` | `UInt32` | The Project's id. It is the same in every row. |
| `issue_fingerprint_id` | `Int64` | The Issue the Event belongs to. |
| `timestamp` | `DateTime64(6, 'UTC')` | The Event's time. |
| `environment`, `server_name`, `release`, `level` | `LowCardinality(String)` | As the SDK sent them, or `''`. |
| `tags` | `Map(String, String)` | The Event's tags. |

## Matching text

| To match | Write |
| --- | --- |
| text anywhere, any case | `positionCaseInsensitive(message, 'timeout') > 0`, or `message ILIKE '%timeout%'` |
| text anywhere, this case | `position(message, 'timeout') > 0`, or `message LIKE '%timeout%'` |
| the start | `startsWith(message, 'GET ')` |
| a regular expression | `match(message, '(?i)user.42')` |
| text in any string Attribute | `positionCaseInsensitive(search_text, 'timeout') > 0` |

## Results

- Results show at most 10,000 rows. A result that has more is marked as cut off.
- Times are shown in UTC.
- A JSON value, map, array or tuple is shown as JSON.
- If the SQL fails or is refused, you see why: ClickHouse's message. When the SQL doesn't parse, the explorer shows the line where it failed with a caret under the spot.
- Cancelling a query in the explorer stops the wait, but the server may still run the query to its 10 seconds.

## From a Query to SQL

SQL mode opens with your Query written as SQL against the `logs` table. Each stage of the Query becomes one `WITH` step (`stage1`, `stage2` and so on). The SQL returns the same rows as the Query:

- A Query that lists Logs returns the table's rows, newest first.
- A Query that returns a table returns its columns. A column whose values have no single type, such as an Attribute grouped in `by` or chosen in `project`, comes back as a `Dynamic` holding each value as its type. An array or object comes back as its JSON text.

A few columns are named `` `#order` ``. They carry the order of a sort into the steps after it, because SQL doesn't keep row order from one step to the next.
