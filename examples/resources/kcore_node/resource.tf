resource "kcore_node" "controller" {
  bootstrap      = true
  address        = "10.0.0.8:9091"
  os_disk        = "/dev/nvme0n1"
  certs_dir      = kcore_cluster.lab.certs_dir
  run_controller = true
  hostname       = "ctrl-1"
  node_id        = "kcore-node-10.0.0.8"
}

resource "kcore_node" "worker_a" {
  address         = "10.0.0.12:9091"
  os_disk         = "/dev/nvme0n1"
  certs_dir       = kcore_cluster.lab.certs_dir
  join_controller = kcore_node.controller.controller_address
  hostname        = "worker-1"
}

resource "kcore_node" "worker_b" {
  address         = "10.0.0.13:9091"
  os_disk         = "/dev/nvme0n1"
  certs_dir       = kcore_cluster.lab.certs_dir
  join_controller = kcore_node.controller.controller_address
  hostname        = "worker-2"
}
