data "kcore_security_groups" "all" {}

output "security_group_names" {
  value = [for g in data.kcore_security_groups.all.security_groups : g.name]
}
