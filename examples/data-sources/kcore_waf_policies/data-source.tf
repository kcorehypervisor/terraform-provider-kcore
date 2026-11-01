data "kcore_waf_policies" "all" {}

output "waf_policy_names" {
  value = [for p in data.kcore_waf_policies.all.policies : p.name]
}
