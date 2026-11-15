#!/bin/sh
# The agent machine's firewall, as a VPC firewall is for an agent VM: it is outside the
# machine. This container owns the network namespace that the agent machine container
# shares (network_mode: service:agent-firewall). Only this container has NET_ADMIN, so
# no process on the agent machine, root too, can change the rules.
#
# Out: only to the server side of the factory network (the subnet of eth0), on three
# ports: Squid (3128), Nomad RPC (4647) and the Dispatcher's private listener. In: no
# new connection (a take-over on the local stack uses docker exec, not ssh).
set -eu

dispatcher_port="${DISPATCHER_PORT:?DISPATCHER_PORT}"
subnet=$(ip -o -4 route show dev eth0 scope link | awk '{ print $1; exit }')
[ -n "$subnet" ] || { echo "agent-firewall: no IPv4 subnet on eth0" >&2; exit 1; }

iptables -F INPUT
iptables -F OUTPUT
iptables -F FORWARD
iptables -P INPUT DROP
iptables -P FORWARD DROP
iptables -P OUTPUT DROP

# Loopback carries Docker's DNS (127.0.0.11), and the local Nomad client and hc.
iptables -A INPUT -i lo -j ACCEPT
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
for port in 3128 4647 "$dispatcher_port"; do
  iptables -A OUTPUT -p tcp -d "$subnet" --dport "$port" -m conntrack --ctstate NEW -j ACCEPT
done
# Reject, not drop, so that a refused connection fails at once.
iptables -A OUTPUT -p tcp -j REJECT --reject-with tcp-reset
iptables -A OUTPUT -j REJECT

for policy in INPUT FORWARD OUTPUT; do ip6tables -P "$policy" DROP; done
ip6tables -A INPUT -i lo -j ACCEPT
ip6tables -A OUTPUT -o lo -j ACCEPT

echo "agent-firewall: out only to $subnet on ports 3128, 4647 and $dispatcher_port"
touch /run/agent-firewall-ready

trap 'exit 0' TERM INT
sleep infinity &
wait
