#!/usr/bin/env bash
# The mechanical half of the try-pr skill (see SKILL.md): a PR's or a
# branch's code in a worktree of its own, under <clone>/.worktrees/.
#
#   try-pr.sh fetch <PR link | PR number | branch>   make or update the worktree; print what it holds
#   try-pr.sh list                                   each try-pr worktree, and its stack note
#   try-pr.sh done <PR link | PR number | branch>    remove the worktree; refused while its stack note is there
#
# Run it in the person's clone or in a worktree of it. A number is a PR of
# origin. It needs bash 3.2 or later and git, gh for GitHub, curl and jq for
# Bitbucket Cloud. The stack note is a file that the agent writes when it
# starts a stack from the worktree, and deletes when it stops that stack.
set -euo pipefail

BITBUCKET_API=${TRY_PR_BITBUCKET_API:-https://api.bitbucket.org/2.0}

die() { echo "try-pr: $*" >&2; exit 1; }
lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

# host/owner/name of a remote URL, in the scp, ssh:// or https:// form.
repo_of_url() {
  local u=${1%/} host rest
  u=${u%.git}
  u=${u#*://}
  u=${u#*@}
  if [[ $u == *:* ]]; then
    host=${u%%:*} rest=${u#*:}
    [[ $rest =~ ^[0-9]+/(.*)$ ]] && rest=${BASH_REMATCH[1]}
    u=$host/$rest
  fi
  lower "$u"
}

# The person's clone: the main worktree, also when this runs in a linked one.
clone_root() {
  local common
  common=$(cd "$(git rev-parse --git-common-dir)" && pwd -P)
  if [[ $(basename "$common") == .git ]]; then dirname "$common"; else git rev-parse --show-toplevel; fi
}

# Sets KIND (pr or branch), FORGE, REPO (host/owner/name), NUM, BRANCH and NAME.
parse() {
  local a=$1 github='^https?://(www\.)?github\.com/([^/]+)/([^/]+)/pull/([0-9]+)([/?#].*)?$'
  local bitbucket='^https?://bitbucket\.org/([^/]+)/([^/]+)/pull-requests/([0-9]+)([/?#].*)?$'
  KIND=pr FORGE="" REPO="" NUM="" BRANCH=""
  if [[ $a =~ $github ]]; then
    FORGE=github REPO=$(lower "github.com/${BASH_REMATCH[2]}/${BASH_REMATCH[3]}") NUM=${BASH_REMATCH[4]}
  elif [[ $a =~ $bitbucket ]]; then
    FORGE=bitbucket REPO=$(lower "bitbucket.org/${BASH_REMATCH[1]}/${BASH_REMATCH[2]}") NUM=${BASH_REMATCH[3]}
  elif [[ $a =~ ^#?([0-9]+)$ ]]; then
    NUM=${BASH_REMATCH[1]}
    local origin
    origin=$(git config --get remote.origin.url) || die "this clone has no origin"
    REPO=$(repo_of_url "$origin")
    case ${REPO%%/*} in
      github.com) FORGE=github ;;
      bitbucket.org) FORGE=bitbucket ;;
      *) die "origin is on ${REPO%%/*}: try-pr knows GitHub and Bitbucket Cloud" ;;
    esac
  elif [[ $a == *://* ]]; then
    die "not a GitHub or Bitbucket Cloud PR link: $a"
  elif git check-ref-format --branch "$a" >/dev/null 2>&1; then
    KIND=branch BRANCH=$a
  else
    die "not a PR link, a PR number or a branch name: $a"
  fi
  if [[ $KIND == pr ]]; then NAME=try-pr-$NUM; else NAME=try-pr-$(printf '%s' "$BRANCH" | tr -c 'A-Za-z0-9._-' '-'); fi
}

# The remote whose URL is the repository $1 (host/owner/name).
remote_of() {
  local r
  for r in $(git remote); do
    if [[ $(repo_of_url "$(git config --get "remote.$r.url")") == "$1" ]]; then echo "$r"; return; fi
  done
  return 1
}

# Sets TITLE, AUTHOR, BRANCH, TARGET, URL and REF (the ref to fetch) from GitHub.
read_github() {
  command -v gh >/dev/null || die "GitHub PRs need gh (https://cli.github.com)"
  local state info
  info=$(gh pr view "$NUM" -R "$REPO" --json state,title,author,headRefName,baseRefName,url \
    --jq '.state, .headRefName, .baseRefName, .url, .author.login, .title') || die "gh could not read PR $NUM of $REPO"
  { IFS= read -r state; IFS= read -r BRANCH; IFS= read -r TARGET; IFS= read -r URL; IFS= read -r AUTHOR
    IFS= read -r TITLE; } <<<"$info"
  [[ $state == OPEN ]] || die "PR $NUM is $state: try-pr runs open PRs only"
  REF=refs/pull/$NUM/head
}

# GET of a Bitbucket API path, with the person's git credential for
# bitbucket.org when there is one: Basic (an app password) first, then
# Bearer (an access token). The credential goes to curl on stdin only.
bitbucket_get() {
  local url=$BITBUCKET_API/$1 user="" pass="" line
  while IFS= read -r line; do
    case $line in
      username=*) user=${line#username=} ;;
      password=*) pass=${line#password=} ;;
    esac
  done < <(printf 'protocol=https\nhost=bitbucket.org\npath=%s.git\n\n' "${REPO#*/}" |
    GIT_TERMINAL_PROMPT=0 git credential fill 2>/dev/null || true)
  if [[ -z $pass ]]; then curl -fsS "$url"; return; fi
  if [[ $user != x-token-auth ]]; then
    printf 'Authorization: Basic %s\n' "$(printf '%s:%s' "$user" "$pass" | base64 | tr -d '\n')" |
      curl -fsS -H @- "$url" 2>/dev/null && return
  fi
  printf 'Authorization: Bearer %s\n' "$pass" | curl -fsS -H @- "$url"
}

# As read_github, from the Bitbucket Cloud API. Only a PR whose source
# branch is in the same repository: a fork's PR is refused.
read_bitbucket() {
  command -v jq >/dev/null || die "Bitbucket PRs need jq"
  local answer fields state source target_repo
  answer=$(bitbucket_get "repositories/${REPO#*/}/pullrequests/$NUM") ||
    die "Bitbucket did not give PR $NUM of ${REPO#*/}; for a private repository, keep a credential for bitbucket.org in git"
  fields=$(jq -er '.state, .source.repository.full_name, .destination.repository.full_name, .source.branch.name,
    .destination.branch.name, .links.html.href, .author.display_name, .title | tostring | gsub("[\r\n\t]"; " ")' \
    <<<"$answer") || die "Bitbucket's answer for PR $NUM is not a PR"
  { IFS= read -r state; IFS= read -r source; IFS= read -r target_repo; IFS= read -r BRANCH; IFS= read -r TARGET
    IFS= read -r URL; IFS= read -r AUTHOR; IFS= read -r TITLE; } <<<"$fields"
  [[ $state == OPEN ]] || die "PR $NUM is $state: try-pr runs open PRs only"
  [[ $(lower "$source") == "$(lower "$target_repo")" ]] ||
    die "PR $NUM comes from the fork $source: try-pr runs Bitbucket PRs from $target_repo itself only"
  REF=refs/heads/$BRANCH
}

# The stack note of worktree $1.
note_of() { echo "$(git -C "$1" rev-parse --git-dir)/try-pr-stack"; }

# The path of try-pr worktree $1 when it is one, else nothing.
worktree_at() {
  [[ -d $1 && $(cd "$1" && git rev-parse --show-toplevel 2>/dev/null) == "$1" ]] && echo "$1"
  return 0
}

cmd_fetch() {
  parse "$1"
  local remote clone wt head subject behind
  if [[ $KIND == pr ]]; then
    remote=$(remote_of "$REPO") || die "no remote of this clone is $REPO: run try-pr in a clone of it"
    if [[ $FORGE == github ]]; then read_github; else read_bitbucket; fi
  else
    remote=origin REF=refs/heads/$BRANCH
    TARGET=$(git ls-remote --symref origin HEAD | awk '$1 == "ref:" { sub("refs/heads/", "", $2); print $2 }')
    [[ -n $TARGET ]] || die "origin names no default branch"
  fi
  git fetch -q "$remote" "$REF" || die "could not fetch $REF from $remote"
  head=$(git rev-parse FETCH_HEAD)
  git fetch -q "$remote" "+refs/heads/$TARGET:refs/remotes/$remote/$TARGET" || die "could not fetch $TARGET from $remote"
  behind=$(git rev-list --count "$head..refs/remotes/$remote/$TARGET")
  subject=$(git log -1 --format=%s "$head")

  clone=$(clone_root)
  if ! git -C "$clone" check-ignore -q .worktrees/try-pr; then
    local exclude
    exclude=$(cd "$(git rev-parse --git-common-dir)" && pwd -P)/info/exclude
    mkdir -p "$(dirname "$exclude")"
    echo /.worktrees/ >>"$exclude"
    echo "ignored  /.worktrees/ is in $exclude now"
  fi
  wt=$clone/.worktrees/$NAME
  if [[ -n $(worktree_at "$wt") ]]; then
    [[ ! -f $(note_of "$wt") ]] || die "a stack runs from $wt ($(note_of "$wt")): stop it first"
    [[ -z $(git -C "$wt" status --porcelain) ]] || die "$wt has changes: git -C $wt status"
    git -C "$wt" checkout -q --detach "$head"
  elif [[ -e $wt ]]; then
    die "$wt is there and is not a worktree of this clone"
  else
    git worktree add -q --detach "$wt" "$head"
  fi

  echo "worktree $wt"
  if [[ $KIND == pr ]]; then
    echo "pr       $NUM $URL"
    echo "title    $TITLE"
    echo "author   $AUTHOR"
  fi
  [[ -z $BRANCH ]] || echo "branch   $BRANCH"
  echo "head     $head $subject"
  echo "behind   $behind commits behind $remote/$TARGET"
  echo "note     $(note_of "$wt")"
}

cmd_list() {
  local prefix path note found=0
  prefix=$(clone_root)/.worktrees/try-pr-
  while IFS= read -r path; do
    [[ $path == "$prefix"* ]] || continue
    found=1
    [[ -d $path ]] || { echo "worktree $path is gone: git worktree prune"; continue; }
    echo "worktree $path $(git -C "$path" log -1 --format='%h %s')"
    note=$(note_of "$path")
    if [[ -f $note ]]; then sed 's/^/  stack  /' "$note"; else echo "  stack  none started by try-pr"; fi
  done < <(git worktree list --porcelain | sed -n 's/^worktree //p')
  ((found)) || echo "no try-pr worktrees"
}

cmd_done() {
  parse "$1"
  local wt
  wt=$(clone_root)/.worktrees/$NAME
  [[ -n $(worktree_at "$wt") ]] || die "no try-pr worktree at $wt"
  [[ ! -f $(note_of "$wt") ]] ||
    die "a stack runs from $wt: stop it as $(note_of "$wt") says, delete that file, then run done again"
  git worktree remove "$wt" || die "$wt was not removed; see git's reason above"
  rmdir "$(dirname "$wt")" 2>/dev/null || true
  echo "removed  $wt"
}

case ${1:-} in
  fetch) [[ $# -eq 2 ]] || die "fetch takes one PR link, PR number or branch"; cmd_fetch "$2" ;;
  list) cmd_list ;;
  done) [[ $# -eq 2 ]] || die "done takes one PR link, PR number or branch"; cmd_done "$2" ;;
  *) sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
