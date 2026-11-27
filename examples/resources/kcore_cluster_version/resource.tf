resource "kcore_cluster_version" "this" {
  version             = "v0.8.2"
  drain_vms           = true
  canary_soak_seconds = 14400
  canary_node         = "node-b"
}
