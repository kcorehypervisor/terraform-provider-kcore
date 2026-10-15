resource "kcore_cluster_update" "os" {
  name                    = "release-0-5-0"
  version                 = "0.5.0"
  flake_ref               = "github:kcorehypervisor/kcore"
  flake_rev               = "b7da7c99f385fec391d0a9fb975fe4791d06ae94"
  nixpkgs_rev             = "b40629efe5d6ec48dd1efba650c797ddbd39ace0"
  all_nodes               = true
  strategy                = "one-at-a-time"
  max_unavailable         = 1
  batch_size              = 1
  activation              = "auto"
  update_kind             = "kcore"
  schema_version          = 59
  previous_schema_version = 56
  min_compatible_reader   = 56
  approval                = "manual"
  approve                 = false

  tested_os_pins {
    nixpkgs_rev        = "b40629efe5d6ec48dd1efba650c797ddbd39ace0"
    ceph_rev           = "e7439b6b14ad3cc35d05608ebca9bce01a25f5f8"
    ceph_sha256        = "19y4py973w62z4qb7gzn0chjs67ihxy8rbjb7g71iyk8yz7z5k8z"
    kubernetes_version = "1.37.0"
  }
}
