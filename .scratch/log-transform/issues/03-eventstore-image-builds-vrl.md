# 03: EventStore's image builds the VRL runtime

**What to build:** EventStore's Docker image, and the release workflow that builds it for amd64 and arm64 on native runners, build the VRL staticlib and link it, so a release carries the Log transform runtime. See the spec's "Build and tooling".

**Blocked by:** 01

**Status:** claimed

- [ ] The Dockerfile gains a Rust stage (Rust 1.95 or later) whose staticlib the Go stage links; the final image needs no Rust.
- [ ] The image builds locally on the dev box, and the binary in it starts and compiles a VRL program.
- [ ] The release workflow still builds both architectures; any cache or toolchain step it needs for Rust is added.
- [ ] The dev stack's EventStore build still works with the documented build step.
