#!/usr/bin/env bash
# The mechanical half of the try-pr skill (see SKILL.md): a PR's or a
# branch's code in a worktree of its own, under <clone>/.worktrees/.
#
#   try-pr.sh fetch <PR>          make or update the worktree; print what it holds
#   try-pr.sh copy <PR> <folder>  copy a folder of this checkout, such as a launch skill, into the worktree
#   try-pr.sh started <PR>        keep the stack note from stdin: the stack's URLs, status and stop
#   try-pr.sh stopped <PR>        delete the stack note, after the stack stopped
#   try-pr.sh list                each try-pr worktree, with its copies and its stack note
#   try-pr.sh done <PR>           remove the copies and the worktree
#
# <PR> is a GitHub or Bitbucket Cloud PR link, a PR number of origin, or a
# branch (branch:<name> for a name of digits): this clone's when it has
# commits that origin's lacks, else origin's. Run it in the person's clone or
# in a worktree of it. It needs bash 3.2 or later and git, gh for GitHub, curl
# and jq for Bitbucket Cloud. fetch and done refuse a worktree while a stack
# note is there, or while it has commits that no branch has. fetch and done
# delete the copies, so a new head starts with only its own files.
set -euo pipefail

BITBUCKET_API=${TRY_PR_BITBUCKET_API:-https://api.bitbucket.org/2.0}

# The PR that parse and the read_ functions fill in: bash 3.2 has no records.
KIND="" FORGE="" REPO="" NUM="" BRANCH="" NAME="" TITLE="" AUTHOR="" TARGET="" URL="" REF="" SHA="" FROM="" WT=""

die() { echo "try-pr: $*" >&2; exit 1; }
lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

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

main_checkout() {
  local common
  common=$(cd "$(git rev-parse --git-common-dir)" && pwd -P)
  if [[ $(basename "$common") == .git ]]; then dirname "$common"; else git rev-parse --show-toplevel; fi
}

parse() {
  local a=$1 github='^https?://(www\.)?github\.com/([^/]+)/([^/]+)/pull/([0-9]+)([/?#].*)?$'
  local bitbucket='^https?://bitbucket\.org/([^/]+)/([^/]+)/pull-requests/([0-9]+)([/?#].*)?$'
  KIND="pr"
  if [[ $a == branch:* ]]; then
    git check-ref-format --branch "${a#branch:}" >/dev/null 2>&1 || die "not a branch name: ${a#branch:}"
    KIND=branch BRANCH=${a#branch:}
  elif [[ $a =~ $github ]]; then
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
  if [[ $KIND == pr ]]; then
    NAME=try-pr-$NUM
  else
    NAME=try-pr-$(printf '%s' "$BRANCH" | tr -c 'A-Za-z0-9._-' '-')-$(printf '%s' "$BRANCH" | git hash-object --stdin | cut -c 1-7)
  fi
}

remote_of() {
  local r
  for r in $(git remote); do
    if [[ $(repo_of_url "$(git config --get "remote.$r.url")") == "$1" ]]; then echo "$r"; return; fi
  done
  return 1
}

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

read_branch() {
  local found=0 origin_head="" local_head ahead
  TARGET=$(git ls-remote --symref origin HEAD | awk '$1 == "ref:" { sub("refs/heads/", "", $2); print $2 }')
  [[ -n $TARGET ]] || die "origin names no default branch"
  git ls-remote --exit-code origin "refs/heads/$BRANCH" >/dev/null || found=$?
  case $found in
    0) git fetch -q origin "refs/heads/$BRANCH" || die "could not fetch $BRANCH from origin"
      origin_head=$(git rev-parse FETCH_HEAD) ;;
    2) ;;
    *) die "could not read the branches of origin" ;;
  esac
  local_head=$(git rev-parse -q --verify "refs/heads/$BRANCH^{commit}") || local_head=""
  if [[ -z $local_head ]]; then
    [[ -n $origin_head ]] || die "neither origin nor this clone has the branch $BRANCH"
    SHA=$origin_head
  elif [[ -z $origin_head ]]; then
    SHA=$local_head FROM="this clone's only: origin does not have it"
  else
    ahead=$(git rev-list --count "$origin_head..$local_head")
    if ((ahead)); then SHA=$local_head FROM="this clone's: $ahead commits ahead of origin's"; else SHA=$origin_head; fi
  fi
}

note_of() { echo "$(git -C "$1" rev-parse --git-dir)/try-pr-stack"; }
copies_of() { echo "$(git -C "$1" rev-parse --git-dir)/try-pr-copies"; }

is_worktree() { [[ -d $1 && $(cd "$1" && git rev-parse --show-toplevel 2>/dev/null) == "$1" ]]; }

existing_worktree() {
  parse "$1"
  WT=$(main_checkout)/.worktrees/$NAME
  is_worktree "$WT" || die "no try-pr worktree at $WT: run fetch first"
}

running() { die "a stack runs from $1: stop it with the stop line of try-pr.sh list, then run try-pr.sh stopped"; }

keep_commits() {
  local n
  n=$(git -C "$1" rev-list --count HEAD --not --branches --remotes refs/worktree/try-pr-head)
  ((n == 0)) ||
    die "$1 has $n commits that no branch has, and try-pr would lose them; keep them first: git branch <name> $(git -C "$1" rev-parse HEAD)"
}

remove_copies() {
  local list folder
  list=$(copies_of "$1")
  [[ -f $list ]] || return 0
  while IFS= read -r folder; do rm -rf "${1:?}/${folder:?}"; done <"$list"
  rm "$list"
}

