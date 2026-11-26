# Dashboard filters in SQL are ClickHouse query parameters

Extends [ADR 0014](0014-dashboard-filters-are-query-parameters.md) to SQL Charts. A **Chart** on a **Dashboard** can save a **SQL query** in place of a **Query**. Its SQL names a **Dashboard filter** as a ClickHouse query parameter, such as `{region:Array(String)}`. Console sends the filter bar's values with the run, as for a Query. EventStore finds the parameters in the SQL and gives ClickHouse each filter's values as a query parameter (clickhouse-go's `WithParameters`, over the native protocol). ClickHouse binds them on the server. EventStore never puts a value into the SQL text. The Project's read-only SQL user (`readonly = 1 CONST`, ADR 0003) takes query parameters.

The type in the SQL says how EventStore writes the values (`event_store/sqlfilter`):

- `Array(String)`: every value, as an array. All is `[]`. For a select: `(empty({region:Array(String)}) OR region IN {region:Array(String)})`.
- `String`: the values joined by one space. All is `''`. For a search box: `({q:String} = '' OR positionCaseInsensitive(message, {q:String}) > 0)`.
- `Nullable(String)`: as `String`, but All is `NULL`.

Another type, or one name with two types, answers 400 with a message that lists the three types. The scan skips string literals, quoted names, heredocs and comments. A name the request does not fill is All, as for a Query, so the Logs explorer's SQL mode, `run_sql` and a SQL **Monitor** run the same SQL. A Monitor's check refuses SQL that names a filter, as it refuses a Query that names one.

A future reader may ask why SQL Charts write `empty(...) OR ...` themselves, and why `{x:UInt8}` is refused only when it is in SQL that EventStore runs. This decision is the cause.

## Considered options

- **Splice the values into the SQL text,** as Grafana's `$region` does. No new type rules, but the values become SQL that EventStore must quote correctly for each place they can go, and a mistake is an injection into SQL that the user's database user runs. ClickHouse's own parameters bind a value as a value, wherever it goes.
- **One fixed type for each filter kind,** such as `Array(String)` for every select and `String` for every search box. EventStore would need no type in the SQL, but a select with one value could not be compared with `=`, and a search box could not be NULL for a test of All. The type in the SQL lets the author pick the form that fits the comparison.
- **Grafana's All as every option.** All would send each value of the select, so SQL needs no `empty(...)`. But a select of a field's values has only its 100 most common values, so All would leave out the rest, and a search box has no options at all. An empty value, written in by the SQL, matches every row.

## Consequences

- SQL can't drop a comparison, so the author writes the All case. The chart editor's SQL tab shows the two forms above.
- SQL Charts have no bin cache: EventStore can't prove that each row of a SQL query comes from the Logs of its own bin. A cache waits until a slow SQL Chart shows the need.
- Only EventStore reads the SQL. A run's answer names the filters the SQL uses, so the browser runs a SQL Chart again only when one of those changes. There is no second scan in TypeScript or Ruby.
