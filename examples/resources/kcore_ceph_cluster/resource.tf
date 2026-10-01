resource "kcore_ceph_cluster" "lab" {
  name           = "lab"
  public_network = "10.0.0.0/24"
  size           = 3
  min_size       = 2

  node {
    node_id    = "kcore-node-10.0.0.8"
    osd_device = "/dev/sdb"
  }

  node {
    node_id    = "kcore-node-10.0.0.12"
    osd_device = "/dev/sdb"
  }

  node {
    node_id    = "kcore-node-10.0.0.13"
    osd_device = "/dev/sdb"
  }
}