cmd_fetch() {
  parse "$1"
  local remote=origin clone wt subject behind
  if [[ $KIND == branch ]]; then
    read_branch
  else
    remote=$(remote_of "$REPO") || die "no remote of this clone is $REPO: run try-pr in a clone of it"
    if [[ $FORGE == github ]]; then read_github; else read_bitbucket; fi
    git fetch -q "$remote" "$REF" || die "could not fetch $REF from $remote"
    SHA=$(git rev-parse FETCH_HEAD)
  fi
  git fetch -q "$remote" "+refs/heads/$TARGET:refs/remotes/$remote/$TARGET" || die "could not fetch $TARGET from $remote"
  behind=$(git rev-list --count "$SHA..refs/remotes/$remote/$TARGET")
  subject=$(git log -1 --format=%s "$SHA")

  clone=$(main_checkout)
  if ! git -C "$clone" check-ignore -q .worktrees/try-pr; then
    local exclude
    exclude=$(cd "$(git rev-parse --git-common-dir)" && pwd -P)/info/exclude
    mkdir -p "$(dirname "$exclude")"
    [[ ! -s $exclude || -z $(tail -c 1 "$exclude") ]] || echo >>"$exclude"
    echo /.worktrees/ >>"$exclude"
    echo "ignored  /.worktrees/ is in $exclude now"
  fi
  wt=$clone/.worktrees/$NAME
  if is_worktree "$wt"; then
    [[ ! -f $(note_of "$wt") ]] || running "$wt"
    keep_commits "$wt"
    remove_copies "$wt"
    [[ -z $(git -C "$wt" status --porcelain) ]] || die "$wt has changes: git -C $wt status"
    git -C "$wt" checkout -q --detach "$SHA"
  elif [[ -e $wt ]]; then
    die "$wt is there and is not a worktree of this clone"
  else
    git worktree add -q --detach "$wt" "$SHA"
  fi
  git -C "$wt" update-ref refs/worktree/try-pr-head "$SHA"

  echo "worktree $wt"
  if [[ $KIND == pr ]]; then
    echo "pr       $NUM $URL"
    echo "title    $TITLE"
    echo "author   $AUTHOR"
  fi
  [[ -z $BRANCH ]] || echo "branch   $BRANCH${FROM:+ ($FROM)}"
  echo "head     $SHA $subject"
  echo "behind   $behind commits behind $remote/$TARGET"
}

cmd_copy() {
  local folder=${2%/} from
  [[ -n $folder && $folder != /* && /$folder/ != */../* ]] ||
    die "copy takes a folder by its path in this checkout, such as .claude/skills/<name>"
  existing_worktree "$1"
  from=$(git rev-parse --show-toplevel)/$folder
  [[ -d $from ]] || die "this checkout has no folder $folder"
  [[ ! -e $WT/$folder ]] || die "the worktree has $folder already"
  mkdir -p "$(dirname "$WT/$folder")"
  cp -R "$from" "$WT/$folder"
  echo "$folder" >>"$(copies_of "$WT")"
  echo "copied   $from to $WT/$folder"
}

cmd_started() {
  local note
  existing_worktree "$1"
  note=$(cat)
  [[ -n $note ]] || die "started takes the stack note on stdin"
  printf '%s\n' "$note" >"$(note_of "$WT")"
  echo "started  the stack of $WT is in its note now"
}

cmd_stopped() {
  existing_worktree "$1"
  rm -f "$(note_of "$WT")"
  echo "stopped  the stack note of $WT is gone"
}

cmd_list() {
  local prefix path note found=0
  prefix=$(main_checkout)/.worktrees/try-pr-
  while IFS= read -r path; do
    [[ $path == "$prefix"* ]] || continue
    found=1
    [[ -d $path ]] || { echo "worktree $path is gone: git worktree prune"; continue; }
    echo "worktree $path $(git -C "$path" log -1 --format='%h %s')"
    [[ ! -f $(copies_of "$path") ]] || sed 's/^/  copied /' "$(copies_of "$path")"
    note=$(note_of "$path")
    if [[ -f $note ]]; then sed 's/^/  stack  /' "$note"; else echo "  stack  none started by try-pr"; fi
  done < <(git worktree list --porcelain | sed -n 's/^worktree //p')
  ((found)) || echo "no try-pr worktrees"
}

cmd_done() {
  existing_worktree "$1"
  [[ ! -f $(note_of "$WT") ]] || running "$WT"
  keep_commits "$WT"
  remove_copies "$WT"
  git worktree remove "$WT" || die "$WT was not removed; see git's reason above"
  rmdir "$(dirname "$WT")" 2>/dev/null || true
  echo "removed  $WT"
}

case ${1:-} in
  fetch) [[ $# -eq 2 ]] || die "fetch takes one PR link, PR number or branch"; cmd_fetch "$2" ;;
  copy) [[ $# -eq 3 ]] || die "copy takes a PR link, PR number or branch, then a folder"; cmd_copy "$2" "$3" ;;
  started | stopped) [[ $# -eq 2 ]] || die "$1 takes one PR link, PR number or branch"; "cmd_$1" "$2" ;;
  list) cmd_list ;;
  done) [[ $# -eq 2 ]] || die "done takes one PR link, PR number or branch"; cmd_done "$2" ;;
  *) awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "${BASH_SOURCE[0]}"; exit 2 ;;
esac
