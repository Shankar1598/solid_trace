#!/usr/bin/env bash
# Tests try-pr.sh on local repositories and on a saved Bitbucket Cloud
# answer, scrubbed to placeholder names (bitbucket-pr.json). It needs no
# network and no forge account. GitHub is proven on a live PR instead.
#
#   bash cli/plugin-skills/skills/try-pr/test/try-pr_test.sh
#
# Each check's condition is a string that check evals, so the names in it
# expand there, and the variables it reads look unused.
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

# The remote: main, and feature/cart-total from main's first commit. main
# then gets 3 more commits, so the branch is 3 behind.
git init -q --bare "$tmp/remote.git"
git init -q "$tmp/seed"
commit() { git -C "$tmp/seed" commit -q --allow-empty -m "$1"; }
commit "first"
git -C "$tmp/seed" switch -q -c feature/cart-total
commit "Add the cart total"
commit "Show the total under the cart lines"
git -C "$tmp/seed" switch -q main
commit "main 2" && commit "main 3" && commit "main 4"
git -C "$tmp/seed" push -q "$tmp/remote.git" main feature/cart-total
feature_head=$(git -C "$tmp/seed" rev-parse feature/cart-total)

# The clone's origin is Bitbucket by name; git fetches the local remote.
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
check() { # name, then a condition for eval
  if eval "$2"; then echo "ok    $1"; else echo "FAIL  $1"; printf '      | %s\n' "$out"; failed=1; fi
}
try() { # dir, then try-pr.sh's arguments; sets out and code
  code=0
  out=$(cd "$1" && "$S" "${@:2}" 2>&1) || code=$?
}
has() { [[ $out == *"$1"* ]]; }
wt=$clone/.worktrees/try-pr-7

try "$clone" fetch https://bitbucket.org/example-team/example-app/pull-requests/7/overview
check "a link makes the worktree at the PR's head" '((code == 0)) && [[ $(git -C "$wt" rev-parse HEAD) == "$feature_head" ]]'
check "it prints the PR's title, author, branch and target" \
  'has "title    Show the order total on the cart page" && has "author   Alex Example" && has "branch   feature/cart-total" && has "pr       7 https://bitbucket.org/example-team/example-app/pull-requests/7"'
check "it says how far behind the target the head is" 'has "behind   3 commits behind origin/main"'
check "it ignores .worktrees/ in .git/info/exclude" 'has "ignored  /.worktrees/" && [[ -z $(git -C "$clone" status --porcelain) ]]'

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
try "$clone" fetch https://gitlab.com/example-team/example-app/-/merge_requests/3
check "another forge's link is refused" '((code == 1)) && has "not a GitHub or Bitbucket Cloud PR link"'

# A curl that records its arguments and its stdin, then serves the file.
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

note=$(git -C "$wt" rev-parse --git-dir)/try-pr-stack
printf 'stop: true\nurls: http://localhost:1\n' >"$note"
try "$clone" list
check "list shows the worktree and its stack note" 'has "worktree $wt" && has "  stack  stop: true"'
try "$clone" fetch 7
check "fetch leaves a worktree alone while its stack note is there" '((code == 1)) && has "a stack runs from $wt"'
try "$clone" "done" 7
check "done is refused while the stack note is there" '((code == 1)) && [[ -d $wt ]]'
rm "$note"

touch "$wt/scratch.txt"
try "$clone" fetch 7
check "fetch leaves a worktree with changes alone" '((code == 1)) && has "has changes"'
try "$clone" "done" 7
check "done leaves a worktree with changes alone" '((code == 1)) && [[ -f $wt/scratch.txt ]]'
rm "$wt/scratch.txt"
try "$clone" "done" https://bitbucket.org/example-team/example-app/pull-requests/7
check "done removes the worktree" '((code == 0)) && [[ ! -e $wt ]] && [[ $(git -C "$clone" worktree list) != *try-pr-7* ]]'

try "$clone" fetch feature/cart-total
check "a branch gets a worktree of its own, behind origin's default branch" \
  '((code == 0)) && has "worktree $clone/.worktrees/try-pr-feature-cart-total" && has "behind   3 commits behind origin/main"'
try "$clone" "done" feature/cart-total
try "$clone" list
check "with none left, list says so, and .worktrees/ is gone" 'has "no try-pr worktrees" && [[ ! -e $clone/.worktrees ]]'

((failed == 0)) && echo "all passed"
exit "$failed"
