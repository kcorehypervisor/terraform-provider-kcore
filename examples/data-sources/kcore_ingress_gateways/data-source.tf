data "kcore_ingress_gateways" "all" {}

output "gateway_phases" {
  value = { for g in data.kcore_ingress_gateways.all.gateways : g.name => g.phase }
}
