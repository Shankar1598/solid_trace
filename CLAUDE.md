# SolidTrace

Always talk in ASD-STE100 Simplified Technical English. Always read GLOSSARY.md, and use their ubiquitous language.

## Agent skills

### Issue tracker

Issues live as markdown files under `.scratch/<feature-slug>/`. `.scratch/` is a private Git repository of its own, cloned into this checkout and ignored by this repo. Project status: `.scratch/STATUS.md`. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, used as-is, recorded as a `Status:` line in each issue file. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Tests

GitHub Actions runs the full suites on each PR and push to `main`. Before a release, check that the CI run of the commit to tag is green, and record it in the Release record. See `docs/agents/testing.md`.

### Security

Before tagging a release, check GitHub's open security findings and settle each. See `docs/agents/security.md`.
