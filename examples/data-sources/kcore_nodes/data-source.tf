data "kcore_nodes" "all" {}

output "node_hostnames" {
  value = [for n in data.kcore_nodes.all.nodes : n.hostname]
}
