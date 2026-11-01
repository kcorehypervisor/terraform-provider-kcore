data "kcore_ingress_routes" "all" {}

output "routes_by_gateway" {
  value = { for r in data.kcore_ingress_routes.all.routes : r.name => r.gateway }
}
