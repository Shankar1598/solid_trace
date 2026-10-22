# SQL queries and the `logs` view

On an instance whose operator has turned SQL queries on, the Logs explorer has a SQL mode. In it you write a raw [DuckDB SQL](https://duckdb.org/docs/sql/introduction) SELECT, and it runs as you wrote it. SQL mode opens with the SQL of your current Query, so you can start from something that works.

- SQL runs only when you press Run (or Ctrl+Enter). Changing the time range waits for Run too.
- A query that runs for more than 10 seconds is stopped, with "the run was stopped after 10 s". DuckDB gives a query every thread it has, so the limit keeps a slow one from holding up ingest. EventStore stops SQL at 10 seconds whatever it is asked, from the explorer or the MCP server.
- The explorer's URL holds your SQL, so you can share it. Opening a link with SQL never runs it: whoever opens the link presses Run.
- SQL can't be turned back into a Query. Leaving SQL mode drops your SQL and returns to the Query it started from. If you changed the SQL, the explorer asks first.
- The histogram and the Builder's chips are hidden in SQL mode.

SQL queries are read-only. A query must be one SELECT, which may start with `WITH` or `FROM`; `DESCRIBE`, `SHOW` and `SUMMARIZE` work too. Anything else is refused before it runs, with "SQL queries are read-only: … isn't allowed":

- statements that change something, such as `DROP`, `INSERT`, `UPDATE`, `DELETE`, `CREATE`, `COPY`, `ATTACH`, `SET`, `PRAGMA`, `CALL`, and `EXPLAIN` too;
- more than one statement, and `PIVOT` without an `IN` list, which DuckDB runs as more than one;
- the functions that change something from a SELECT: `query`, `query_table`, `json_execute_serialized_sql`, `enable_logging`, `disable_logging`, `truncate_duckdb_logs`, `write_log`, `enable_profiling`, `disable_profiling`, `enable_peg_parser`, `disable_peg_parser`, `checkpoint`, `force_checkpoint`, `nextval` and `setseed`.

DuckDB's extensions load only as EventStore needs them, so a function from any other extension isn't found.

SQL queries read only the Project you run them on: the `logs` view and the `events` view, named as they are, without a schema or database. Anything else they read is refused before it runs, with "SQL queries can read only the logs and events views: … isn't allowed":

- any other table or view, such as the tables that store Logs, and a qualified name such as `main.logs`;
- a file path or URL in `FROM`, such as `FROM 'x.parquet'` or `FROM 's3://…'`;
- every table function but `range`, `generate_series` and `unnest`, which read nothing: the file readers such as `read_parquet`, `read_csv` and `glob`, and the catalogue's such as `duckdb_tables`;
- `SHOW TABLES`, `SHOW ALL TABLES`, `information_schema` and `pg_catalog`, and `DESCRIBE`, `SHOW` or `SUMMARIZE` of anything but the two views or a query of them;
- the functions that read settings or the catalogue: `current_setting`, `getvariable`, `pg_get_viewdef` and `pg_get_constraintdef`;
- a `WITH` part named as a table or view in the database, `events` included, or with a name that isn't a plain identifier, such as a file path.

`WITH` parts, subqueries, joins, `VALUES`, `PIVOT` and `UNPIVOT` over the two views work.

SQL queries are on by default, and the operator can turn them off (`bin/rails instance_settings:set SQL_QUERIES_ENABLED=false`). They can't change data, settings or files, or read beyond their Project, but a slow one can slow down ingest for up to 10 seconds. See [ADR 0003](../adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md).

## The `logs` view

Read Logs from the `logs` view, not from the tables that store them. The view keeps the same columns when storage changes, so your SQL keeps working.

The view holds the Logs of the Project and time range in the explorer's bar, and no others. It is all of the Project's Logs your SQL can read.

```sql
SELECT service, count(*) AS logs
FROM logs
WHERE level = 'error'
GROUP BY service
ORDER BY logs DESC
```

| Column | Type | Holds |
| --- | --- | --- |
| `project_id` | `UINTEGER` | The Project's id. It is the same in every row. |
| `id` | `UUID` | The Log's id. |
| `time` | `TIMESTAMP` | The Log's time, in UTC. |
| `level` | `VARCHAR` | `trace`, `debug`, `info`, `warn`, `error`, `fatal`, or NULL when the Log has no Level. |
| `message` | `VARCHAR` | The message. |
| `service` | `VARCHAR` | The Service, or NULL. |
| `environment` | `VARCHAR` | The environment, or NULL. |
| `trace_id` | `VARCHAR` | The trace id, or NULL. |
| `span_id` | `VARCHAR` | The span id, or NULL. |
| `attrs_string` | `MAP(VARCHAR, VARCHAR)` | The Log's string Attributes, by name. |
| `attrs_number` | `MAP(VARCHAR, DOUBLE)` | Its number Attributes. |
| `attrs_bool` | `MAP(VARCHAR, BOOLEAN)` | Its true/false Attributes. |
| `attrs_json` | `MAP(VARCHAR, VARCHAR)` | Its arrays and deeply nested objects, as JSON text. |

## Attributes

Every Attribute is in the map for the type of its value. A dotted name such as `http.method` is one key, because nested names are flattened when Logs arrive. Read a value with `map['key']`. It is NULL when the Log doesn't have that Attribute with that type:

```sql
SELECT time, message, attrs_number['http.status'] AS status
FROM logs
WHERE attrs_string['http.method'] = 'POST'
ORDER BY time DESC
LIMIT 100
```

An Attribute keeps the type of each value, so one Log can hold `status` in `attrs_number` and another in `attrs_string`. To read either, use `coalesce(attrs_string['status'], CAST(attrs_number['status'] AS VARCHAR))`. A whole number too large to hold exactly is kept as a string.

`map_keys(attrs_string)` lists a Log's string Attribute names. Read JSON values with DuckDB's JSON functions, such as `json_extract(attrs_json['tags'], '$[0]')`.

## The `events` view

The `events` view holds the Project's Events of the time range in the explorer's bar, and no others. Join it with `logs` to see Logs beside Events.

| Column | Type | Holds |
| --- | --- | --- |
| `uuid` | `VARCHAR` | The Event's id. |
| `project_id` | `BIGINT` | The Project's id. It is the same in every row. |
| `issue_fingerprint_id` | `BIGINT` | The Issue the Event belongs to. |
| `timestamp` | `TIMESTAMP` | The Event's time, in UTC. |
| `environment`, `server_name`, `release`, `level` | `VARCHAR` | As the SDK sent them. |
| `tags` | `MAP(VARCHAR, VARCHAR)` | The Event's tags. |

## Matching text

`message` is most of what a Log stores, so how you match it decides how fast a query is. DuckDB's string functions are several times faster than `ILIKE`:

| Instead of | Write |
| --- | --- |
| `message ILIKE '%timeout%'` | `contains(lower(message), 'timeout')` |
| `message NOT ILIKE '%timeout%'` | `NOT contains(lower(message), 'timeout')` |
| `message LIKE '%timeout%' ESCAPE '\'` | `contains(message, 'timeout')` |
| `message ILIKE '%user_42%'` | `regexp_matches(message, '(?i)user.42')` |

A plain `LIKE '%timeout%'` and a pattern without a leading `%`, such as `ILIKE 'GET %'`, are already fast. When a query matches `message` slowly, it still runs, and its answer carries a `notice` saying what to write instead. `run_sql` passes it on to MCP clients; the explorer doesn't show it yet.

## Filtering on Attributes

`attrs_string['name']` reads a map built for the view, so a condition on an Attribute, such as `attrs_string['file'] = 'sidekiq.log'`, reads every Log of the range. A Query reads only the matching parts of each Log file, as its Log file order sorts them: see [Reading less](query-language.md#reading-less).

## Results

- Results show at most 10,000 rows. A result that has more is marked as cut off.
- Times are shown in UTC.
- A JSON value, map, list or struct is shown as JSON.
- If the SQL fails or is refused, you see why: DuckDB's error message, or what isn't allowed.
- A query has no time limit. Cancelling it in the explorer stops the wait, but the server may still finish the query.
- A query reads the Logs as they were when it started, in one transaction. A long query holds up nothing else, but Log files it may read are only deleted (by Retention or the disk limit) after it ends.

## From a Query to SQL

SQL mode opens with your Query written as SQL against the `logs` view. Each stage of the Query becomes one `WITH` step (`stage1`, `stage2` and so on). The SQL returns the same rows as the Query:

- A Query that lists Logs returns the view's rows, newest first.
- A Query that returns a table returns its columns. A column whose values have no single type, such as an Attribute grouped in `by` or chosen in `project`, comes back as JSON.

A few columns are named `"#order"`. They carry the order of a sort into the steps after it, because SQL doesn't keep row order from one step to the next.
