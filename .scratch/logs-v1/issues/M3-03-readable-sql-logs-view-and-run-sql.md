# M3-03: Readable SQL, the logs view and Run SQL

**What to build:** on an instance with SQL queries on, EventStore runs a member's raw DuckDB SQL against a documented `logs` view for the Project and time range, and prints any Query as readable SQL against that view. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore) (Readable SQL, Run SQL); [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md); user stories 67–69.

**Blocked by:** M1-02, M3-01.

**Status:** ready-for-agent

- [ ] The compiler prints a Query as readable SQL against the `logs` view, one CTE per stage.
- [ ] A query-app operation returns that readable SQL for a Query.
- [ ] Run SQL, only when the instance setting is on (refused otherwise): EventStore builds `logs` as a temporary view on that query's connection, covering the Project's hot table, its days in Parquet and its `logs_late` rows, with promoted columns merged back into the four maps, and runs the SQL as written. The view's columns: `project_id`, the built-in fields, the Attributes maps.
- [ ] The `logs` view is documented for users.
- [ ] Cross-check test: for a set of Queries (filters, search, summarize, top, distinct), the readable SQL run through Run SQL returns the same rows as the Query.
- [ ] Tests: Run SQL refused while the setting is off; the view covers exactly the requested Project and range across hot, Parquet and late rows.
