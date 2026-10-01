resource "kcore_cluster_update" "os" {
  name      = "os-0.3.0"
  version   = "0.3.0"
  flake_ref = "github:kcorehypervisor/kcore"
  all_nodes = true
  strategy  = "one-at-a-time"
  approval  = "manual"
  approve   = false
}
