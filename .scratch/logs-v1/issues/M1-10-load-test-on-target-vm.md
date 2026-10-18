# M1-10: Load test on the target VM

**What to build:** proof that the storage design holds at scale. Run EventStore on the target VM (4 vCPU, 16 GB) at 3–5 TB/month of uncompressed Logs (1.2–1.9 MB/s, about 1–6k Logs/s) with 5× bursts for 10 minutes. Spec: [Testing Decisions](../spec.md#testing-decisions) (Load test), [Further Notes](../spec.md#further-notes) (Known risks); user stories 34, 81.

Needs a VM the dev box can't stand in for (2 CPU, 11 GB) and runs far past the dev box's one-minute benchmark limit, so it is for a human.

**Blocked by:** M1-04, M1-05, M1-06, M1-09.

**Status:** ready-for-human

- [ ] A 5× burst for 10 minutes gets no `503` and loses no Log; Pebble drains afterwards.
- [ ] Log checkpoints, including the one after each hourly delete, don't stall Event writes; checkpoint durations are recorded at full scale.
- [ ] Per-Project hot tables and the daily set change hold up at scale.
- [ ] Once M2-01 lands: the newest Logs appear within about 2 s, and a day-long filter or group-by on a common Attribute finishes within about 5 s; record the numbers.
- [ ] Findings written up in the logs research folder, with any design changes filed as tickets.
