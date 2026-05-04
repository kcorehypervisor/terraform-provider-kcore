data "kcore_vm" "existing" {
  id = var.vm_id
  # Optional hint when the controller needs disambiguation:
  # target_node = "10.0.0.5:9091"
}

output "vm_node_id" {
  value = data.kcore_vm.existing.node_id
}

output "vm_assigned_ip" {
  value = data.kcore_vm.existing.assigned_ip
}
