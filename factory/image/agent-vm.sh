#!/bin/sh
# The agent VM image recipe (README.md). Run it as root on Debian 12 in your image
# pipeline. It installs and sets:
#
# - Claude Code and hc at the pinned versions below, each checked by its SHA-256;
# - the wrapper (/usr/local/bin/factory-wrapper) and the agent CLI (solidtrace), from
#   FACTORY_BIN_DIR (factory/image/build makes them);
# - Claude Code's first-run dialogs, accepted in files that root owns;
# - the run user agent (not root, in the docker group) and the access group;
# - the one sudoers rule: members of the access group run exactly "hc connect" as the
#   run user, with no password;
# - git: no credential helper of the system (the wrapper gives each Execution its
#   own), and git over ssh rewritten to HTTPS;
# - the proxy settings, for logins, the Docker daemon and the Nomad client;
# - the Nomad client's settings for the Runner's job (not Nomad itself).
#
#   FACTORY_PROXY_URL=http://10.100.0.3:3128 FACTORY_DISPATCHER_HOST=10.100.0.3 \
#   FACTORY_BIN_DIR=/tmp/factory-bin ./agent-vm.sh [all|system|factory]
#
# Settings (environment variables):
#   FACTORY_PROXY_URL        Squid on the server VM, as http://<host>:<port> (required)
#   FACTORY_DISPATCHER_HOST  the host of the Dispatcher's private listener, which tools
#                            reach direct, not through the proxy (required)
#   FACTORY_BIN_DIR          the directory with factory-wrapper and solidtrace (required
#                            for the phases all and factory)
#   FACTORY_ACCESS_GROUP     the access group, which OS Login gives to each person of
#                            the Google group (default factory-access)
#   FACTORY_DOWNLOAD_CACHE   a directory that keeps the downloads for the next run
#
# The phase "system" does all but the two binaries of this repository; the phase
# "factory" installs them. "all" (the default) does both. An image pipeline runs "all";
# the local stack's Dockerfile runs the two phases in two layers.
#
# The run user is agent, the wrapper's default (WRAPPER_RUN_USER, WRAPPER_HOME
# /home/agent): nothing gives another name to the wrapper, so it is not a setting.
#
# The recipe checks each setting before it changes anything. A second run changes
# nothing. The image has no credential of any kind: a Run gets its tokens from the
# Dispatcher.
set -eu

CLAUDE_CODE_VERSION=2.1.325
CLAUDE_CODE_SHA256=0dc4f4a9a165fc60746d7ae8489712e5fc352f1e7ad4eb22e7141bd0812eb0cf
HC_VERSION=0.1.21
HC_SHA256=78424755c10a40dde38a0bdb55c20ac19706e9ae05ed68fc2acf1fcff0d941c1

here="$(cd "$(dirname "$0")" && pwd)"
phase="${1:-all}"
run_user=agent
access_group="${FACTORY_ACCESS_GROUP-factory-access}"
proxy_url="${FACTORY_PROXY_URL:-}"
dispatcher_host="${FACTORY_DISPATCHER_HOST:-}"
bin_dir="${FACTORY_BIN_DIR:-}"
cache="${FACTORY_DOWNLOAD_CACHE:-}"

die() {
  echo "agent-vm.sh: $*" >&2
  exit 1
}

# --- Check the settings first: a bad value never gets into a file ----------------------

[ "$(id -u)" = 0 ] || die "must run as root"
case "$phase" in
  all | system | factory) ;;
  *) die "no phase '$phase': use all, system or factory" ;;
esac

# A user or group name: lower case letters, digits, '_' and '-', at most 32 characters.
# So a name cannot change the sudoers rule: no space, comma, '%', '=' or new line.
is_name() {
  printf '%s' "$1" | grep -q -x -E '[a-z_][a-z0-9_-]{0,31}' && [ "$(printf '%s' "$1" | wc -l)" = 0 ]
}
if [ -n "${FACTORY_RUN_USER+set}" ] && [ "$FACTORY_RUN_USER" != "$run_user" ]; then
  die "FACTORY_RUN_USER is not a setting: the run user is $run_user, the wrapper's default (WRAPPER_RUN_USER)"
fi
if getent passwd "$run_user" >/dev/null && [ "$(id -u "$run_user")" = 0 ]; then
  die "the run user $run_user has the user id 0"
fi
is_name "$access_group" || die "FACTORY_ACCESS_GROUP '$access_group' is not a group name"
case "$access_group" in
  root | docker | sudo | adm | "$run_user") die "FACTORY_ACCESS_GROUP must not be $access_group" ;;
esac

