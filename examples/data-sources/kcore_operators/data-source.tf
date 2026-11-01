data "kcore_operators" "all" {}

output "cluster_admins" {
  value = [for o in data.kcore_operators.all.operators : o.name if contains(o.roles, "cluster-admin")]
}
