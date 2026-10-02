# M3-04: SQL mode in the explorer

**What to build:** on an instance with SQL queries on, a member switches to SQL mode, starts from the readable SQL of the current Query, and runs it; a shared link with SQL never runs on open. Spec: [Console](../spec.md#console) (Proxying); [Logs explorer](../spec.md#logs-explorer-console-frontend) (Bar, SQL mode, URL); user stories 67, 68, 70, 71, 74.

**Blocked by:** M3-02, M3-03.

**Status:** ready-for-agent

- [ ] Console's EventStore client gains readable SQL and Run SQL; Console refuses both when the setting is off.
- [ ] The switch shows `Builder | Query | SQL` when the setting is on and `Builder | Query` when off.
- [ ] SQL mode opens with the readable SQL of the current Query, editable, run only on Run. Chips and histogram are hidden.
- [ ] Leaving SQL mode with edits asks before discarding them (one-way).
- [ ] The URL carries the SQL text; SQL never runs on page load, only when Run is pressed.
- [ ] Console request tests: SQL operations refused and SQL mode hidden while the setting is off.
- [ ] vitest: SQL never auto-runs from a URL; the one-way discard prompt.
