# The Nomad Autoscaler agent of the factory (docs/factory.md, "Scaling"). It runs on the server
# VM, with docker-compose.yml of this directory, and reads the policies of
# /etc/nomad-autoscaler/policies (policies/ of this directory: one file for each machine kind).
# Set SERVER_IP to the server VM's private address. The Nomad token comes from the environment
# (NOMAD_TOKEN, in the file autoscaler.env).

nomad {
  address = "http://SERVER_IP:4646"
}

policy {
  dir = "/etc/nomad-autoscaler/policies"

  # The factory has no scaling policy in a Nomad job: read the files only.
  source "nomad" {
    enabled = false
  }
}

# The Nomad APM and the target-value strategy load with no block. The gce-mig target is in the
# Autoscaler's binary too (no plugin directory), but it loads only when this block names it. It
# reads Google's credentials from the VM's service account (the metadata server).
target "gce-mig" {
  driver = "gce-mig"
}

#
# The fallback policy (fallback/) reads Prometheus: uncomment this block when you use it, and
# put the address of your Prometheus in it.
#
# apm "prometheus" {
#   driver = "prometheus"
#   config = {
#     address = "http://127.0.0.1:9090"
#   }
# }

http {
  bind_address = "127.0.0.1"
  bind_port    = 8080
}
