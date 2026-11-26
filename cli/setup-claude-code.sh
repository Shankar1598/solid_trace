#!/bin/sh
# Installs or upgrades SolidTrace for Claude Code on this machine: the
# solidtrace CLI of a release, the SolidTrace marketplace and the solidtrace
# plugin (capture and the review skills). It asks no questions. Run it again
# to upgrade.
#
#   curl -fsSL https://raw.githubusercontent.com/Shankar1598/solid_trace/main/cli/setup-claude-code.sh | sh
#
# Environment:
#   SOLIDTRACE_VERSION      the release, such as v0.24.3 (default: the newest)
#   SOLIDTRACE_INSTALL_DIR  the folder of solidtrace (default: ~/.local/bin)
#   SOLIDTRACE_CONSOLE_URL and SOLIDTRACE_INGEST_URL   sign in when this
#                           machine has no sign-in (Console's Capture setup
#                           page gives both)

set -eu

# All of the script is in main, so that through curl | sh, sh reads all of it
# before a command can read stdin.
main() {
  REPOSITORY="Shankar1598/solid_trace"
  MARKETPLACE="solidtrace"
  BIN_DIR="${SOLIDTRACE_INSTALL_DIR:-$HOME/.local/bin}"
  ST="$BIN_DIR/solidtrace"
  CLAUDE_SETTINGS="$HOME/.claude/settings.json"
  TODO=""

  for tool in curl claude; do
    command -v "$tool" >/dev/null 2>&1 || fail "$tool is necessary, and it is not on PATH"
  done

  VERSION="${SOLIDTRACE_VERSION:-}"
  if [ -z "$VERSION" ]; then
    latest="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPOSITORY/releases/latest")" \
      || fail "could not find the newest release of $REPOSITORY"
    VERSION="${latest##*/}"
  fi
  case "$VERSION" in
    v[0-9]*) ;;
    *) fail "a release is a tag such as v0.24.3, not $VERSION" ;;
  esac
  # A release before the marketplace file was in the repository has no
  # plugin that this script can add. Stop before the script changes a thing.
  curl -fsSIL -o /dev/null "https://raw.githubusercontent.com/$REPOSITORY/$VERSION/.claude-plugin/marketplace.json" \
    || fail "$VERSION has no .claude-plugin/marketplace.json, so this script cannot add its plugin: use a newer release"
  RELEASE_URL="https://github.com/$REPOSITORY/releases/download/$VERSION"
  info "SolidTrace $VERSION (now: $("$ST" version 2>/dev/null || echo "no solidtrace in $BIN_DIR"))"

  info "Installing the solidtrace CLI in $BIN_DIR"
  curl -fsSL "$RELEASE_URL/install.sh" | SOLIDTRACE_VERSION="$VERSION" SOLIDTRACE_INSTALL_DIR="$BIN_DIR" sh \
    || fail "the CLI install failed"
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) todo "Add $BIN_DIR to PATH, so that the solidtrace plugin finds solidtrace." ;;
  esac

  # The marketplace is the file .claude-plugin/marketplace.json of the
  # repository at the release's tag. Claude Code clones only that folder and
  # the plugin's. It does not add a marketplace of the same name at another
  # tag, so a move to a new tag removes the marketplace first. That also
  # removes its plugins (also solidtrace-skills, of v0.24.3 and earlier), and
  # the plugin keeps no data.
  if marketplace_at_tag; then
    info "The SolidTrace marketplace is at $VERSION already"
  else
    if claude plugin marketplace list --json </dev/null 2>/dev/null | marketplace_object | grep -q .; then
      info "Removing the SolidTrace marketplace and its plugins, to add it at $VERSION"
      claude plugin marketplace remove "$MARKETPLACE" </dev/null || fail "could not remove the $MARKETPLACE marketplace"
    fi
    info "Adding the SolidTrace marketplace at $VERSION"
    claude plugin marketplace add "$REPOSITORY#$VERSION" --sparse .claude-plugin cli/plugin </dev/null \
      || fail "could not add the $MARKETPLACE marketplace"
  fi

  info "Installing the solidtrace plugin"
  install_plugin "solidtrace@$MARKETPLACE"
  if [ -z "${BITBUCKET_TOKEN:-}" ] && ! grep -qs '"BITBUCKET_TOKEN"' "$CLAUDE_SETTINGS"; then
    todo "On bitbucket.org, open-pr and update-pr need BITBUCKET_EMAIL and BITBUCKET_TOKEN in env of $CLAUDE_SETTINGS (an Atlassian API token with the scopes read:user:bitbucket, read:repository:bitbucket, read:pullrequest:bitbucket and write:pullrequest:bitbucket). GitHub needs no token."
  fi

  # capture status exits 1 when one of its checks fails, also when this machine
  # is signed in. So read its text, not its exit code, and do not pipe it.
  status="$("$ST" capture status 2>&1 || true)"
  if printf '%s\n' "$status" | grep -q "✓ Sign-in"; then
    info "This machine is signed in already"
  elif [ -n "${SOLIDTRACE_CONSOLE_URL:-}" ] && [ -n "${SOLIDTRACE_INGEST_URL:-}" ]; then
    info "Signing in to $SOLIDTRACE_CONSOLE_URL"
    if [ "$(uname -s)" != Darwin ] && [ -z "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
      "$ST" login --device --ingest-url "$SOLIDTRACE_INGEST_URL" "$SOLIDTRACE_CONSOLE_URL" || fail "the sign-in failed"
    else
      "$ST" login --ingest-url "$SOLIDTRACE_INGEST_URL" "$SOLIDTRACE_CONSOLE_URL" || fail "the sign-in failed"
    fi
  else
    todo "Sign in with the command on Console's Settings → Agent sessions → Capture setup page: $ST login --ingest-url <Ingest URL> <Console URL>"
  fi

  printf '\n'
  claude plugin list 2>/dev/null | grep -A3 -E "solidtrace@solidtrace\$" | sed 's/^/  /' || true
  printf '\n'
  "$ST" capture status 2>&1 | sed -n '1,8p' | sed 's/^/  /' || true
  printf '\nDone: SolidTrace %s. Restart Claude Code: a running session does not load new plugins.\n' "$VERSION"
  if [ -n "$TODO" ]; then
    printf '\nStill to do:\n%s' "$TODO"
  fi
}

