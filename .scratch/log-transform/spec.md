# Log transform

**Status:** ready-for-agent

Planned with the user on 2026-10-21. It started when Dashboard 1 on the `tagalys` Project pegged the production VM's CPU and took 5 s to load: four of its Charts found `sidekiq.log` job lines by searching `message`. The design was settled in one grilling session; the terms are in `GLOSSARY.md` (**Log transform**).

## Problem Statement

The only way to pick out lines of a free-text log is to search `message`, and that is the widest field a Log has: 292 bytes on average on `tagalys`. Since the r3 and r4 indexers joined, that Project takes about 3,300 Logs/s. A Chart that searches `message` over 90 minutes reads about 3.5 GB. That doesn't fit beside DuckDB in the VM's memory, so every refresh re-reads it from a disk that tops out at about 140 MB/s, and a Dashboard of such Charts saturates the VM. As a stopgap, the four job-line Charts were moved to a Dashboard of their own with auto-refresh off.

The real fix is to turn those lines into Attributes before they're stored (status, worker class, duration), so a Chart reads a narrow column instead of the text. Today that can only be done in the sender's own shipper, which has two problems:

- **Not every sender has a shipper that can do it.** Vector can, with a `remap` on every host, but Sentry SDKs, OTLP exporters and plain HTTP senders can't.
- **Nobody working in SolidTrace can do it.** An admin, or an AI agent working through the MCP server, can see that a Chart is slow because it searches `message`, but has no way to fix it without access to the sender's hosts and config.

## Solution

Each Project gets one **Log transform**: a VRL program, in the same language as Vector's `remap`, that Log processing runs on each of the Project's Logs between the stored Log batch and DuckDB.

**What a program can do:**
- Set, change and remove Fields: for example, parse `message` into Attributes, then shorten it or delete it.
- Drop a Log with `abort`.

**What happens to the result:**
- Whatever the program returns is normalised exactly as Log ingest normalises a sender's Log.
- A Log the program fails on is stored as it arrived, with the error in `solidtrace.transform_error`.
- A change applies to the Logs processed after it. Stored Logs keep what they had.
- Events never go through it.

**Where it's edited:**
- In Console, on a **Log transform** page in the Project, linked from the Project's settings. It has an editor that underlines errors as you type, a preview that runs the draft on the Project's recent arrivals and shows each Log before and after, the program's running counts, and the saved versions with Restore. Only admins can save or restore; members see the page read-only and can preview.
- Over MCP, with `get_log_transform`, `preview_log_transform` and `update_log_transform`.

**Safety:** every saved version is kept, so a bad save can be undone for the Logs that come after it.

## User Stories

