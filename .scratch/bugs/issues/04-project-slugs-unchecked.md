# 04: The New Project page saves slugs that can't be reached

**Status:** ready-for-agent
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