info() { printf '==> %s\n' "$1"; }
fail() { printf 'setup: %s\n' "$1" >&2; exit 1; }
todo() { TODO="$TODO  - $1
"; }

# marketplace_object reads `claude plugin marketplace list --json` and prints
# the marketplace MARKETPLACE on one line, with no spaces: such as
# {"name":"solidtrace","source":"github","repo":"o/n","ref":"v0.25.0",...
marketplace_object() {
  tr -d ' \n' | tr '}' '\n' | grep -E "\"name\":\"$MARKETPLACE\"(,|\$)"
}

# marketplace_at_tag is true when the marketplace MARKETPLACE is REPOSITORY
# on GitHub at the tag VERSION.
marketplace_at_tag() {
  claude plugin marketplace list --json </dev/null 2>/dev/null | marketplace_object \
    | grep -F "\"repo\":\"$REPOSITORY\"" | grep -qF "\"ref\":\"$VERSION\""
}

# plugin_installed NAME is true when Claude Code has the plugin NAME.
plugin_installed() {
  claude plugin list 2>/dev/null | grep -qE "(^|[[:space:]])$1\$"
}

# install_plugin NAME installs the plugin NAME, or updates it to the
# marketplace's version.
install_plugin() {
  if plugin_installed "$1"; then
    claude plugin update "$1" </dev/null || fail "could not update $1"
  else
    claude plugin install "$1" </dev/null || fail "could not install $1"
  fi
}

main "$@"
