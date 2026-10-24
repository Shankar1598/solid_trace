# 03: Source counts, the Kept step and Biggest sources

Spec: [../spec.md](../spec.md)

**What to build:** The band gains its middle step: Received → Kept → Stored. Kept is the Logs and bytes actually stored, after unreadable Logs and Logs the Log transform dropped. The dropped slice between Received and Kept is hatched in two tones, unreadable and dropped by the Log transform. The compression ratio becomes kept bytes over stored bytes.

A **Biggest sources** section lists the Services and files that sent the most Logs, and the most bytes, in the range. Each shows its Project, its share of the total, and how many of its Logs the Log transform dropped. A source opens the Logs explorer filtered to its Service and file.

End to end:

- Log processing writes a new `log_source_counts` summing table: per Project, the Logs' own UTC day, Service and `file`, with Logs, bytes and Logs dropped by the Log transform.
- It goes in the same insert, with the same dedup token, as the key counts, so it's exact on retries.
- A Log's bytes are its message plus its Attributes' JSON, measured after the Log transform. A dropped Log counts under the Service and `file` it arrived with.
- The table is partitioned like `log_key_counts` and joins the day tables, so Retention, Project deletion and the disk limit drop it.
- The route and `get_overview` answer Kept and the sources.

**Blocked by:** 01

**Status:** resolved

- [x] Logs from two Services and files, some dropped by a Log transform, come back from `/overview` as the right kept Logs and bytes, transform drops and ranked sources (Go route test)
- [x] A Log processing retry doesn't count a source twice (Go route test)
- [x] Retention and Project deletion remove a Project's Source counts with its Logs (Go route test)
- [x] `get_overview` includes Kept and Biggest sources (MCP tool test)
- [x] 01's Playwright flow also sees the Kept step and a source in Biggest sources, and opens it into the Logs explorer filtered to that Service and file

## Answer

Built on `overview/03-source-counts-kept-and-biggest-sources`, merged into `feature/overview` as `9023bcb` (2026-10-24). Details are in `~/.cache/solid-trace/overview-notes/notes.md` under "03 result".

Tests run on the branch after the merge:
- Go: `-run 'Overview|SourceCounts|DropsAll|InsertedAgain|SQLCant|IngestCounts|Overloaded|ReadTimes|Health|Waiting' . ./handler` passed.
- Rails: the Overview controller, MCP and health tests, 22 runs, 0 failures.
- `tsc` is clean.
- The flow passed 2 of 2 on the implementer's own stack.
- The route answered in about 30 ms.

Choices:
- **Sources with no file.** A source with no Service or file opens the Logs explorer with `isempty(...)`, in Query mode, since `file == ""` matches nothing.
- **The dropped slices** are sized by their share of the Logs received, since dropped bytes aren't counted. The dev data has no drops, so they haven't been seen.
- **Kept's colour.** Kept uses chart-2, Project 2's colour; noted for 07.

While it was being built, this ticket file was empty: the orchestrator's status script had truncated it. The implementer built from the spec. The file was restored afterwards.
