data "kcore_cluster_updates" "all" {}

output "update_phases" {
  value = { for u in data.kcore_cluster_updates.all.updates : u.name => u.phase }
}
