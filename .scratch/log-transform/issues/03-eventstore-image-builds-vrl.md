# 03: EventStore's image builds the VRL runtime

**What to build:** EventStore's Docker image, and the release workflow that builds it for amd64 and arm64 on native runners, build the VRL staticlib and link it, so a release carries the Log transform runtime. See the spec's "Build and tooling".

**Blocked by:** 01

**Status:** resolved

- [ ] The Dockerfile gains a Rust stage (Rust 1.95 or later) whose staticlib the Go stage links; the final image needs no Rust.
- [ ] The image builds locally on the dev box, and the binary in it starts and compiles a VRL program.
- [ ] The release workflow still builds both architectures; any cache or toolchain step it needs for Rust is added.
- [ ] The dev stack's EventStore build still works with the documented build step.

## Answer

Built and merged (`09ec478`, 2026-10-21). A `vrl` stage on `rust:1.99-slim-bookworm` (dependency layer, then the crate) builds the staticlib with a new `dist` profile (fat LTO: 31 MB against 75 MB, about the same build memory) and the Go stage links it from `vrl/lib/linux_${TARGETARCH}/`. The final image needs no Rust. Proved on amd64: the image's binary contains the runtime's symbols and starts, and the `vrl` test binary built in the image's Go stage passes all 12 tests inside the final image. A cold image build takes about 9–10 minutes on 2 cores. Not verified: an arm64 build, a real release workflow run (actionlint passes; only a comment changed), and the Rust layer's share of the gha cache.
