data "kcore_node" "worker" {
  id = var.node_id
}

output "node_address" {
  value = data.kcore_node.worker.address
}

output "node_dc" {
  value = data.kcore_node.worker.dc_id
}
