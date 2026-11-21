# The Nomad ACL policy of the Dispatcher's token (docs/factory.md). The Dispatcher registers
# one parameterized job for each machine kind, dispatches it for each Execution, reads the
# allocations and the nodes to follow them, and stops a job. It can do nothing else: it
# cannot read a task's logs or secrets, run a command in an allocation, change the node
# pools, or change the cluster's ACLs.
#
#   nomad acl policy apply factory-dispatcher dispatcher-policy.hcl
#   nomad acl token create -name factory-dispatcher -policy factory-dispatcher -type client
#
# Change "default" when DISPATCHER_NOMAD_NAMESPACE names another namespace.

namespace "default" {
  capabilities = ["submit-job", "dispatch-job", "read-job", "list-jobs"]
}

node {
  policy = "read"
}
