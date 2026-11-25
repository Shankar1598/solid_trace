#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2034,SC2317
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
S=$here/../try-pr.sh
tmp=$(cd "$(mktemp -d)" && pwd -P)
trap 'rm -rf "$tmp"' EXIT
export HOME=$tmp/home GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=$tmp/gitconfig GIT_TERMINAL_PROMPT=0
export TRY_PR_BITBUCKET_API=file://$tmp/api
mkdir -p "$HOME"
git config --global user.name Test
git config --global user.email test@example.com
git config --global init.defaultBranch main

git init -q --bare "$tmp/remote.git"
git init -q "$tmp/seed"
commit() { git -C "$tmp/seed" commit -q --allow-empty -m "$1"; }
commit "first"
git -C "$tmp/seed" switch -q -c feature/cart-total
commit "Add the cart total"
commit "Show the total under the cart lines"
git -C "$tmp/seed" switch -q main
mkdir -p "$tmp/seed/.claude/skills/launch"
echo "Starts the app." >"$tmp/seed/.claude/skills/launch/SKILL.md"
git -C "$tmp/seed" add .claude
commit "Add the launch skill" && commit "main 3" && commit "main 4"
git -C "$tmp/seed" push -q "$tmp/remote.git" main feature/cart-total
feature_head=$(git -C "$tmp/seed" rev-parse feature/cart-total)

git config --global url."$tmp/remote.git".insteadOf git@bitbucket.org:example-team/example-app.git
git clone -q git@bitbucket.org:example-team/example-app.git "$tmp/clone"
clone=$tmp/clone
git -C "$clone" worktree add -q --detach "$tmp/linked" main

pulls=$tmp/api/repositories/example-team/example-app/pullrequests
mkdir -p "$pulls"
cp "$here/bitbucket-pr.json" "$pulls/7"
jq '.id = 8 | .state = "MERGED"' "$here/bitbucket-pr.json" >"$pulls/8"
jq '.id = 9 | .source.repository.full_name = "alex-example/example-app"' "$here/bitbucket-pr.json" >"$pulls/9"

failed=0
check() {
  if eval "$2"; then echo "ok    $1"; else echo "FAIL  $1"; printf '      | %s\n' "$out"; failed=1; fi
}
try() {
  code=0
  out=$(cd "$1" && "$S" "${@:2}" 2>&1) || code=$?
}
has() { [[ $out == *"$1"* ]]; }
wt=$clone/.worktrees/try-pr-7
worktree_of_out() { sed -n 's/^worktree //p' <<<"$out"; }

printf '*.log' >"$clone/.git/info/exclude"
try "$clone" fetch https://bitbucket.org/example-team/example-app/pull-requests/7/overview
check "a link makes the worktree at the PR's head" '((code == 0)) && [[ $(git -C "$wt" rev-parse HEAD) == "$feature_head" ]]'
check "it prints the PR's title, author, branch and target" \
  'has "title    Show the order total on the cart page" && has "author   Alex Example" && has "branch   feature/cart-total" && has "pr       7 https://bitbucket.org/example-team/example-app/pull-requests/7"'
check "it says how far behind the target the head is" 'has "behind   3 commits behind origin/main"'
check "it ignores .worktrees/ in .git/info/exclude" 'has "ignored  /.worktrees/" && [[ -z $(git -C "$clone" status --porcelain) ]]'
check "it keeps the last line of an exclude file with no final newline" \
  'grep -qxF "*.log" "$clone/.git/info/exclude" && grep -qxF /.worktrees/ "$clone/.git/info/exclude"'

git -C "$tmp/seed" switch -q feature/cart-total
commit "Round the total"
git -C "$tmp/seed" push -q "$tmp/remote.git" feature/cart-total
try "$tmp/linked" fetch 7
check "a number, from a linked worktree, moves the same worktree to the new head" \
  '((code == 0)) && [[ $(git -C "$wt" rev-parse HEAD) == $(git -C "$tmp/seed" rev-parse HEAD) ]] && has "worktree $wt"'
