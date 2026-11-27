resource "kcore_ceph_cluster" "lab" {
  name            = "lab"
  public_network  = "10.0.0.0/24"
  cluster_network = "10.20.0.0/24"
  size            = 3
  min_size        = 2
  encrypt_osds    = true

  # Optional. Check kcore docs/ceph-lifecycle.md for which of these the
  # reconciler acts on in your release; older controllers ignore them.
  ms_mode        = "secure"
  image_features = "layering,exclusive-lock"
  cache_policy   = "writethrough"
  failure_domain = "host"
  # stretch = true  # only with failure_domain = "room"
  # mirroring {
  #   enabled      = true
  #   mode         = "pool"
  #   peer_cluster = "other-san"
  # }

  pool {
    name        = "kcore-vms"
    application = "rbd"
    size        = 3
    min_size    = 2
  }

  recovery {
    max_backfills       = 1
    recovery_max_active = 1
    scrub_begin_hour    = 1
    scrub_end_hour      = 6
    nearfull_ratio_bp   = 8500
  }

  node {
    node_id       = "kcore-node-10.0.0.8"
    mon_addr      = "10.0.0.8:6789"
    cluster_addr  = "10.20.0.8"
    public_iface  = "eth1"
    cluster_iface = "eth2"
    osd_device    = "/dev/disk/by-id/ata-EXAMPLE_1"
    # rack          = "rack-a"
    # room          = "room-1"
  }

  node {
    node_id       = "kcore-node-10.0.0.12"
    mon_addr      = "10.0.0.12:6789"
    cluster_addr  = "10.20.0.12"
    public_iface  = "eth1"
    cluster_iface = "eth2"
    osd_device    = "/dev/disk/by-id/ata-EXAMPLE_2"
  }

  node {
    node_id       = "kcore-node-10.0.0.13"
    mon_addr      = "10.0.0.13:6789"
    cluster_addr  = "10.20.0.13"
    public_iface  = "eth1"
    cluster_iface = "eth2"
    osd_device    = "/dev/disk/by-id/ata-EXAMPLE_3"
  }

  # A host that is gone for good: keep it out of `node` and list it here.
  # lost_members = ["kcore-node-10.0.0.14"]
}

output "ceph_health" {
  value = kcore_ceph_cluster.lab.health
}