# The proxy and the Dispatcher's host go into environment files: a host name or an IP
# address, and a port. No user name or password: the image has no credential.
printf '%s' "$proxy_url" | grep -q -x -E 'http://[A-Za-z0-9.-]+:[0-9]{1,5}' && [ "$(printf '%s' "$proxy_url" | wc -l)" = 0 ] ||
  die "FACTORY_PROXY_URL '$proxy_url' is not http://<host>:<port>"
printf '%s' "$dispatcher_host" | grep -q -x -E '[A-Za-z0-9.-]+' && [ "$(printf '%s' "$dispatcher_host" | wc -l)" = 0 ] ||
  die "FACTORY_DISPATCHER_HOST '$dispatcher_host' is not a host name or an IP address"

if [ "$phase" != system ]; then
  [ -n "$bin_dir" ] && [ -f "$bin_dir/factory-wrapper" ] && [ -f "$bin_dir/solidtrace" ] ||
    die "FACTORY_BIN_DIR '$bin_dir' has no factory-wrapper and solidtrace (factory/image/build makes them)"
fi
[ -f "$here/files/managed-settings.json" ] && [ -f "$here/files/claude.json" ] ||
  die "no files/ beside the recipe ($here)"

# --- The steps ---------------------------------------------------------------------------

# Puts the file at URL $1 with the SHA-256 $2 at path $3. A file in the cache with
# another SHA-256 is downloaded again; a download with another SHA-256 is deleted.
download() {
  url="$1" sha="$2" path="$3"
  if [ -n "$cache" ]; then
    mkdir -p "$cache"
    cached="$cache/$sha-$(basename "$url")"
    if [ -f "$cached" ] && echo "$sha  $cached" | sha256sum -c --status -; then
      cp "$cached" "$path"
      return 0
    fi
  fi
  rm -f "$path"
  curl -fsSL --retry 3 -o "$path" "$url" || die "cannot download $url"
  if ! echo "$sha  $path" | sha256sum -c --status -; then
    rm -f "$path"
    die "the SHA-256 of $url is not the pinned one ($sha)"
  fi
  [ -z "$cache" ] || cp "$path" "$cached"
}

# Installs file $1 at $2, owned by root, with mode $3.
put() { install -o root -g root -m "$3" "$1" "$2"; }

