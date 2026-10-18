# Ticket 04 probe: json_serialize_sql on DuckDB 1.5.6 (duckdb-go v2.10506.0), 2026-10-17

- Bind the SQL as `CAST(? AS VARCHAR)`: an untyped `?` fails with "json_serialize_sql first argument must be a VARCHAR".
  The result is JSON, which duckdb-go decodes to a map; cast it to VARCHAR to scan a string.
- Every non-SELECT is rejected: {"error":true,"error_type":"not implemented","error_message":"Only SELECT statements can be serialized to json!"}.
  Seen for DROP, INSERT, UPDATE, DELETE, CREATE, COPY t TO, COPY (SELECT) TO, ATTACH, DETACH, SET, RESET, PRAGMA (table_info and enable_profiling),
  CALL, CHECKPOINT, EXPLAIN, LOAD, INSTALL, USE, EXPORT DATABASE, VACUUM, BEGIN, ALTER, WITH ... DELETE, WITH ... INSERT, PIVOT without IN.
- Parse as SELECT: DESCRIBE, SHOW, SUMMARIZE (SHOW_REF), FROM-first, VALUES, TABLE t, PIVOT ... IN (...), UNION.
- Multi-statement: "SELECT 1; SELECT 2" -> statements has 2 entries. Any non-SELECT anywhere -> the error above for the whole string.
  "SELECT 1;", "SELECT 1; -- x", "SELECT 1; ;" -> 1 statement. "" -> 0 statements.
- Syntax error: {"error":true,"error_type":"parser","error_message":"syntax error at or near \"SELEC\"","error_subtype":"SYNTAX_ERROR","position":"0"} (position is a string byte offset).
- Function nodes: {"class":"FUNCTION","type":"FUNCTION","function_name":"setseed","schema":"main","catalog":"system","children":[...]}.
  Window functions: class WINDOW with function_name. Table functions: {"type":"TABLE_FUNCTION","function":{FUNCTION node}}.
  Names come lowercased (QUERY_TABLE, main."Query"). A named WINDOW is inlined into the window expression; list comprehensions become list_apply/list_filter lambdas.
- query('DROP TABLE t') is refused by DuckDB 1.5.6 itself ("Expected a single SELECT statement").
- Other functions that change state from a SELECT in 1.5.6: enable_profiling, disable_profiling, enable_peg_parser, disable_peg_parser,
  truncate_duckdb_logs (table) and write_log (scalar). json_execute_serialized_sql runs a serialized SELECT whose calls the AST walk can't see.
- Built-in macros calling those names: none that change state (histogram_values reads through query_table).
- Extensions: autocomplete, core_functions, icu, json, parquet are statically linked; sqlite_scanner and ducklake were installed in ~/.duckdb,
  so with autoload on a SELECT naming their functions would load them.
