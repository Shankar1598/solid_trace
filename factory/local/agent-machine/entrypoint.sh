#!/bin/sh
# Starts the agent machine of the local factory stack: it prepares cgroup v2 for the
# Nomad client and the wrapper, then starts the Nomad client.
#
# The container gets SYS_ADMIN (and no AppArmor profile) so that this script can make
# the cgroup file system writable, as it is on a VM, and so that the Nomad client can
# mount each task's secrets directory. Root on the agent machine is then root on the
# dev box, not root on a VM (README), so the run user has no capabilities and no way
# to root. The container has no NET_ADMIN, so no process here can change the firewall
# (agent-firewall).
set -eu

cg=/sys/fs/cgroup
[ "$(stat -fc %T "$cg")" = cgroup2fs ] || { echo "agent-machine: cgroup v2 is necessary" >&2; exit 1; }
mount -o remount,rw "$cg"

# cgroup v2 lets a cgroup give controllers to its children only when it has no
# processes of its own. So move this container's processes into a child, and give the
# controllers to the children of the container's cgroup (Nomad's nomad.slice).
mkdir -p "$cg/init"
for pid in $(cat "$cg/cgroup.procs"); do
  echo "$pid" >"$cg/init/cgroup.procs" 2>/dev/null || true
done
for controller in $(cat "$cg/cgroup.controllers"); do
  echo "+$controller" >"$cg/cgroup.subtree_control"
done
[ -e "$cg/init/cgroup.kill" ] || { echo "agent-machine: cgroup.kill is necessary (Linux 5.14 or later)" >&2; exit 1; }

nomad=/opt/nomad/bin/nomad
[ -x "$nomad" ] || { echo "agent-machine: no Nomad binary at $nomad" >&2; exit 1; }
exec "$nomad" agent -config=/etc/nomad.d/client.hcl
