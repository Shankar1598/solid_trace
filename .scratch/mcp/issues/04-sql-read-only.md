# 04: SQL queries are read-only

**Status:** resolved
**Track:** Go
**Blocked by:** none

## What to build

Make every SQL query read-only, in the explorer's SQL mode and in the MCP's `run_sql`. Both go through `LogStore.RunSQL` (`event_store/storage/logs_sql.go`). The spec's Tickets section, G2, has the design; the escapes come from `.scratch/logs/issues/04-apl-kql-surface-and-sql-sandboxing.md` (seen on DuckDB 1.4.3).

1. **Probe first** on the bundled DuckDB 1.5.6, under a minute:
   - Does `json_serialize_sql` still reject every non-SELECT, PRAGMA included?
   - How does it report a multi-statement string?
   - What shape do function and table-function nodes have in its AST?
   Record the answers in the spec's Implementation decisions.
2. **Before running the SQL, refuse it unless:**
   - `SELECT json_serialize_sql(?)` parses it, with the SQL bound as a parameter and nothing else run;
   - it is exactly one statement;
   - that statement is a SELECT;
   - its AST calls none of `query`, `query_table`, `enable_logging`, `disable_logging`, `checkpoint`, `force_checkpoint`, `nextval` or `setseed`, as scalar or table functions.
3. **Turn off** `autoload_known_extensions` and `autoinstall_known_extensions` after EventStore has loaded the extensions it needs. Check the object-store code (ADR 0008) for what it loads, and when.
4. **A refusal is a 400** with `SQL queries are read-only: <what> isn't allowed`. The explorer shows it as it shows other SQL errors.
5. **Amend ADR 0003:** SQL can't change data any more, but it can still read every Organization's Logs and Events, server files and the bucket, so the setting stays off by default. Update the setting's description in Console and `docs/logs/sql.md` to match.

## Acceptance

- Go tests, one case for each escape, each expected to be refused:
  - DROP, INSERT, UPDATE, DELETE, CREATE, COPY TO, ATTACH, SET and PRAGMA;
  - CALL `checkpoint()`;
  - a multi-statement string;
  - `query('DROP TABLE …')`, `query_table(…)`, `enable_logging()` and `nextval(…)`.
- Ordinary SELECTs still run: the `logs` view, CTEs, FROM-first syntax and window functions.
- Extend `console/e2e/flows/logs-v1-sql.spec.ts` so that `DROP TABLE logs.log_key_counts` is refused with the message, and a SELECT runs.
- `go test -p 1 ./...` passes.

## Comments

### 2026-10-17 resolved

Built on `feature/mcp` in workflow `wf_7acd8b1c-eef` (implement, one review and one verify, a skeptic on high and medium findings, fixes, recheck). Head `400a98f` on `feature/mcp-go`, merged into `feature/mcp` at `19939fb`. The probe's answers are in [research/probe-04-readonly-sql.md](../research/probe-04-readonly-sql.md). One finding confirmed and fixed in the copy: on bucket installs, SQL can still send requests from the server to any web address it reaches (httpfs), which the setting's description, ADR 0003 and `docs/logs/sql.md` now say. The Playwright change to `e2e/flows/logs-v1-sql.spec.ts` is written, not run: ticket 10 runs it. Decisions are under 04 in the spec's Implementation decisions; findings left for later are in [carry-over.md](../carry-over.md).
