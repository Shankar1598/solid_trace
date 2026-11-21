# The Nomad client of an agent VM (docs/factory.md). Your image pipeline or the VM's startup
# puts this file at /etc/nomad.d/client.hcl. The image recipe (factory/image/agent-vm.sh)
# writes /etc/nomad.d/factory.hcl beside it, which turns raw_exec on and sets
# max_kill_timeout to 120 seconds: Nomad reads both files.
#
# Set SERVER_IP to the server VM's private address, and node_pool to the machine kind's name
# (the name in DISPATCHER_MACHINE_KINDS). The pool must exist on the server:
#   nomad node pool apply standard.pool.hcl     (a file: node_pool "standard" { description = "..." })
# A client registers with an introduction token, which you make on the server for each VM:
#   nomad node intro create -node-name VM_NAME -ttl 10m
# Put the token in the client's data directory, as the file client/intro_token.jwt, readable
# by root only. Do not put it in the VM's metadata.
#
# HashiCorp's package also puts /etc/nomad.d/nomad.hcl in place, which turns on a server and a
# client and binds to 0.0.0.0. Nomad merges the files, and "enabled = false" does not override
# a true. Empty that file (: > /etc/nomad.d/nomad.hcl) before you start Nomad.

data_dir = "/opt/nomad/data"

client {
  enabled   = true
  servers   = ["SERVER_IP:4647"]
  node_pool = "standard"
}

acl {
  enabled = true
}

disable_update_check = true