packages() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -q
  apt-get install -y -q --no-install-recommends ca-certificates curl git sudo procps
  rm -rf /var/lib/apt/lists/*
}

claude_code() {
  tmp="$(mktemp)"
  download "https://downloads.claude.ai/claude-code-releases/$CLAUDE_CODE_VERSION/linux-x64/claude" "$CLAUDE_CODE_SHA256" "$tmp"
  put "$tmp" /usr/local/bin/claude 755
  rm -f "$tmp"
}

herdr_core() {
  tmp="$(mktemp -d)"
  download "https://releases.herdr.dev/core/$HC_VERSION/herdr-core-linux-x86_64.tar.gz" "$HC_SHA256" "$tmp/hc.tar.gz"
  tar -xzf "$tmp/hc.tar.gz" -C "$tmp" herdr-core
  put "$tmp/herdr-core" /usr/local/bin/herdr-core 755
  ln -sfn herdr-core /usr/local/bin/hc
  rm -rf "$tmp"
}

# The run user (not root, in the docker group) and the access group. The Docker
# package, when the pipeline installs it, uses the group that is here.
users() {
  getent group docker >/dev/null || groupadd --system docker
  getent group "$access_group" >/dev/null || groupadd --system "$access_group"
  getent passwd "$run_user" >/dev/null || useradd --create-home --shell /bin/bash "$run_user"
  usermod --append --groups docker "$run_user"
  # The Nomad job gives each wrapper the directory <executions>/<execution>, which the
  # wrapper makes: root owns each parent, and the run user can only search them.
  install -d -o root -g root -m 755 /var/lib/factory
  install -d -o root -g root -m 711 /var/lib/factory/executions
}

# The take-over rule. visudo checks the file before it goes in place.
sudoers() {
  tmp="$(mktemp)"
  printf '%s\n' \
    '# The take-over rule: the one sudoers rule of an agent VM (factory/image/agent-vm.sh).' \
    '# Members of the access group run exactly "hc connect" as the run user, with no' \
    '# password, and nothing else.' \
    "%$access_group ALL=($run_user) NOPASSWD: /usr/local/bin/hc connect" >"$tmp"
  visudo -c -q -f "$tmp" || die "visudo refuses the take-over rule"
  put "$tmp" /etc/sudoers.d/factory-take-over 440
  rm -f "$tmp"
  visudo -c -q || die "visudo refuses the sudoers files"
}

# Claude Code's first-run dialogs, accepted in files that root owns. The wrapper deletes
# the run user's home after each Execution and sets CLAUDE_CONFIG_DIR, so files in the
# home do not apply to a Run.
# - The managed settings, which the run user cannot change: no bypass-mode warning, and
#   no auto-updater, so that the pinned version stays.
# - The template of the run user's .claude.json, with the onboarding done. For each
#   Execution the wrapper copies it to $CLAUDE_CONFIG_DIR/.claude.json and adds
#   projects["<checkout>"].hasTrustDialogAccepted for its checkout: Claude Code trusts
#   a checkout only at its exact path.
dialogs() {
  install -d -o root -g root -m 755 /etc/claude-code /etc/factory
  put "$here/files/managed-settings.json" /etc/claude-code/managed-settings.json 644
  put "$here/files/claude.json" /etc/factory/claude.json 644
}

git_settings() {
  # The wrapper writes the credential helper of each Execution, for the forge's URL only.
  git config --system --unset-all credential.helper || true
  for host in github.com bitbucket.org; do
    git config --system --unset-all "url.https://$host/.insteadOf" || true
    git config --system --add "url.https://$host/.insteadOf" "git@$host:"
    git config --system --add "url.https://$host/.insteadOf" "ssh://git@$host/"
  done
}

# Tools read HTTPS_PROXY. NO_PROXY has the Dispatcher's host (its private listener,
# which Squid refuses) and the metadata server, which stays reachable direct.
proxy() {
  no_proxy="localhost,127.0.0.1,169.254.169.254,metadata.google.internal,$dispatcher_host"
  tmp="$(mktemp)"
  {
    echo '# The proxy settings of the agent VM (factory/image/agent-vm.sh).'
    echo "HTTPS_PROXY=$proxy_url"
    echo "HTTP_PROXY=$proxy_url"
    echo "https_proxy=$proxy_url"
    echo "http_proxy=$proxy_url"
    echo "NO_PROXY=$no_proxy"
    echo "no_proxy=$no_proxy"
  } >"$tmp"
  put "$tmp" /etc/factory/proxy.env 644
  rm -f "$tmp"
  # Logins (ssh, su) read /etc/environment through PAM: replace the recipe's lines.
  touch /etc/environment
  grep -v -E '^(HTTPS?_PROXY|https?_proxy|NO_PROXY|no_proxy|DISABLE_AUTOUPDATER)=' /etc/environment >/etc/environment.new || true
  { grep -v '^#' /etc/factory/proxy.env; echo DISABLE_AUTOUPDATER=1; } >>/etc/environment.new
  chmod 644 /etc/environment.new
  mv /etc/environment.new /etc/environment
  # The Docker daemon and the Nomad client (and so each task that it starts: the
  # wrapper, the harness and its tools) get the same file from systemd.
  for unit in docker nomad; do
    install -d -o root -g root -m 755 "/etc/systemd/system/$unit.service.d"
    printf '%s\n' '# The proxy settings of the agent VM (factory/image/agent-vm.sh).' '[Service]' \
      'EnvironmentFile=/etc/factory/proxy.env' >"/etc/systemd/system/$unit.service.d/factory-proxy.conf"
    chmod 644 "/etc/systemd/system/$unit.service.d/factory-proxy.conf"
  done
}

# The Nomad client's settings that the Runner's job needs. The operator's own file in
# /etc/nomad.d has the rest (servers, the node pool, the introduction token).
nomad_settings() {
  install -d -o root -g root -m 755 /etc/nomad.d
  printf '%s\n' \
    '# The Nomad client settings that the factory needs (factory/image/agent-vm.sh).' \
    '# The wrapper runs as root under raw_exec, and changes to the run user itself.' \
    'plugin "raw_exec" {' \
    '  config {' \
    '    enabled = true' \
    '  }' \
    '}' \
    '' \
    '# The Runner gives each task 120 seconds to stop (kill_timeout): the wrapper'"'"'s end' \
    '# procedure and its preemption handler need them.' \
    'client {' \
    '  max_kill_timeout = "120s"' \
    '}' >/etc/nomad.d/factory.hcl
  chmod 644 /etc/nomad.d/factory.hcl
}

factory_binaries() {
  put "$bin_dir/factory-wrapper" /usr/local/bin/factory-wrapper 755
  put "$bin_dir/solidtrace" /usr/local/bin/solidtrace 755
}

if [ "$phase" != factory ]; then
  packages
  claude_code
  herdr_core
  users
  sudoers
  dialogs
  git_settings
  proxy
  nomad_settings
  # The recipe that made this image, so that a check can compare it.
  sha256sum <"$0" | cut -d' ' -f1 >/etc/factory/image-recipe
  chmod 644 /etc/factory/image-recipe
fi
if [ "$phase" != system ]; then
  factory_binaries
fi
echo "agent-vm.sh: done ($phase)"
