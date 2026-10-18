# 23: Merge feature/logs-v2

**What to do:** Decide when to merge `feature/logs-v2` (which contains `feature/logs-v1`) into `feature/logging` and `main`, or open a pull request. The Go worktree `../solid_trace-v2-go` (`feature/logs-v2-go`, level with `feature/logs-v2`) can be removed once nothing else is planned on that track.

**Status:** resolved

## Comments

### 2026-10-18 resolved

Merged into `main` with `--no-ff`, by way of `feature/mcp`, which holds `feature/logs-v2` and `feature/logs-v1`, then `feature/console-bugs` and the newer `feature/logs-v2` (`deploy/install`, `deploy/setup`). There were no conflicts. `feature/logging` isn't updated; `main` holds it. The Go worktree `../solid_trace-v2-go` can be removed.
