# The agent machine's firewall for the local factory stack (see firewall.sh).
FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends iptables iproute2 \
 && rm -rf /var/lib/apt/lists/*

COPY firewall.sh /usr/local/bin/firewall.sh
RUN chmod 755 /usr/local/bin/firewall.sh

HEALTHCHECK --interval=1s --timeout=1s --retries=30 CMD test -f /run/agent-firewall-ready
CMD ["/usr/local/bin/firewall.sh"]
