#!/bin/sh
# Installs the solidtrace CLI of one SolidTrace release on Linux or macOS.
# It downloads the binary for this OS and architecture and the release's
# checksum file, and installs the binary only when its SHA-256 is the one
# in the checksum file.
#
#   curl -fsSL https://github.com/<owner>/<name>/releases/download/<version>/install.sh | sh
#
# Each release's install.sh has its version and repository in it. These
# variables change what it does:
#   SOLIDTRACE_VERSION       the release to install, such as v0.19.0
#   SOLIDTRACE_INSTALL_DIR   where to put solidtrace (default ~/.local/bin)
#   SOLIDTRACE_DOWNLOAD_URL  the folder of the release files (default the
#                            GitHub release of SOLIDTRACE_VERSION)
set -eu

version="${SOLIDTRACE_VERSION:-@VERSION@}"
repository="@REPOSITORY@"
install_dir="${SOLIDTRACE_INSTALL_DIR:-$HOME/.local/bin}"

die() {
  echo "solidtrace install: $*" >&2
  exit 1
}

case "$version" in
  v[0-9]*) ;;
  *) die "no version: set SOLIDTRACE_VERSION, such as v0.19.0" ;;
esac
if [ -n "${SOLIDTRACE_DOWNLOAD_URL:-}" ]; then
  base="$SOLIDTRACE_DOWNLOAD_URL"
else
  case "$repository" in
    *@*) die "no repository: set SOLIDTRACE_DOWNLOAD_URL" ;;
  esac
  base="https://github.com/$repository/releases/download/$version"
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "this OS is not supported: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "this architecture is not supported: $(uname -m)" ;;
esac
binary="solidtrace-$os-$arch"

download() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https,http' -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1"
  else
    die "curl or wget is necessary"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d ' ' -f 1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d ' ' -f 1
  else
    die "sha256sum or shasum is necessary"
  fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

download "$base/$binary" "$tmp/$binary" || die "could not download $base/$binary"
download "$base/SHA256SUMS" "$tmp/SHA256SUMS" || die "could not download $base/SHA256SUMS"

want="$(awk -v name="$binary" '$2 == name { print $1 }' "$tmp/SHA256SUMS")"
[ -n "$want" ] || die "SHA256SUMS has no line for $binary"
got="$(sha256 "$tmp/$binary")"
if [ "$got" != "$want" ]; then
  die "the SHA-256 of $binary is $got, but SHA256SUMS gives $want: not installed"
fi

mkdir -p "$install_dir"
chmod 755 "$tmp/$binary"
mv -f "$tmp/$binary" "$install_dir/solidtrace"
echo "Installed solidtrace $version in $install_dir/solidtrace."
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to your PATH, so that the Claude Code plugin finds solidtrace." ;;
esac
echo "Next: solidtrace login (Console's capture setup page gives the full command)."
