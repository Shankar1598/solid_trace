#!/bin/sh
# Squid with the Dispatcher (compose.dispatcher.yml): the Dispatcher writes the allowlist
# file, and this loop makes Squid read it again when it changes, as the reference
# configuration's systemd path unit does (factory/reference/systemd/squid-allowlist.path).
# Then it starts Squid as the image does.
set -eu
list=/etc/squid/allowlist.txt
(
  last="$(md5sum "$list")"
  while sleep 2; do
    now="$(md5sum "$list")"
    if [ "$now" != "$last" ]; then
      last="$now"
      squid -k reconfigure -f /etc/squid/squid.conf && echo "squid: the allowlist changed: Squid reads it again" >&2
    fi
  done
) &
exec entrypoint.sh -f /etc/squid/squid.conf -NYC
