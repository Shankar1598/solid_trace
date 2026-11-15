# The agent machine's firewall for the local factory stack (see firewall.sh).
FROM debian:bookworm-20261104-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

RUN apt-get update \
 && apt-get install -y --no-install-recommends iptables iproute2 \
 && rm -rf /var/lib/apt/lists/*

COPY firewall.sh /usr/local/bin/firewall.sh
RUN chmod 755 /usr/local/bin/firewall.sh

HEALTHCHECK --interval=1s --timeout=1s --retries=30 CMD test -f /run/agent-firewall-ready
CMD ["/usr/local/bin/firewall.sh"]
