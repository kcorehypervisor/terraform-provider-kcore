resource "kcore_cluster" "prod" {
  name      = "prod"
  version   = "v0.8.3"
  certs_dir = "${path.module}/certs"

  update {
    reboot    = false
    drain_vms = true
  }

  node {
    name       = "node-107"
    address    = "192.168.40.107"
    os_disk    = "/dev/sda"
    controller = true
  }

  node {
    name    = "node-105"
    address = "192.168.40.105"
    os_disk = "/dev/sda"
  }
}
