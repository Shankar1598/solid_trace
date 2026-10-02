# M1-09: Retention, disk limit and Project deletion

**What to build:** Logs never fill the disk and never outlive their Project. Each Project's Logs are deleted past its Retention, an instance-wide disk limit deletes the oldest Logs first, and a deleted Project's Logs go with it. Spec: [Log storage](../spec.md#log-storage-eventstore) (Retention, Disk limit, Project deletion); [Further Notes](../spec.md#further-notes); user stories 18, 29–33.

**Blocked by:** M1-02, M1-03, M1-08.

**Status:** ready-for-agent

- [ ] A daily pass inside EventStore deletes each Project's `day=` folders past its effective Retention (read from Console's SQLite), with those days' promoted-set and popularity rows.
- [ ] Normalisation's "older than Retention" time rule uses the Project's effective Retention instead of the fixed default.
- [ ] One instance-wide disk limit for Log files; when exceeded, the oldest hour files are deleted first, from any Project. Where the limit is configured and its default (for example a share of free disk) are the implementer's call; record the choice in this ticket's Comments.
- [ ] **Project deletion is detected when a Project's row disappears from Console's `projects` table** (not when its keys vanish: keys are hard-deleted and a Project can briefly have none). EventStore then drops its hot table, its `logs_late` rows, its folders and its promoted-set and count rows. Console has no Project destroy route today, so this path is exercised by tests only.
- [ ] Tests (controllable clock): Retention deletes the right days for the default and an override; the disk limit deletes the oldest hours across Projects; a removed `projects` row deletes that Project's Logs and leaves others; deleting a Project's last key deletes nothing.