check "it adds the ignore line once" '! has "ignored" && [[ $(grep -c "^/.worktrees/$" "$clone/.git/info/exclude") == 1 ]]'

try "$clone" fetch 8
check "a merged PR is refused with its state" '((code == 1)) && has "PR 8 is MERGED: try-pr runs open PRs only" && [[ ! -e $clone/.worktrees/try-pr-8 ]]'
try "$clone" fetch 9
check "a PR from a fork is refused" '((code == 1)) && has "PR 9 comes from the fork alex-example/example-app" && [[ ! -e $clone/.worktrees/try-pr-9 ]]'
try "$clone" fetch https://bitbucket.org/other-team/other-app/pull-requests/3
check "a PR of a repository with no remote here is refused" '((code == 1)) && has "no remote of this clone is bitbucket.org/other-team/other-app"'
try "$clone" fetch https://github.com/example-org/example-app/pull/3
check "a GitHub link is matched against the remotes too" '((code == 1)) && has "no remote of this clone is github.com/example-org/example-app"'

git -C "$tmp/seed" switch -q --detach main
commit "Fix the tax line"
git -C "$tmp/seed" push -q "$tmp/remote.git" HEAD:refs/pull/3/head
pr3_head=$(git -C "$tmp/seed" rev-parse HEAD)
git -C "$tmp/seed" switch -q main
git config --global --add url."$tmp/remote.git".insteadOf git@github.com:example-org/example-app.git
git -C "$clone" remote add upstream git@github.com:example-org/example-app.git
mkdir -p "$tmp/gh/pulls"
cat >"$tmp/gh/gh" <<'EOF2'
#!/usr/bin/env bash
[[ "$1 $2 $4 $5" == "pr view -R github.com/example-org/example-app" ]] || { echo "fake gh: $*" >&2; exit 1; }
num=$3
while (($#)); do [[ $1 != --jq ]] || expr=$2; shift; done
jq -r "$expr" "${0%/*}/pulls/$num.json"
EOF2
chmod +x "$tmp/gh/gh"
echo '{"state": "OPEN", "title": "Fix the tax line", "author": {"login": "sam-example"}, "headRefName": "fix/tax-line",
  "baseRefName": "main", "url": "https://github.com/example-org/example-app/pull/3"}' >"$tmp/gh/pulls/3.json"
jq '.state = "CLOSED"' "$tmp/gh/pulls/3.json" >"$tmp/gh/pulls/4.json"
PATH=$tmp/gh:$PATH try "$clone" fetch https://github.com/example-org/example-app/pull/3
check "a GitHub PR's head comes from refs/pull, through the remote of its repository" \
  '((code == 0)) && [[ $(git -C "$clone/.worktrees/try-pr-3" rev-parse HEAD) == "$pr3_head" ]]'
check "it prints gh's title, author, branch and target in their places" \
  'has "title    Fix the tax line" && has "author   sam-example" && has "branch   fix/tax-line" && has "pr       3 https://github.com/example-org/example-app/pull/3" && has "behind   0 commits behind upstream/main"'
PATH=$tmp/gh:$PATH try "$clone" "done" https://github.com/example-org/example-app/pull/3
check "done removes a GitHub PR's worktree, though no branch has the PR's head" '((code == 0)) && [[ ! -e $clone/.worktrees/try-pr-3 ]]'
PATH=$tmp/gh:$PATH try "$clone" fetch https://github.com/example-org/example-app/pull/4
check "a closed GitHub PR is refused with its state" '((code == 1)) && has "PR 4 is CLOSED: try-pr runs open PRs only" && [[ ! -e $clone/.worktrees/try-pr-4 ]]'
try "$clone" fetch https://gitlab.com/example-team/example-app/-/merge_requests/3
check "another forge's link is refused" '((code == 1)) && has "not a GitHub or Bitbucket Cloud PR link"'

mkdir -p "$tmp/bin"
cat >"$tmp/bin/curl" <<'EOF2'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CURL_LOG.args"
[[ " $* " != *" @- "* ]] || cat >>"$CURL_LOG.stdin"
for url; do :; done
cat "${url#file://}"
EOF2
chmod +x "$tmp/bin/curl"
secret=s3cret-test-value
for user in alex-example x-token-auth; do
  git config --global credential.helper "!f() { echo username=$user; echo password=$secret; }; f"
  export CURL_LOG=$tmp/curl-$user
  PATH=$tmp/bin:$PATH try "$clone" fetch 7
  check "the credential of $user goes to curl on stdin only, and is never printed" \
    '((code == 0)) && ! has "$secret" && ! grep -q "$secret" "$CURL_LOG.args"'
done
check "a user and password go as Basic" \
  '[[ $(cat "$tmp/curl-alex-example.stdin") == "Authorization: Basic $(printf "alex-example:%s" "$secret" | base64)" ]]'
check "an access token (x-token-auth) goes as Bearer" \
  '[[ $(cat "$tmp/curl-x-token-auth.stdin") == "Authorization: Bearer $secret" ]]'
git config --global --unset credential.helper

try "$clone" started 7 </dev/null
check "started takes a note only" '((code == 1)) && has "started takes the stack note on stdin"'
printf 'stop: true\nurls: http://localhost:1\n' >"$tmp/note"
try "$clone" started 7 <"$tmp/note"
try "$clone" list
check "list shows the worktree and its stack note" 'has "worktree $wt" && has "  stack  stop: true" && has "  stack  urls: http://localhost:1"'
try "$clone" fetch 7
check "fetch leaves a worktree alone while its stack note is there" '((code == 1)) && has "a stack runs from $wt"'
try "$clone" "done" 7
check "done is refused while the stack note is there" '((code == 1)) && has "a stack runs from $wt" && [[ -d $wt ]]'
try "$clone" stopped 7
try "$clone" list
check "stopped deletes the note" '((code == 0)) && has "  stack  none started by try-pr"'

touch "$wt/scratch.txt"
try "$clone" fetch 7
check "fetch leaves a worktree with changes alone" '((code == 1)) && has "has changes"'
try "$clone" "done" 7
check "done leaves a worktree with changes alone" '((code == 1)) && has "try-pr: $wt was not removed" && [[ -f $wt/scratch.txt ]]'
rm "$wt/scratch.txt"

git -C "$wt" commit -q --allow-empty -m "Try a smaller total"
kept=$(git -C "$wt" rev-parse HEAD)
try "$clone" fetch 7
check "fetch leaves a worktree with commits that no branch has alone, and says how to keep them" \
  '((code == 1)) && has "1 commits that no branch has" && has "git branch <name> $kept" && [[ $(git -C "$wt" rev-parse HEAD) == "$kept" ]]'
try "$clone" "done" 7
check "done leaves a worktree with commits that no branch has alone" \
  '((code == 1)) && has "git branch <name> $kept" && [[ $(git -C "$wt" rev-parse HEAD) == "$kept" ]]'
git -C "$clone" branch -q kept "$kept"
try "$clone" fetch 7
check "once a branch has the commits, fetch moves the worktree to the head again" \
  '((code == 0)) && [[ $(git -C "$wt" rev-parse HEAD) == $(git -C "$tmp/seed" rev-parse feature/cart-total) ]]'
git -C "$clone" branch -q -D kept

try "$clone" copy 7 .claude/skills/launch/
check "copy puts a folder of this checkout into the worktree" '((code == 0)) && [[ -f $wt/.claude/skills/launch/SKILL.md ]]'
try "$clone" list
check "list shows the copy" 'has "  copied .claude/skills/launch"'
try "$clone" copy 7 .claude/skills/launch
check "copy leaves a folder that the worktree has alone" '((code == 1)) && has "the worktree has .claude/skills/launch already"'
try "$clone" copy 7 .claude/../../outside
check "copy takes only a folder inside this checkout" '((code == 1)) && has "copy takes a folder by its path"'
try "$clone" fetch 7
check "fetch removes the copy, so the head has only its own files" \
  '((code == 0)) && [[ ! -e $wt/.claude/skills/launch && -z $(git -C "$wt" status --porcelain) ]]'
try "$clone" copy 7 .claude/skills/launch
try "$clone" "done" https://bitbucket.org/example-team/example-app/pull-requests/7
check "done removes the copy, then the worktree" '((code == 0)) && [[ ! -e $wt ]] && [[ $(git -C "$clone" worktree list) != *try-pr-7* ]]'

origin_head=$(git -C "$tmp/seed" rev-parse feature/cart-total)
try "$clone" fetch feature/cart-total
slash_wt=$(worktree_of_out)
check "a branch gets a worktree of its own, behind origin's default branch" \
  '((code == 0)) && [[ $slash_wt == "$clone/.worktrees/try-pr-feature-cart-total-"* ]] && has "behind   3 commits behind origin/main"'
git -C "$clone" branch -q feature-cart-total origin/main~1
try "$clone" fetch feature-cart-total
dash_wt=$(worktree_of_out)
check "feature/cart-total and feature-cart-total get a worktree each" \
  '((code == 0)) && [[ $dash_wt != "$slash_wt" && $(git -C "$slash_wt" rev-parse HEAD) == "$origin_head" && $(git -C "$dash_wt" rev-parse HEAD) == $(git -C "$clone" rev-parse feature-cart-total) ]]'
try "$clone" "done" feature-cart-total

unpushed=$(git -C "$clone" commit-tree -p "$origin_head" -m "Unpushed work" "$origin_head^{tree}")
git -C "$clone" branch -q feature/cart-total "$unpushed"
try "$clone" fetch feature/cart-total
check "a branch with commits that origin's lacks runs from this clone, and says so" \
  '((code == 0)) && has "branch   feature/cart-total (this clone" && has "1 commits ahead of origin" && [[ $(git -C "$slash_wt" rev-parse HEAD) == "$unpushed" ]]'
git -C "$clone" branch -q -f feature/cart-total "$origin_head~1"
try "$clone" fetch feature/cart-total
check "a branch with no commits that origin's lacks runs from origin" \
  '((code == 0)) && has "branch   feature/cart-total" && ! has "this clone" && [[ $(git -C "$slash_wt" rev-parse HEAD) == "$origin_head" ]]'
try "$clone" "done" feature/cart-total

git -C "$clone" branch -q local-only origin/main~1
try "$clone" fetch local-only
check "a branch that only this clone has is used as it is" \
  '((code == 0)) && has "branch   local-only (this clone" && [[ $(git -C "$(worktree_of_out)" rev-parse HEAD) == $(git -C "$clone" rev-parse local-only) ]]'
try "$clone" "done" local-only
git -C "$clone" branch -q 1234 origin/main~2
try "$clone" fetch branch:1234
check "branch:<name> takes a name of digits as a branch" \
  '((code == 0)) && has "branch   1234 (this clone" && [[ $(git -C "$(worktree_of_out)" rev-parse HEAD) == $(git -C "$clone" rev-parse 1234) ]]'
try "$clone" "done" branch:1234
try "$clone" fetch no-such-branch
check "a branch that neither has is refused" '((code == 1)) && has "neither origin nor this clone has the branch no-such-branch"'
try "$clone" list
check "with none left, list says so, and .worktrees/ is gone" 'has "no try-pr worktrees" && [[ ! -e $clone/.worktrees ]]'

((failed == 0)) && echo "all passed"
exit "$failed"
