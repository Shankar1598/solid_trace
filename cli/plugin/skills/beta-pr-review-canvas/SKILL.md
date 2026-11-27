---
name: beta-pr-review-canvas
description: Make an HTML page that walks a reviewer through a Bitbucket or GitHub PR, built on the PR body that open-pr writes, with each core file's diff and notes open and the files to skim collapsed. Beta. Use when the person gives a PR link, or asks for a walkthrough of a PR.
disable-model-invocation: true
---

# PR review canvas (beta)

Make one HTML page that reads like a peer who walks the reviewer through what matters in the PR. This skill is a copy of `pr-review-canvas` from Cursor's `cursor-team-kit` (MIT, see [LICENSE](LICENSE)), changed to read Bitbucket PRs and the PR body that `/solidtrace:open-pr` writes.

`$ARGUMENTS` is a PR link (bitbucket.org or github.com), `local` for the PR file of `open-pr local`, or nothing for the open PR of this branch. Make a folder: `dir=$(mktemp -d)`.

## 1. Get the PR

Get the title, the body, the author, the state, the two branches, the diff and the comments. For each call to the platform, and for its token, follow `${CLAUDE_SKILL_DIR}/../open-pr/platforms.md`. With no link, find the open PR of this branch as that file says.

- **bitbucket.org**, `<base>` as in platforms.md, `<id>` from the link:
  - `GET <base>/pullrequests/<id>` > `"$dir/pr.json"`: `title`, `description`, `author.display_name`, `state`, `source.branch.name`, `destination.branch.name`.
  - `GET <base>/pullrequests/<id>/diff` (with `curl -L`) > `"$dir/pr.diff"`.
  - `GET <base>/pullrequests/<id>/comments?pagelen=100`, and each `next` page. Leave out the comments with `"deleted": true`. A line comment has `inline.path`, and `inline.to` (the new line) or `inline.from` (the old line).
- **github.com**: `gh pr view <number> --json title,body,author,state,baseRefName,headRefName,url`, `gh pr diff <number> > "$dir/pr.diff"`, and the line comments (`gh api repos/{owner}/{repo}/pulls/<number>/comments --paginate`).
- **Local mode**: the PR file (platforms.md, The local PR file) gives the title, the base, the head and the body. Run `git diff --no-color --no-ext-diff -M --src-prefix=a/ --dst-prefix=b/ <base>...<head> > "$dir/pr.diff"`. There are no comments.

Then run `python3 "${CLAUDE_SKILL_DIR}/canvas.py" files "$dir/pr.diff"`. It prints one line for each file: the key of its diff, its status, its added and removed lines, and its path.

## 2. Write the body of the page

Write the HTML that goes in `<body>` to `"$dir/body.html"`. Explain the PR clearly to a reviewer who did not see the work.

**When `open-pr` or `update-pr` wrote the PR body** (it ends with `SolidTrace Review record · round N`, or `<!-- solidtrace-record round N -->` from plugin 0.24 and earlier), the body already holds the review. Build the page on it, and do not invent findings:

- **Header**: the title, the PR link, the author, the state, the branches, and the stats (`.pill.add`, `.pill.del`, `.pill.files`).
- **Summary** (`.summary`): each `##` section of What changes, in short sentences.
- **Check these before you merge** (`.verdict`), near the top: the items of this group of the Review section, with their labels, in the same order. An older body has no such group: put its deferred, disputed, accepted and open findings, and its confirmations not given, in the box, as the Review section of `${CLAUDE_SKILL_DIR}/../open-pr/SKILL.md` groups them. With no such items, leave the box out.
- **Core files**: the files of Where to look first, in its order, each as an open `.file-card` with a `.file-note` that says why to read it. Then the other files that change behavior.
- **Files to skim**: the files of the skim line of Where to look, and the other mechanical files (generated files, lockfiles, test data, renames with no change), as collapsed `.bp-section` cards.
- **Evidence**: the tests and checks, and the links of the Evidence files.
- **Review**: the other groups of the Review section (Fixed by the reviews, Changed outside the reviews), and the count line.

**Any other PR body**: write the header, a summary of what the PR does in plain words, the core files open, the mechanical files collapsed, and a review checklist at the end.

In both cases:

