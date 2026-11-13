# AGPL-3.0 dependency scan

Research note, 2026-11-13. Answers [Do any dependencies conflict with AGPL-3.0?](../issues/14-agpl-dependency-conflicts.md).

## Summary

- **No dependency SolidTrace links, loads or bundles conflicts with AGPL-3.0.** I found no GPL-2.0-only, SSPL, BUSL, Commons Clause, Elastic or unlicensed code in the Go binary, the Rust library linked into it, the Rails process or the browser bundle.
- **One thing to act on: Console's image ships Brakeman**, under the custom Brakeman Public Use License. That licence allows non-commercial use only and forbids distributing it "as part of" a commercial product. It is a dev/test tool and never runs in production. It is there because the Dockerfile sets `BUNDLE_WITHOUT="development"`, and Brakeman is in `group :development, :test`, so Bundler still installs it. Fix: `BUNDLE_WITHOUT="development:test"` in `console/Dockerfile`. That also drops 32 other test gems from the image, including bundler-audit (GPL-3.0-or-later, compatible but needless).
- **Notes, not conflicts:**
  - The `vrl` crate (MPL-2.0) is statically linked into EventStore. MPL-2.0 combines with AGPL-3.0 (MPL §3.3). Its source files carry no "Incompatible With Secondary Licenses" notice. If SolidTrace ever patches vrl's files, those changes stay MPL-2.0 and must be published.
  - `rdoc` is "GPL-2.0 or the Ruby terms", with no BSD option. It is loaded only by `irb` (`rails console`), not by the server. The Ruby terms are permissive. Low risk.
  - Published images carry many third-party binaries (Debian's GPL tools, the Tailwind CLI with Bun and LGPL JavaScriptCore inside). They are separate programs, so they are mere aggregation under AGPL §5. Publishing images to ghcr.io still means passing on their licences and source offers. That is ordinary image hygiene, not an AGPL conflict.
  - Nothing ships a third-party notices file. MIT, BSD and Apache-2.0 require the notices to travel with copies (Go binary, Rust library, JS bundle). That is a follow-up for [ticket 15](../issues/15-licence-follow-ups.md), not a conflict.
- Every runtime service image (ClickHouse, Caddy, Vector) is Apache-2.0 or MPL-2.0, and runs as a separate process.

## Method

- **Go:** `go list -deps ./...` in `event_store/` (with and without `-test`, same 113 modules), offline (`GOPROXY=off`). Licence from the LICENSE/COPYING/NOTICE files in `~/go/pkg/mod`, matched by text. Every module had a licence file.
- **Rust:** `cargo tree --offline -e normal --target x86_64-unknown-linux-gnu` in `event_store/vrl/rust`, licence from each crate's `license` field. Build-only crates came from `-e build,normal --target all`.
- **Ruby:** `Bundler.definition.specs_for(...)` in `console/`, licences from the installed gemspecs. Scope by group: `default` = loaded in the Rails process; `test` = installed in the image but not loaded (see Brakeman above); `development` = not in the image. I checked Bundler's rule in its source: `Definition#dependencies_for` keeps a gem when `d.groups.intersect?(requested_groups)` (`lib/ruby/4.0.0/bundler/definition.rb`, Bundler 4.0.16). So a gem in `:development, :test` is installed unless both are excluded.
- **JavaScript:** parsed `console/bun.lock` (603 packages). Licence from `~/.bun/install/cache/<name>@<version>@@@1/package.json`. "Browser" = reachable through `dependencies` from the bare imports in `console/app/frontend` (an upper bound). Everything else is build/test-only: the Dockerfile deletes `node_modules` after `assets:precompile`, so only the Vite bundle ships.
- **Images:** `console/Dockerfile`, `event_store/Dockerfile`, `deploy/docker-compose.yml`, `docker-compose.dev.yml`, `console/config/deploy.yml`.
- Scripts and raw lists are in `~/.cache/solid-trace/agpl-scan/`.
- Not scanned: `.claude/worktrees/` (excluded), `.ruby-lsp/Gemfile.lock` (editor tooling). Nothing is vendored: `console/vendor/` holds only `.keep` files, and no tracked source file carries a third-party copyright header.

Compatibility judgements follow the FSF licence list ([gnu.org/licenses/license-list.html](https://www.gnu.org/licenses/license-list.html)). It lists Apache-2.0 as compatible with GPLv3, MPL-2.0 as compatible with AGPLv3 through §3.3, and Boost, Unicode and the Ruby licence (through dual licensing) as GPL-compatible.

## Problems and unknowns

| Dependency | Licence | How it is used | Verdict |
|---|---|---|---|
| brakeman 8.1.0 (gem) | Brakeman Public Use License (custom) | Dev/test tool. Installed in Console's published image, never run there | **Problem, easy fix.** §3 grants use "only for the purpose of non-Commercial Use"; §2 counts "Distributing the Software as a commercial product or as part of one" as commercial; §4 allows redistribution only when it doesn't conflict with that ([LICENSE.md](https://raw.githubusercontent.com/presidentbeef/brakeman/main/LICENSE.md)). Not AGPL-combinable either. Exclude the test group from the image. Using it in CI on your own code is the licence's own example of non-commercial use. |
| rdoc 7.1.0 (gem) | GPL-2.0 or the Ruby terms (`LICENSE.rdoc`) | Ruby library, required by `irb` (railties → irb → rdoc). Loaded by `rails console`, not by Puma or the jobs | Low risk. The GPL-2.0 option doesn't fit AGPL-3.0. The Ruby-terms option is permissive (clause 4: "You may modify and include the part of the software into any other software (possibly commercial)"). No action needed. |
| vrl 0.37.0 (crate) | MPL-2.0 | Statically linked into the EventStore binary (cgo, `libst_vrl.a`) | Compatible (MPL §3.3). Unmodified, from crates.io. Keep it unmodified, or publish any changes to its files under MPL-2.0. |
| http-accept 1.7.0 (gem) | none in gemspec | Loaded (rest-client dependency) | Resolved: its README says "Released under the MIT license." |
| @hugeicons/core-free-icons 3.3.0 (npm) | `package.json` says MIT; README says "All rights reserved"; no LICENSE file | In the browser bundle | Resolved: the upstream repo says "The free icons (@hugeicons/core-free-icons) and all source code in this repository are released under the MIT License" ([github.com/hugeicons/hugeicons](https://github.com/hugeicons/hugeicons)). Pro packs (`@hugeicons-pro/*`) are restricted, but not used. |
| @fontsource-variable/ibm-plex-sans, jetbrains-mono | OFL-1.1 | Font files in the CSS bundle | Fine. OFL lets fonts be bundled with any software, and the fonts stay OFL. Keep their notice. |
| tailwindcss-ruby 4.1.18 (gem, `default` group) | MIT | Ships the standalone `tailwindcss` CLI in the runtime image. The binary embeds Bun v1.3.3 (it contains JavaScriptCore strings). Runs only at `assets:precompile` (tailwindcss-rails hooks it) | Separate program, so aggregation, not a conflict. It seems unused: the frontend builds Tailwind through `@tailwindcss/vite`, and no layout links `tailwind.css`. Dropping `tailwindcss-rails`/`tailwindcss-ruby` would remove it. Optional. |
| 70 npm platform binaries (`@esbuild/*`, `@rollup/rollup-*`, `@tailwindcss/oxide-*`, `lightningcss-*`, `fsevents`, plus `gensync`, `@rolldown/pluginutils`) | not in the local cache | Build-only | Unknown locally. They are platform builds of esbuild, Rollup, Tailwind Oxide and Lightning CSS. Their installed linux-x64 siblings are MIT (MPL-2.0 for lightningcss). None ship. |

## Counts for the rest

### Go: EventStore binary (linked)

113 external modules, all statically linked into `event_store`. The test build adds none.

| Licence | Modules |
|---|---|
| Apache-2.0 | 59 |
| MIT | 21 |
| BSD-3-Clause | 20 |
| Apache-2.0 + BSD-3-Clause files (OpenTelemetry) | 8 |
| BSD-2-Clause | 3 |
| Apache-2.0 + MIT (yaml.v3) | 1 |
| Apache-2.0 + BSD-3-Clause + MIT (klauspost/compress) | 1 |

All are compatible. Apache-2.0 works only with (A)GPL v3, which SolidTrace uses. go-sqlite3 (MIT) compiles in the SQLite amalgamation, which is public domain. The Go standard library is BSD-3-Clause. Notable names: pebble (BSD-3), clickhouse-go and ch-go (Apache-2.0), AWS and GCP SDKs (Apache-2.0), fiber/fasthttp (MIT), DataDog/zstd (BSD-3).

Other `go.mod` files ship nothing: `docs/traces/go` (example: sentry-go MIT, x/sys and x/text BSD-3), `event_store/testdata/transactions/capture-go` (test fixture, same deps), `.scratch/eventstore-performance/research/sonic-bench` (research: sonic Apache-2.0, golang-asm BSD-3, cpuid MIT).

### Rust: `libst_vrl.a`, linked into EventStore through cgo

256 crates linked (plus 21 proc-macros that run only at build time, and 62 build-only crates).

| Licence | Crates |
|---|---|
| MIT OR Apache-2.0 | 159 |
| MIT | 46 |
| Unicode-3.0 | 15 |
| Apache-2.0 | 10 |
| Unlicense OR MIT | 7 |
| BSD-3-Clause | 7 |
| Zlib | 2 |
| MPL-2.0 (vrl, above) | 1 |
| BSL-1.0 (xxhash-rust; this is the Boost licence, not Business Source) | 1 |
| Apache-2.0 OR BSL-1.0 (ryu) | 1 |
| ISC, MIT-0, CC0-1.0, 0BSD | 1 each |
| BSD-2-Clause OR MIT OR Apache-2.0 | 1 |
| (MIT OR Apache-2.0) AND Unicode-3.0 | 1 |
| (MIT OR Apache-2.0) AND BSD-3-Clause (encoding_rs) | 1 |

All are compatible. `onig_sys` bundles Oniguruma's C source (BSD-2-Clause). Build-only crates are MIT/Apache/Unlicense/Zlib, and two offer LGPL-2.1-or-later as one option. No `ring` or OpenSSL.

### Ruby: Console

172 gems in the lock. 131 are loaded in the Rails process, 33 are test gems installed in the image (see Brakeman), and 8 are development-only (not in the image).

Loaded in the Rails process (131):

| Licence | Gems |
|---|---|
| MIT | 91 |
| Ruby OR BSD-2-Clause | 23 |
| Apache-2.0 | 7 |
| Ruby (json and reline also offer BSD-2 in COPYING; rdoc above) | 3 |
| BSD-3-Clause (puma, sqlite3) | 2 |
| BSD-2-Clause | 1 |
| MIT OR Apache-2.0, MIT OR BSD-2-Clause, BSD-2/BSD-3/MPL-2.0 | 1 each |
| none declared (http-accept, MIT per README) | 1 |

Native gems bundle C libraries under permissive licences. nokogiri ships libxml2, libxslt, libgumbo and zlib (per its `LICENSE-DEPENDENCIES.md`). sqlite3 ships SQLite (public domain), and zstd-ruby ships zstd. thruster ships a Go binary (MIT; its deps are compress, x/crypto, x/net and x/text, all permissive), which runs as a separate process.

In the image but not loaded (33): 25 MIT, 2 Ruby OR BSD-2, 2 BSD-2, 2 Apache-2.0 (selenium-webdriver, addressable), 1 GPL-3.0-or-later (bundler-audit), 1 Brakeman Public Use License.

Development-only (8): 6 MIT, 1 Ruby OR BSD-2, 1 ISC.

### JavaScript: Console

603 packages in `bun.lock`. Only the Vite bundle reaches the image or browsers.

In the browser bundle (185, upper bound):

| Licence | Packages |
|---|---|
| MIT | 167 |
| ISC (lucide-react and others) | 12 |
| BSD-3-Clause (d3-ease, qs) | 2 |
| OFL-1.1 (fonts, above) | 2 |
| Apache-2.0 (class-variance-authority) | 1 |
| MIT AND ISC (victory-vendor) | 1 |

Build/test-only (418): 298 MIT, 70 not cached (above), 21 ISC, 9 Apache-2.0, 6 BSD-3, 5 BSD-2, 3 MPL-2.0 (lightningcss), 2 BlueOak-1.0.0 (minimatch, isexe), 2 0BSD, 1 CC-BY-4.0 (caniuse-lite data), 1 MIT OR CC0. None ship.

### Images and bundled binaries

| Image | Licence of the main software | Use |
|---|---|---|
| `clickhouse/clickhouse-server:26.8.34.13` | Apache-2.0 | Separate process (EventStore talks to it over TCP) |
| `caddy:2.11.8-alpine` | Apache-2.0 | Separate process, HTTPS profile only |
| `timberio/vector:0.60.0-alpine` | MPL-2.0 | Separate process (ships container logs) |
| `debian:bookworm-slim` | Debian (mixed: GPL, LGPL, permissive) | EventStore's final base. Adds `libsqlite3-0` (public domain) and `ca-certificates` |
| `ruby:4.0.9-slim` | Debian base + Ruby (Ruby OR BSD-2) | Console's base. Adds `libjemalloc2` (BSD-2, `LD_PRELOAD`), `libvips` (LGPL-2.1+, installed but no gem uses it), `sqlite3`, `curl` |
| `rust:1.99-slim-bookworm`, `golang:1.27-bookworm`, `oven/bun:1.3.14` | — | Build stages only, not in the final images |
| `bitnamilegacy/minio:2025.7.23-debian-12-r5` | AGPL-3.0 | `docker-compose.dev.yml` only |

EventStore's binary links glibc dynamically (LGPL-2.1+) and the GCC runtime (runtime exception). Both are fine. The OS packages in the bases include GPL-2.0-only programs. They run as separate programs, so AGPL §5 treats them as an aggregate, not a combination.
