# Unrestricted SQL queries behind an off-by-default instance setting

SQL queries run raw DuckDB SQL exactly as written, with no validation, sandbox or Project scoping, but only on installs whose operator turns on one instance-level setting (off by default). Research showed that making untrusted SQL safe on DuckDB needs structural isolation (a separate instance or process seeing one Project's data), because instance-wide settings, statements that run during prepare, `main.<table>` and `query()` all escaped lighter checks; building that was judged not worth it for v1. Instead, the operator opts in knowing that any member can then read every organization's Logs and Events and server files, write files, drop tables and starve ingest. Don't add partial checks (AST allow-lists, scoping CTEs) as if they made it safe: they were shown not to.

## Consequences

- Users query a stable `logs` view, not the storage tables, so storage layout changes don't break their SQL.
- If SQL must later be offered on installs with untrusted members, the fix is the structural sandbox, and existing cross-Project SQL will break.

Source: [What query language(s) do users write?](../../.scratch/logs/issues/11-query-language.md).