1. As an admin, I want to write a program that parses a Project's `message` into Attributes before the Logs are stored, so that Charts and Monitors filter on narrow Attributes instead of searching text.
2. As an admin, I want the program to delete or shorten `message` once it has parsed it, so that the Project's Logs take less disk and scans read less.
3. As an admin, I want the program to drop Logs I never need, such as health checks, so that they're never stored or scanned.
4. As an admin, I want to write the program in VRL, so that I can reuse what I know from Vector and paste a `remap` across.
5. As an admin, I want built-in fields at the top level (`.message`, `.level`, `.service`) and Attributes nested by their dots (`.http.method`), as Vector shows them, so that programs read naturally.
6. As an admin, I want to change built-in fields too, such as setting `.level` from a parsed word or `.service` from a file name, so that Logs from a shipper that can't set them come out right.
7. As an admin, I want what my program returns to follow the same rules as Logs from a sender (reserved names, Levels, size limits), so that a program can't store a Log that ingest would have refused.
8. As an admin, I want a program that fails on a Log to leave that Log as it arrived, with the error in `solidtrace.transform_error`, so that a parse that doesn't match never loses a Log.
9. As an admin, I want to find every Log my program failed on with `isnotnull(solidtrace.transform_error)`, so that I can see which lines it doesn't handle.
10. As an admin, I want a Monitor on `isnotnull(solidtrace.transform_error)` to work like any other, so that I'm told when a program starts failing.
11. As an admin, I want the editor to underline compile errors as I type, at the right characters and with VRL's message, so that I can fix a program before I save it.
12. As an admin, I want to preview a draft on the Project's recent arrivals before saving it, seeing each Log before and after, so that I know what the program will do.
13. As an admin, I want the preview to say how many Logs the draft kept, dropped and failed on, so that a draft that drops everything is obvious before it's saved.
14. As an admin, I want the preview to include recent Logs from each Service, so that a draft aimed at a quiet Service still has Logs to run on.
15. As an admin, I want to paste my own sample Logs into the preview, so that I can test a line that hasn't arrived recently.
16. As an admin, I want a save to be refused when the program doesn't compile, so that a typo never reaches Log processing.
17. As an admin, I want a save to take effect within seconds, so that I can watch the result in the Logs explorer straight away.
18. As an admin, I want to be told, when I save, that Logs already stored don't change, so that I'm not surprised when a Chart on a new Attribute starts at the moment of the save.
19. As an admin, I want every saved version kept, with who saved it and when, so that I can see how the program got to where it is.
20. As an admin, I want to restore an earlier version, which saves it again as the newest, so that a bad save can be undone for the Logs that follow.
21. As an admin, I want to save an empty program to turn the transform off, so that the Project's Logs pass through unchanged.
22. As an admin, I want the page to show how many Logs the current version has processed, dropped and failed on since EventStore started, and its average time per Log, so that I can tell whether it's doing what I meant and whether it's slow.
23. As an admin, I want the failed count to link to the Logs explorer with `isnotnull(solidtrace.transform_error)`, so that I can go straight to the failures.
24. As an admin, I want the page to say when the saved program no longer compiles, for example after an upgrade changed VRL, and that Logs are being stored as they arrived meanwhile, so that I know to fix it.
25. As a member, I want to read the Project's Log transform, so that I understand why a Field looks the way it does.
26. As a member, I want to preview a draft, so that I can propose a change to an admin with evidence.
27. As a member, I want Save and Restore to be unavailable to me, since a program can drop and delete data for good.
28. As an AI agent, I want `get_log_transform` to return the current program, its version, who saved it and when, the recent versions and the running counts, so that I can understand a Project's Logs before I query them.
29. As an AI agent, I want `preview_log_transform` to run a draft on recent arrivals or on Logs I pass in, and return each before and after with the kept, dropped and failed counts, so that I can iterate on a program without changing anything.
30. As an AI agent acting for an admin, I want `update_log_transform` to save a program and return VRL's errors when it doesn't compile, so that I can fix a slow `message` search end to end.
31. As an AI agent acting for a member, I want `update_log_transform` to be refused with a message saying only admins can change it, so that I can tell the user to ask an admin.
32. As an AI agent, I want the tools' descriptions to explain what `.` looks like, `abort`, `solidtrace.transform_error`, which functions are missing and that stored Logs don't change, so that I write working programs first time.
33. As an operator, I want a program unable to reach the network, read EventStore's environment or read the host, so that an admin's program can't reach EventStore's loopback API or its secrets.
34. As an operator, I want a panic inside VRL to fail only that Log, never EventStore, so that a VRL bug can't stop ingest.
35. As an operator, I want a program's cost per Log measured before this ships, so that I know the VM keeps up at the current volume.
36. As an operator, I want ingest to behave exactly as it does now, with a `200` still meaning stored, so that a slow or broken program never holds up a sender.
37. As a sender, I want my Logs accepted whatever the Project's program does, so that a program change never makes my requests fail.
38. As a `tagalys` user, I want `sidekiq.log` job lines turned into Attributes, so that the job-line Charts can move back to Dashboard 1 and refresh every minute without saturating the disk.
39. As an admin of a new Project, I want it to start with no program, so that Logs are stored as they arrive until someone writes one.
40. As an admin, I want deleting a Project to delete its Log transform versions, so that nothing is left behind.

## Implementation Decisions

### The VRL runtime

**What it is:**
- A new EventStore module wraps the Rust `vrl` crate (MPL-2.0), behind cgo, as a Rust staticlib.
- FlowG (`link-society/flowg`) is prior art for the pattern.
- Logs cross the boundary as msgpack, which is already the Log batch encoding.

**The Go interface is narrow:**
- `Compile(program)` returns a compiled program, or diagnostics. Each diagnostic has its severity, code, message, labels with byte start and end, and VRL's formatted text.
- A compiled program runs on a slice of Logs in one call, to keep FFI calls down. It returns one outcome per Log: the Log it returned, dropped (by `abort`), or failed with the error text.

