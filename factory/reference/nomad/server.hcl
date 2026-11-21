# The Nomad server of the factory, on the server VM (docs/factory.md). One server. Nomad is
# not part of SolidTrace's images (ADR 0019): you install it from HashiCorp's packages.
# Put this file at /etc/nomad.d/server.hcl, and set PRIVATE_IP to the VM's private address.
#
# HashiCorp's package also puts /etc/nomad.d/nomad.hcl in place, which turns on a server and a
# client and binds to 0.0.0.0. Nomad merges the files, and "enabled = false" does not override
# a true. Empty that file (: > /etc/nomad.d/nomad.hcl) before you start Nomad.

data_dir  = "/opt/nomad/data"
bind_addr = "PRIVATE_IP"

server {
  enabled          = true
  bootstrap_expect = 1

  # A Nomad client registers only with an introduction token, so a process on an agent
  # VM cannot add a client of its own that Nomad would give a Run's task to.
  client_introduction {
    enforcement = "strict"
  }
}

# The server VM runs no task.
client {
  enabled = false
}

# With ACLs on, each call to Nomad's API needs a token.
acl {
  enabled = true
}

disable_update_check = true
