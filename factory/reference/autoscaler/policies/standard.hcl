# The scaling policy of one machine kind (docs/factory.md, "Scaling"). Make one file like this
# for each kind of DISPATCHER_MACHINE_KINDS: change the name of the policy, the node pool and
# the group, and copy the rest.
#
# The Autoscaler looks at the memory that Nomad allocated on the machines of the node pool.
# Each Run takes all the memory of one VM, so the share is the share of busy VMs. With a target
# of 70%, the Autoscaler keeps about one free VM for each two or three busy VMs. A pool that is
# idle goes down to its minimum. The Autoscaler removes only a VM that has no allocation: it
# never ends a Run to save a VM (node_selector_strategy "empty"). Runs over the maximum wait in
# the queue: the Run's page says "waiting for a VM", and after 10 minutes there is a warning Log.
#
# Set min and max (the operator's choice; the first team uses 1 and 10), the Google Cloud
# project, the zone, and the name of the managed instance group. For a regional group, use
# "region" in place of "zone".

scaling "factory-standard" {
  enabled = true
  type    = "cluster"
  min     = 1
  max     = 10

  policy {
    # A new VM needs about two minutes to start (more when Spot capacity is short). Look often,
    # add fast, and remove slowly.
    evaluation_interval  = "30s"
    cooldown             = "5m"
    cooldown_on_scale_up = "1m"

    check "allocated_memory" {
      source = "nomad-apm"
      query  = "percentage-allocated_memory"

      strategy "target-value" {
        target = 70
      }
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
