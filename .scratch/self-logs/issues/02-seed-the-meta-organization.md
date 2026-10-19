# 02: Seed the Meta Organization

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md), decisions 3 to 6 and 9

**What to build:** the Meta Organization, seeded once, owned by whoever set up the instance.

- [ ] `deploy/install` generates the Meta Organization's project key into `deploy/.env` once, as it does `SECRET_KEY_BASE`, and Compose passes it to `db_prepare` and to Vector (03).
- [ ] One idempotent seeding step, run by `db_prepare` after `db:prepare`: with the key set and the `instance_settings` flag unset, it creates the Organization "SolidTrace" with the Project "SolidTrace" whose project key is that key, sets the flag, and makes the oldest user, if any, its admin. Without the key (local development) it does nothing.
- [ ] Sign-up: while the Meta Organization has no member, the first user to sign up becomes its admin.
- [ ] A deleted Meta Organization or Project is not seeded again.
- [ ] The Project gets the instance's default retention, like any new Project.
- [ ] Tests: the seeding on an empty database, on one with users, run twice, and after the Organization is deleted; the first sign-up joins as admin, a later one does not.
- [ ] Read [ADR 0001](../../../docs/adr/0001-eventstore-writes-console-tables.md) if the migration touches a table EventStore reads (`project_keys`).