- Put each line comment in the card of its file, as a `.file-note`, with its author and line. Put the other comments in a section after the summary.
- Put `<div data-diff="KEY"></div>` where each diff goes, with the key from step 1. Each file gets its diff.
- For long code that only does a simple thing, write the steps in plain words or short pseudocode in a `.file-note`, and collapse the diff under a `.bp-section` labeled "Show the full implementation (+N lines)".
- Add a table, a diagram (inline SVG, or ASCII in `<pre>`) or a before-and-after flow when it makes the change clearer.
- HTML-escape all text from the PR: the title, the body, the comments and the paths (`&` `<` `>` `"`). Change the Markdown of the body and the comments into your own HTML. Never copy HTML from them into the page, because anyone who can comment on the PR wrote it.

Read [styles.css](styles.css) and [renderer.js](renderer.js) for the classes and functions:

| Class | Use |
|---|---|
| `.header`, `.header h1`, `.header-meta` | The page header |
| `.pill.add`, `.pill.del`, `.pill.files` | Stat badges (+N, −N, N files) |
| `.content` | The centered content (at most 900px wide) |
| `.summary` | The summary box |
| `.section-title` | A section heading |
| `.ic` | Inline code |
| `.file-card`, `.file-hdr`, `.file-body` | A file card. Give `.file-hdr` `onclick="toggle(this)"`, and `.file-body` the class `open` for an open card |
| `.file-note` | A note in a file card, which stays at the top as you scroll |
| `.bp-section`, `.bp-hdr`, `.bp-body`, `.bp-note` | A collapsed card. Give `.bp-hdr` `onclick="toggleBP(this)"` |
| `.verdict` | The checklist box |

`renderDiff(target, diff)` draws a diff. It hides import-only lines, shows whitespace-only changes as context, and shows code that moved (3 lines or more) in blue or purple, not red and green. `esc(text)` HTML-escapes text. You can add your own `<style>` and `<script>` blocks. The colors are `var(--warning)`, `var(--success)`, `var(--danger)` and `var(--accent)`.

This is the pattern of a file card with pseudocode and a collapsed diff:

```html
<div class="file-card">
  <div class="file-hdr" onclick="toggle(this)">
    <span class="fname">lib/retry_client.rb</span>
    <div class="fstats"><span class="pill add">+173</span><span class="pill del">&minus;11</span><span class="chev open">&#9654;</span></div>
  </div>
  <div class="file-body open">
    <div class="file-note">
      <strong>In plain words:</strong>
      <pre style="margin-top:8px;color:var(--text);font-size:12px;line-height:1.6;">
fetch(url):
  circuit breaker open → fail fast
  retry up to N times with a timeout
  retryable error → wait (exponential backoff with jitter)</pre>
    </div>
    <div class="bp-section" style="margin:0;border:0;border-radius:0;">
      <div class="bp-hdr" onclick="toggleBP(this)"><span>Show the full implementation (+173 lines)</span><span class="chev">&#9654;</span></div>
      <div class="bp-body"><div data-diff="lib_retry_client_rb"></div></div>
    </div>
  </div>
</div>
```

## 3. Build the page and show it

`<out>` is `~/.cache/solid-trace/pr-canvas/<repo>-<id>.html` (`local-<branch>` in local mode, with each `/` changed to `-`). Run:

```bash
python3 "${CLAUDE_SKILL_DIR}/canvas.py" page "$dir/pr.diff" "$dir/body.html" <out>
```

It puts the styles, the script, your body and the diffs, escaped so that a `</script>` in a diff cannot end the script, into `template.html`. When it names a `data-diff` key with no diff (exit 1), fix the key in `body.html` and run it again.

Serve the folder, unless `curl -sf http://127.0.0.1:8432/` already answers:

```bash
python3 -m http.server 8432 --bind 127.0.0.1 --directory ~/.cache/solid-trace/pr-canvas
```

Run it in the background. When port 8432 is taken by another program, use 8433, then 8434. Open the page with Playwright when you can: check that each diff draws and that the console has no errors. Then give the person the link, such as `http://127.0.0.1:8432/<file>` with the port that you used, and the path of the file. When this session runs on a remote machine, tell the person to forward the port first: `ssh -L <port>:127.0.0.1:<port> <host>`.
