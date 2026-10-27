# Security findings before a release

GitHub scans the repository on its own, and a scan that finds something doesn't fail or email anyone. Findings wait in the Security tab, so check them before every release.

## What scans what

| Scan | Covers | Runs |
|---|---|---|
| Dependabot alerts and security updates | Gems, bun packages, Go modules, the VRL crate, base images, Actions | When an advisory is published, then it opens a fix PR |
| Dependabot version updates (`.github/dependabot.yml`) | The same, minor and patch grouped per part | Weekly PRs |
| CodeQL (default setup) | Go, Ruby, JS/TS, the workflows | Each push to `main`, PRs, weekly |
| Secret scanning with push protection | Keys and tokens | Each push; a push carrying one is blocked |
| Scan images (`.github/workflows/scan-images.yml`) | The images `deploy/docker-compose.yml` runs, as `bin/security images` does | Weekly, after each release's images are built, by hand |

`bin/security` runs the local equivalents, and its comment lists them.

## Before tagging a release

List what's open, from the repository's root:

```bash
R=repos/Shankar1598/solid_trace
gh api "$R/dependabot/alerts?state=open" --paginate \
  --jq '.[] | "\(.security_advisory.severity)\t\(.dependency.package.name)\t\(.dependency.manifest_path)\t\(.security_advisory.ghsa_id)"'
gh api "$R/code-scanning/alerts?state=open" --paginate \
  --jq '.[] | "\(.tool.name)\t\(.rule.security_severity_level // .rule.severity)\t\(.most_recent_instance.category)\t\(.rule.id)\t\(.most_recent_instance.location.path)"'
gh api "$R/secret-scanning/alerts?state=open" --paginate \
  --jq '.[] | "\(.secret_type_display_name)\t\(.html_url)"'
gh pr list --author app/dependabot
```

Each open finding then needs one of these before tagging:

- **Fix it** in the release, usually by merging Dependabot's PR or bumping the dependency.
- **Accept an image's CVE** in `.trivyignore.yaml`, with a statement and an `expired_at` date, as the file's comment explains. The next scan closes its alert.
- **Dismiss a CodeQL or Dependabot alert** in GitHub, with the reason (false positive, used in tests, won't fix) and a comment saying why.
- **Leave it open on purpose** when no fix exists yet. Name it in the Release record, as Caddy's findings are.

A secret alert is never left open: revoke the key first, then close the alert.

Record the outcome in the changelog's Release record (see `testing.md`): what was fixed, accepted or left open. Name any new finding since the last release.

After the release's images are built, Scan images runs on its own. Check its alerts again, as the Release record has recorded `bin/security images` until now.
