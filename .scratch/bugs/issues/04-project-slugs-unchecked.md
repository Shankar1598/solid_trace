# 04: The New Project page saves slugs that can't be reached

**Status:** resolved
**Area:** Console
**Found:** 2026-10-17, by the MCP build's ticket 09 (09-review-1)

## What happens

A Project's slug is `name.parameterize` (`Project#generate_slug`). Nothing checks it, and `index_projects_on_slug` isn't unique. Projects are found by `find_by!(slug:)` within the Organization, and their pages and Queries (`['slug']`) name them by slug. So the New Project page saves Projects that can't be reached:

- **Empty slug.** A name with no Latin letters or digits, such as `日本語`, `Проект` or `!!!`, gives slug `""`. The Project's link is the Projects list.
- **Reserved slug.** The name `New` gives slug `new`, and `/:org_slug/projects/new` is the New Project page.
- **Duplicate slug.** Two Projects named `Shop` in one Organization both get `shop`. Only one of them can ever be found.

The MCP's `create_project` already refuses all three (`console/app/mcp/mcp_tools/create_project.rb`, `c807d38`), but the UI and the model don't.

## Fix

- Move the tool's three checks onto `Project` as validations: a slug present, not `new`, and unique per Organization. The tool and the New Project form then share them, and the tool's own checks can go.
- Add a unique index on `[organization_id, slug]`. First look for existing duplicates; if there are any, report them rather than rename them.
- Model tests, and a controller test that the form answers the error.

## Comments

### 2026-10-18 resolved

The checks are now on `Project`, so the New Project form and `create_project` share them:

- **Validations:** a name is required, and on create the slug must be present, not `new`, and not another Project's in the Organization.
- **Where errors land:** on `name`, since the slug is made from it. The form shows them under the field. The MCP answers them as "Not saved: Name …".
- **create_project:** its own copies of the checks are gone.
- **Existing Projects:** the checks run on create only, so a Project made before them can still be saved.
- **Factory:** default Project names now carry a sequence number. The factory took names from `Faker::App.name`, so two Projects in one Organization could have collided at random.

Tests:

- `test/models/project_test.rb` covers each case.
- `test/controllers/projects_controller_test.rb` checks that the form answers a duplicate slug's error.
- `test/mcp/admin_tools_test.rb` covers the tool's answers, now including a blank name.
- Five of these fail without the model change. The whole Rails suite passed with the model change (554 runs).

**No unique index, unlike the fix above suggested.** An install that already has duplicate slugs would fail to migrate, and the validation covers every way a Project is made today: the form, the MCP and the factories. Duplicates made before this fix stay as they are; nothing renames them.

