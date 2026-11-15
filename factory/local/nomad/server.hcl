# The Nomad server of the local factory stack: one server, as in dev mode, but with no
# client of its own, so that Nomad places each task on the agent machine. ACLs are on,
# so that a process on the agent machine (the run user too) cannot register a job
# through a Nomad API with no token. The stack script bootstraps the ACLs.

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
}

client {
  enabled = false
}

acl {
  enabled = true
}

disable_update_check = true
