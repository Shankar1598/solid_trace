# The fallback policy of one machine kind. Use it in place of policies/standard.hcl, and only
# when cluster check 4 fails (ticket 24: the Nomad APM with gce-mig, and scale-in with the empty
# selector). It reads the number of busy VMs from Prometheus, and the pass-through strategy takes
# the query's value as the number of VMs that the group must have (the Autoscaler keeps it
# between min and max).
#
# It needs Nomad's metrics in Prometheus (telemetry with prometheus_metrics, and
# publish_allocation_metrics and publish_node_metrics on the clients), and the "prometheus" apm
# block of autoscaler.hcl. The query counts the VMs of the node pool that have memory allocated,
# and divides by 0.7 (the target of 70%), as the main policy does. With no busy VM it gives 0,
# so the group goes down to its minimum. Check the name of the metric and of its node_pool label
# in your Prometheus: they depend on the Nomad version.

scaling "factory-standard" {
  enabled = true
  type    = "cluster"
  min     = 1
  max     = 10

  policy {
    evaluation_interval  = "30s"
    cooldown             = "5m"
    cooldown_on_scale_up = "1m"

    check "busy_vms" {
      source = "prometheus"
      query  = "ceil((count(nomad_client_allocated_memory{node_pool=\"standard\"} > 0) or vector(0)) / 0.7)"

      strategy "pass-through" {}
    }

    target "gce-mig" {
      project  = "my-project"
      zone     = "europe-west1-b"
      mig_name = "factory-agents-standard"

      node_pool              = "standard"
      node_selector_strategy = "empty"
      node_drain_deadline    = "5m"
      node_purge             = "true"
    }
  }
}
