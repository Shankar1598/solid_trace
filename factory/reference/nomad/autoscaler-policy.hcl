# The Nomad ACL policy of the Autoscaler's token (docs/factory.md, "Scaling"). The Autoscaler
# reads the nodes and their allocations, and drains a node that it removes. It cannot submit or
# change a job, and it cannot change the ACLs. These are the rights that HashiCorp's guide for
# cluster scaling names; cluster check 4 (ticket 24) tries them on a real cluster.
#
#   nomad acl policy apply factory-autoscaler autoscaler-policy.hcl
#   nomad acl token create -name factory-autoscaler -policy factory-autoscaler -type client
#
# Change "default" when DISPATCHER_NOMAD_NAMESPACE names another namespace.

namespace "default" {
  capabilities = ["list-jobs", "read-job"]
}

node {
  policy = "write"
}

operator {
  policy = "read"
}
