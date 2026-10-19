# 02: Seed the Meta Organization

**Status:** resolved
**Spec:** [spec.md](../spec.md), decisions 3 to 6 and 9

**What to build:** the Meta Organization, seeded once, owned by whoever set up the instance.

- [x] `deploy/install` generates the Meta Organization's project key into `deploy/.env` once, as it does `SECRET_KEY_BASE`, and Compose passes it to `db_prepare` and to Vector (03).
- [x] One idempotent seeding step, run by `db_prepare` after `db:prepare`: with the key set and the `instance_settings` flag unset, it creates the Organization "SolidTrace" with the Project "SolidTrace" whose project key is that key, sets the flag, and makes the oldest user, if any, its admin. Without the key (local development) it does nothing.
- [x] Sign-up: while the Meta Organization has no member, the first user to sign up becomes its admin.
- [x] A deleted Meta Organization or Project is not seeded again.
- [x] The Project gets the instance's default retention, like any new Project.
- [x] Tests: the seeding on an empty database, on one with users, run twice, and after the Organization is deleted; the first sign-up joins as admin, a later one does not.
- [x] Read [ADR 0001](../../../docs/adr/0001-eventstore-writes-console-tables.md) if the migration touches a table EventStore reads (`project_keys`).

## Comments

- The env var is **`META_PROJECT_KEY`** (32 hex characters). `deploy/install` writes it to `deploy/.env` once (`ensure_env_secret` (was `create_meta_project_key`)). Compose passes it to `db_prepare` only, whose command is now `bin/rails db:prepare meta_organization:seed`; the console environment is anchored as `&console-environment` so 03 can merge it the same way. Ticket 03 still has to pass it to Vector.
- `MetaOrganization.seed(key)` (`console/app/models/meta_organization.rb`) runs from the `meta_organization:seed` task. A migration adds `instance_settings.meta_organization_seeded` (the flag) and `meta_organization_id` (FK, nullified on delete), which sign-up uses to find it. EventStore reads `instance_settings` columns by name, so the new columns don't affect it; `project_keys` is unchanged.
- Sign-up: `MetaOrganization.join_first(user)` makes the user its admin while the Meta Organization has never had a member (archived ones count, so an archived admin is not replaced by whoever signs up next).
- Tests: `test/lib/tasks/meta_organization_task_test.rb`, `test/controllers/registrations_controller_test.rb`, plus `test/lib/tasks/instance_settings_task_test.rb` and `test/controllers/organizations_controller_test.rb`: 23 runs, 0 failures. I also checked `docker compose config` and ran `db:prepare meta_organization:seed` against the dev DB.
