# 15: Which licence follow-ups land, and when?

Type: grilling
Status: open
Blocked by: 14
Map: [SolidTrace Software Factory](../map.md)

## Question

Follow-ups from [Which licence does SolidTrace take?](03-licence.md), suggested in [research/fable-opinion.md](../research/fable-opinion.md), section 3. Which does the user want, and before which release?

- The README says "version 3", not "version 3 or later". Which is meant?
- A link to the source in Console's footer, so a modified install can meet AGPL's offer of source.
- The conflicts [Do any dependencies conflict with AGPL-3.0?](14-agpl-dependency-conflicts.md) found. Console's image ships Brakeman under a non-commercial licence; the fix is `BUNDLE_WITHOUT="development:test"` in `console/Dockerfile`. A third-party notices file for the images.
- MIT for files that run beside a team's own code (the run entrypoint, example job files, templates), once the factory module adds them.
- A contributor agreement before outside contributions, so the user stays able to relicense.

The user's employment agreement with Tagalys is theirs to check, not a ticket.
