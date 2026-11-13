# 14: Do any dependencies conflict with AGPL-3.0?

Type: research
Status: resolved
Map: [SolidTrace Software Factory](../map.md)

## Question

SolidTrace takes AGPL-3.0 ([Which licence does SolidTrace take?](03-licence.md)). Scan every dependency SolidTrace ships or links: Go modules (`go.mod` files, including `event_store/`), Ruby gems (`console/Gemfile.lock`), JavaScript packages (`console/` and any other `package.json`), and the base images and binaries in the Dockerfiles (ClickHouse and any others).

For each licence found, say whether it can be combined with AGPL-3.0 (GPL-2.0-only, SSPL, BUSL, Commons Clause, unlicensed and custom licences are the ones to flag), and how the dependency is used (linked, bundled, a separate process, build-only). List only the problems and the unknowns; a count per licence is enough for the rest.

## Answer

Resolved 2026-11-13 (research). Full scan: [research/agpl-dependency-scan.md](../research/agpl-dependency-scan.md).

- **No conflicts in anything SolidTrace links, loads or bundles.** That covers 113 Go modules, 256 Rust crates in the VRL library, 131 gems loaded by Rails, and 185 packages in the browser bundle. All are MIT, BSD, ISC, Apache-2.0, Unicode, Zlib, OFL fonts, or MPL-2.0 (`vrl`, which combines with AGPL through MPL §3.3).
- **One fix:** Console's image ships Brakeman (Brakeman Public Use License: non-commercial only, and no distribution as part of a commercial product). The Dockerfile's `BUNDLE_WITHOUT="development"` keeps `:development, :test` gems. Change it to `development:test`.
- **Notes:** `rdoc` is GPL-2.0 or Ruby terms, loaded only by `rails console`, low risk. Service images (ClickHouse and Caddy Apache-2.0, Vector MPL-2.0) are separate processes. Nothing ships a third-party notices file yet, which belongs with the [licence follow-ups](15-licence-follow-ups.md).
