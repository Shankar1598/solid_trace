# The Nomad client of the agent machine. It runs as root, and its raw_exec tasks run
# as root: the wrapper changes to the run user itself. The image recipe's
# /etc/nomad.d/factory.hcl turns raw_exec on and sets max_kill_timeout (120 seconds) for
# the Runner's job; Nomad reads both files.

data_dir  = "/var/lib/nomad"
bind_addr = "0.0.0.0"

advertise {
  http = "{{ GetInterfaceIP \"eth0\" }}"
  rpc  = "{{ GetInterfaceIP \"eth0\" }}"
  serf = "{{ GetInterfaceIP \"eth0\" }}"
}

client {
  enabled           = true
  servers           = ["nomad:4647"]
  network_interface = "eth0"

  # The client sees the dev box's disk. When it is nearly full, the usual threshold
  # (80 percent) deletes each allocation, with its logs, at once.
  gc_disk_usage_threshold  = 100
  gc_inode_usage_threshold = 100

  meta {
    machine = "local"
  }
}

acl {
  enabled = true
}

disable_update_check = true