**What the build leaves out:**
- The crate is built without default features. It has the stdlib but no network, environment or system functions: `http_request`, `dns_lookup`, `reverse_dns`, `get_env_var`, `get_hostname` and their kin.
- A program calling one gets the "undefined function" compile error.

**Panics:**
- A panic is caught at the FFI boundary (`catch_unwind`, with panics set to unwind). It becomes a failed outcome for that Log.
- It never aborts the process.

**Caching:** a compiled program is cached by version id and freed when a newer version replaces it.

**Build and tooling:**
- EventStore's Dockerfile gains a Rust stage (Rust 1.95 or later), whose staticlib the Go stage links.
- The release workflow already builds each architecture on a native runner.
- Local development gains Rust through `mise`, plus one documented step that builds the staticlib before `go build` and `go test`.

### What the program sees

**Built-in fields** are at the top level:
- `.time` is a VRL timestamp.
- `.level` is absent when the Log has none.
- `.message`, `.service`, `.environment`, `.trace_id` and `.span_id` are strings.

**Attributes** are nested by their dots: `http.method` is `.http.method`.
- Numbers, strings and bools keep their types. A JSON Attribute (an array) is a VRL array.
- When a key can't nest because a shorter key already holds a value (a Log with both `a` and `a.b`), the longer key stays at the top level as a quoted dotted key, `."a.b"`.
- `solidtrace.*` Attributes are visible.

**The Log's id** is never shown and never changes.

**When the program returns:**
1. `.` is flattened back into dotted Attributes, as Log ingest flattens a sender's Log (ADR 0002).
2. It is then normalised by the same code Log ingest uses:
   - built-in field rules and `original.<field>`;
   - Levels;
   - the `solidtrace.` prefix renamed to `original.solidtrace.`;
   - the limits and `solidtrace.truncated`.

Normalisation gains an entry point for a decoded object, which JSON ingest and the Log transform share. If the program leaves `.` as something other than an object, that's a failure.

### Where it runs, and what a failure does

**Where:**
- In Log processing: after a Project's Log batches are read from Pebble, and before the hot/late split.
- So promotion's key counts, the hot table, `logs_late` and Match monitors all see the transformed Logs.
- A dropped Log is removed from its batch. The Processing cursor still moves past a batch that ends up empty.

**Which version:**
- At the start of each pass over a Project, Log processing reads the Project's current version from Console's SQLite, read-only and uncached, as Retention is read.
- It compiles the version once, keyed by version id.
- A save therefore takes effect on the next processing pass, within seconds.

