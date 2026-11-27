#!/usr/bin/env python3
"""Split a PR's unified diff into one patch per file, and build the canvas page.

  canvas.py files DIFF                         one line per file: key, status, +adds, -dels, path
  canvas.py page DIFF BODY OUT                 write the page: template + styles + renderer + BODY + patches
"""
import json
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def unquote(path):
    if len(path) >= 2 and path[0] == '"' and path[-1] == '"':
        path = path[1:-1].encode("latin-1", "backslashreplace").decode("unicode_escape").encode("latin-1").decode("utf-8", "replace")
    return path


def strip_prefix(path):
    path = unquote(path)
    return path[2:] if path[:2] in ("a/", "b/") else path


def parse(text):
    """Return a list of files: path, old_path, status, adds, dels, patch (from the first @@ line)."""
    files, cur = [], None
    for line in text.split("\n"):
        if line.startswith("diff --git "):
            cur = {"path": "", "old_path": "", "status": "modified", "adds": 0, "dels": 0, "patch": [], "in_hunks": False}
            files.append(cur)
            m = re.match(r'diff --git (?:"?a/)(.+?)"? (?:"?b/)(.+?)"?$', line)
            if m:
                cur["old_path"], cur["path"] = m.group(1), m.group(2)
            continue
        if cur is None:
            continue
        if cur["in_hunks"]:
            if line.startswith("+"):
                cur["adds"] += 1
            elif line.startswith("-"):
                cur["dels"] += 1
            cur["patch"].append(line)
            continue
        if line.startswith("@@"):
            cur["in_hunks"] = True
            cur["patch"].append(line)
        elif line.startswith("new file mode"):
            cur["status"] = "added"
        elif line.startswith("deleted file mode"):
            cur["status"] = "removed"
        elif line.startswith("rename from "):
            cur["status"], cur["old_path"] = "renamed", unquote(line[len("rename from "):])
        elif line.startswith("rename to "):
            cur["path"] = unquote(line[len("rename to "):])
        elif line.startswith("--- ") and line[4:] != "/dev/null":
            cur["old_path"] = strip_prefix(line[4:])
        elif line.startswith("+++ ") and line[4:] != "/dev/null":
            cur["path"] = strip_prefix(line[4:])
        elif line.startswith("Binary files"):
            cur["status"] += " binary"
    for f in files:
        if f["status"].startswith("removed"):
            f["path"] = f["old_path"]
        while f["patch"] and f["patch"][-1] == "":
            f["patch"].pop()
        f["patch"] = "\n".join(f["patch"])
        del f["in_hunks"]
    keys = {}
    for f in files:
        base = re.sub(r"[^a-zA-Z0-9]", "_", f["path"])
        n = keys.get(base, 0) + 1
        keys[base] = n
        f["key"] = base if n == 1 else f"{base}_{n}"
    return files


def cmd_files(diff):
    for f in parse(Path(diff).read_text(errors="replace")):
        path = f["path"] if f["status"] != "renamed" else f"{f['old_path']} -> {f['path']}"
        print(f"{f['key']}\t{f['status']}\t+{f['adds']}\t-{f['dels']}\t{path}")


def cmd_page(diff, body, out):
    patches = {f["key"]: f["patch"] for f in parse(Path(diff).read_text(errors="replace"))}
    # A patch can hold "</script>": escape <, > and & so the JSON cannot end its script tag.
    safe = json.dumps(patches).replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")
    page = (
        (HERE / "template.html").read_text()
        .replace("/* INJECT_CSS */", (HERE / "styles.css").read_text())
        .replace("/* INJECT_JS */", (HERE / "renderer.js").read_text())
        .replace('{"__PR_DIFFS_PLACEHOLDER__":true}', safe)
        .replace("<!-- INJECT_BODY -->", Path(body).read_text())
    )
    Path(out).parent.mkdir(parents=True, exist_ok=True)
    Path(out).write_text(page)
    used = set(re.findall(r'data-diff="([^"]+)"', Path(body).read_text()))
    missing = sorted(used - patches.keys())
    print(f"wrote {out}: {len(patches)} patches, {len(used)} diff placeholders")
    if missing:
        print("no patch for data-diff keys: " + ", ".join(missing), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    args = sys.argv[1:]
    if len(args) == 2 and args[0] == "files":
        cmd_files(args[1])
    elif len(args) == 4 and args[0] == "page":
        cmd_page(*args[1:])
    else:
        sys.exit(__doc__)
