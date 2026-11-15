# The Nomad server of the local factory stack: one server, as in dev mode, but with no
# client of its own, so that Nomad places each task on the agent machine. ACLs are on,
# so that a process on the agent machine (the run user too) cannot register a job
# through a Nomad API with no token. The stack script bootstraps the ACLs.
#
# ACLs do not stop a node registration. So client introduction is strict: a client
# registers only with an introduction token. The stack script makes one for the agent
# machine's client and puts it in a file that only root on the agent machine can read.
# A Nomad client that the run user starts cannot register, so the server never places
# a Run's task on it.

data_dir  = "/nomad/data"
bind_addr = "0.0.0.0"

advertise {
  http = "{{ GetInterfaceIP \"eth0\" }}"
  rpc  = "{{ GetInterfaceIP \"eth0\" }}"
  serf = "{{ GetInterfaceIP \"eth0\" }}"
}

server {
  enabled          = true
  bootstrap_expect = 1

  client_introduction {
    enforcement = "strict"
  }
}

client {
  enabled = false
}

acl {
  enabled = true
}

disable_update_check = true
