resource "kcore_shared_filesystem" "home" {
  name         = "home"
  ceph_cluster = kcore_ceph_cluster.lab.name
  fs_name      = "home"

  client {
    name  = "web"
    paths = ["/"]
  }
}
