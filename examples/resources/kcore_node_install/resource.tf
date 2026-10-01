resource "kcore_node_install" "controller" {
  address        = "10.0.0.8:9091"
  os_disk        = "/dev/nvme0n1"
  certs_dir      = kcore_cluster.lab.certs_dir
  run_controller = true
  hostname       = "ctrl-1"
  node_id        = "kcore-node-10.0.0.8"
}

resource "kcore_node_install" "worker" {
  address         = "10.0.0.12:9091"
  os_disk         = "/dev/nvme0n1"
  certs_dir       = kcore_cluster.lab.certs_dir
  join_controller = "10.0.0.8:9090"
  hostname        = "worker-1"
}