**What a failure does:**
- **On one Log:** the Log is stored as it arrived (after ingest's normalisation, before the program), and `solidtrace.transform_error` holds the error text, cut to 1 KB. EventStore sets this Attribute after normalisation, so the reserved-name rule doesn't rename it.
- **When the saved version doesn't compile** (for example, an upgrade brought a VRL that refuses it):
  - Every Log of the Project is stored as it arrived, with `solidtrace.transform_error` naming the compile error.
  - The running counts report the compile error.
  - Nothing is lost.

**Limits:** a program is at most 64 KB. There is no per-Log timeout: VRL has no unbounded loops, Rust's regex engine is linear-time, and ingest's limits already cap a Log's size.

**Unchanged:** ingest and its `200`, Event processing, and stored Logs.

### Recent arrivals and running counts

These live in EventStore's memory and are lost on restart.

**Recent arrivals:**
- For each Project, the newest Logs as they arrived, before any program: up to 100 for each Service, and at most 4 MB per Project. The oldest go first.
- Log processing fills them as it reads batches.

**Running counts:**
- For each Project's current version: Logs processed, dropped and failed, and total program time.
- They reset when the version changes.

### EventStore's loopback routes

These sit beside `/logs/validate`, unauthenticated like the rest of the query API.

- **`POST /api/:project_id/logs/transform/check`**
  - Takes `{ program }`.
  - Returns `{ errors: [...], warnings: [...] }`. Each item has `message`, `code`, `start`, `end` (byte offsets) and `text` (VRL's formatted diagnostic).
  - A program over 64 KB is an error.
- **`POST /api/:project_id/logs/transform/preview`**
  - Takes `{ program, logs? }`.
  - Runs the program on the recent arrivals, or on up to 200 `logs` given as JSON objects, which are first normalised as Log ingest normalises them.
  - Returns:
    - `{ results: [ { before, after } | { before, dropped: true } | { before, error } ], kept, dropped, failed }`;
    - or the check's `errors` when it doesn't compile.
  - Nothing is stored.
- **`GET /api/:project_id/logs/transform/stats`**
  - Returns `{ version_id, processed, dropped, failed, average_micros, compile_error? }`.

### Console

**The versions table:**
- `log_transform_versions` holds `project_id`, `program` (text, at most 64 KB), `user_id` (who saved it; null once that user is gone) and `created_at`.
- The newest row is the Project's current Log transform. No row, or an empty program, means none.
- Restoring inserts a copy of the old row.
- Deleting a Project deletes its rows.
- EventStore reads this table, so its Go test seeding and the migration change together, as with the other Console tables EventStore touches (ADR 0001).

**Saving:**
- One service saves a version for both the page and the MCP tool, as `MemberInvite` serves both of its callers.
- It runs EventStore's check first and refuses on errors, returning them.
- Saving the same text as the current version is a no-op.

**Who may do what:**
- Saving and restoring use `ProjectPolicy#update?` (admins).
- Reading and previewing need only membership of the Organization.

**The page:**
- A new Inertia page under the Project, linked from the Project's settings page.
- Editor:
  - A CodeMirror editor, with `@codemirror/lint` underlining errors and warnings from the check route.
  - It is debounced, as the Query editor validates.
  - Syntax highlighting is up to the build.
- Preview: the before and after of each Log side by side, with kept, dropped and failed counts.
- Save asks for confirmation, saying stored Logs don't change.
- The running counts, with the failed count linking to the Logs explorer with `isnotnull(solidtrace.transform_error)`.
- A compile error from the stats, shown prominently.
- The last 20 versions, with who saved each and when, view, and Restore.
- Members get the editor read-only, with Preview.

### MCP tools

These follow the existing tool base: role checks through `authorize!`, and an answer ending with the Console page's link.

- **`get_log_transform(organization?, project, version?)`**
  - Read-only.
  - Returns the current program (or the given older version's), its version id, who saved it and when, the last 10 versions (id, who, when, size), and the running counts.
- **`preview_log_transform(organization?, project, program, logs?)`**
  - Read-only, for any member.
  - Returns the check's errors, or each Log's before and after (the first 20 in full) with the kept, dropped and failed counts.
- **`update_log_transform(organization?, project, program)`**
  - Marked destructive; admins only (`authorize!(project, :update?, …)`).
  - Saves a new version, or is refused with VRL's formatted errors.
  - Restoring is a call with an old version's text.

**Descriptions:** the tools' descriptions explain:
- the shape of `.`;
- `abort`;
- `solidtrace.transform_error`;
- the missing functions;
- that a save applies only to Logs processed after it;
- a link to VRL's function reference.

`docs/mcp.md` gains them.

### Order of work

1. **A probe, first.** It runs in under a minute on the dev box, through the cgo bridge, batched:
   - The program: a realistic one that `parse_regex`es a `sidekiq.log` job line, sets four Attributes and deletes `message`.
   - Pass: about 3,300 Logs/s in under a quarter of one core, which is about 75 µs per Log including msgpack.
   - If it fails, the runtime question goes back to the user before any other work.
2. **An ADR, after the probe passes.** It records VRL through cgo, run in Log processing, with the probe's numbers and the alternatives: WASM with wazero, OTTL, expr and CEL, and running at ingest.
3. **EventStore:** the runtime, the hook, recent arrivals and counts, and the routes.
4. **Console:** the table, saving, the page and the MCP tools.

## Testing Decisions

Tests check what a sender, an admin, a member or an agent sees, not how it's built. The Rust bridge has no tests of its own: it's covered through EventStore.

### EventStore root tests: the main seam

These are `main_test` tests on `TestEnv`.

**How a test works:**
1. It writes a `log_transform_versions` row into Console's test SQLite, as `seedProject` writes Retention.
2. It sends Logs through ingest (or `storeLogBatch`).
3. It runs Log processing.
4. It reads the result through the Query API.

**Cases:**
- A program that parses `message` sets Attributes that a Query filters on.
- A program that deletes `message` stores Logs without it.
- `abort` stores nothing for those Logs, and the cursor moves past the batch.
- A runtime failure stores the Log as it arrived, with `solidtrace.transform_error`.
- A program setting `solidtrace.x` gets `original.solidtrace.x`.
- A program setting `.level` to an unknown word gets an absent Level with `original.level`.
- A nested result is flattened.
- The `a`/`a.b` collision round-trips.
- A new version applies only to batches processed after it.
- A version that doesn't compile stores every Log as it arrived, with the error, and the stats report it.
- `http_request` and `get_env_var` are refused at compile time.
- An empty program and no row both leave Logs unchanged.
- Through the routes:
  - check reports spans;
  - preview runs on recent arrivals per Service and on given Logs, and stores nothing;
  - stats count processed, dropped and failed, and reset on a new version.

**Prior art:** `logs_processing_test.go`, `logs_retention_test.go` (`seedProject`), `logs_normalise_test.go`, `logs_promotion_test.go`.

### Console Rails tests, with EventStore stubbed by WebMock

- **Version model and save service:**
  - the newest version is current;
  - a refused check saves nothing and returns the errors;
  - saving the same text is a no-op;
  - restoring inserts a copy;
  - deleting a Project deletes the versions.
- **Controller:**
  - admins can save and restore;
  - a member's save is refused;
  - the page gives members the program read-only;
  - a program that doesn't compile is refused with the errors.
- **MCP**, in a new tools test beside `test/mcp/logs_tools_test.rb`, through `call_tool`:
  - the three tools;
  - admin against member for `update_log_transform`;
  - errors passed through;
  - each answer ending with the page's link.

**Prior art:** `test/mcp/logs_tools_test.rb`, `test/mcp/admin_tools_test.rb`, `test/services/event_store_test.rb` (`validate_logs_query`), the Projects controller tests.

### One Playwright flow on the dev stack

**The flow:**
1. An admin opens the Log transform page from the Project's settings.
2. They type a program with an error and see it underlined.
3. They fix it and preview it.
4. They save, send a test Log, and find the new Attribute in the Logs explorer.
5. They restore the previous version.
6. A member sees the page read-only.

No screenshots.

**Prior art:** `query-editor.spec.ts`, `project-keys.spec.ts`.

### How to run them

- Run only the relevant tests: `go test -p 1` for the touched root tests, and the touched Rails files. The user runs the full suites.

## Out of Scope

- Re-running a program over stored Logs, or any backfill.
- More than one program per Project, per-Service programs, or programs shared across an Organization.
- Transforming Events.
- Running at Log ingest, or refusing a sender's Logs because of a program.
- Reserving promoted columns for the Attributes a program sets. ADR 0005's ranking is unchanged, and `tagalys` has about 14 of its 100 slots free. This is to be revisited if a program's Attributes are left out.
- A per-Log timeout, and counts kept across restarts.
- VRL's network, environment and system functions, Vector's enrichment tables and secrets.
- VRL in the browser (the playground's WASM build).
- Marking in the Fields list which Attributes the program set.

## Further Notes

**The numbers behind this (2026-10-20 and 21):**
- `tagalys` went from 0.5–1.3 M Logs an hour to about 12 M at 22:00 UTC on 2026-10-20.
- `vmstat` showed reads pinned at about 140 MB/s, with `wa` up to 74%, while Dashboard 1 loaded.
- Moving its metric Charts from `message == "sidekiq_*"` to `isnotnull(<metric-only Attribute>)` made them about 13× faster. That speed-up is what this spec makes possible for any text.

**The first program:**
- Once this ships, write the `tagalys` program for `sidekiq.log` job lines with the user.
- Then rewrite Charts 23–26 on Dashboard 2 ("Sidekiq — job lines") to use its Attributes, and move them back to Dashboard 1.
- That needs a `ready-for-human` ticket, since it changes production data.

**Other notes:**
- Pebble deletes a Log batch once its DuckDB commit succeeds. After processing, a Log as it arrived survives only in the recent arrivals, which is why preview reads them rather than stored Logs.
- Newest-per-Service recent arrivals go a little beyond the grilling's "last ~200 Logs". The extra is so a draft aimed at a quiet Service, such as `sidekiq.log` lines among indexer Logs, still has Logs to run on.
- VRL is MPL-2.0. Linking it is fine, but changes to VRL's own files would have to be published. None are planned.
