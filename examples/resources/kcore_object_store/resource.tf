resource "kcore_object_store" "s3" {
  name         = "s3"
  ceph_cluster = kcore_ceph_cluster.lab.name
  port         = 7480
}
