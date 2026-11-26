#!/bin/sh
# Installs or upgrades SolidTrace for Claude Code on this machine: the
# solidtrace CLI of a release, the SolidTrace marketplace, the capture plugin
# and the review plugin. It asks no questions. Run it again to upgrade.
#
#   curl -fsSL https://raw.githubusercontent.com/Shankar1598/solid_trace/main/cli/setup-claude-code.sh | sh
#
# Environment:
#   SOLIDTRACE_VERSION      the release, such as v0.24.3 (default: the newest)
#   SOLIDTRACE_INSTALL_DIR  the folder of solidtrace (default: ~/.local/bin)
#   SOLIDTRACE_REVIEW_PLUGIN=no   do not install the review plugin
#   SOLIDTRACE_CONSOLE_URL and SOLIDTRACE_INGEST_URL   sign in when this
#                           machine has no sign-in (Console's Capture setup
#                           page gives both)

set -eu

# All of the script is in main, so that through curl | sh, sh reads all of it
# before a command can read stdin.
main() {
  REPOSITORY="Shankar1598/solid_trace"
  MARKETPLACE_FILE="$HOME/.local/share/solidtrace/.claude-plugin/marketplace.json"
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
  RELEASE_URL="https://github.com/$REPOSITORY/releases/download/$VERSION"
  info "SolidTrace $VERSION (now: $("$ST" version 2>/dev/null || echo "no solidtrace in $BIN_DIR"))"

  info "Installing the solidtrace CLI in $BIN_DIR"
  curl -fsSL "$RELEASE_URL/install.sh" | SOLIDTRACE_VERSION="$VERSION" SOLIDTRACE_INSTALL_DIR="$BIN_DIR" sh \
    || fail "the CLI install failed"
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) todo "Add $BIN_DIR to PATH, so that the capture plugin finds solidtrace." ;;
  esac

  # Claude Code cannot add the marketplace file from a github.com URL, so the
  # script downloads it into a .claude-plugin folder and adds it from there.
  info "Adding the SolidTrace marketplace"
  mkdir -p "$(dirname "$MARKETPLACE_FILE")"
  curl -fsSL -o "$MARKETPLACE_FILE.new" "$RELEASE_URL/marketplace.json" \
    || fail "could not download $RELEASE_URL/marketplace.json"
  mv -f "$MARKETPLACE_FILE.new" "$MARKETPLACE_FILE"
  if claude plugin marketplace list 2>/dev/null | grep -qF "$MARKETPLACE_FILE"; then
    claude plugin marketplace update solidtrace </dev/null || fail "could not update the solidtrace marketplace"
  else
    claude plugin marketplace add "$MARKETPLACE_FILE" </dev/null || fail "could not add the solidtrace marketplace"
  fi

  info "Installing the capture plugin"
  install_plugin solidtrace@solidtrace

  if [ "${SOLIDTRACE_REVIEW_PLUGIN:-yes}" = no ]; then
    info "Not installing the review plugin (SOLIDTRACE_REVIEW_PLUGIN=no)"
  else
    info "Installing the review plugin"
    install_plugin solidtrace-skills@solidtrace
    if [ -z "${BITBUCKET_TOKEN:-}" ] && ! grep -qs '"BITBUCKET_TOKEN"' "$CLAUDE_SETTINGS"; then
      todo "On bitbucket.org, open-pr and update-pr need BITBUCKET_EMAIL and BITBUCKET_TOKEN in env of $CLAUDE_SETTINGS (an Atlassian API token with the scopes read:user:bitbucket, read:repository:bitbucket, read:pullrequest:bitbucket and write:pullrequest:bitbucket). GitHub needs no token."
    fi
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
  claude plugin list 2>/dev/null | grep -A3 -E "solidtrace(-skills)?@solidtrace\$" | sed 's/^/  /' || true
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
