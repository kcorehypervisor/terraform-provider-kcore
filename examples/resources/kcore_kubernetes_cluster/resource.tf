resource "kcore_kubernetes_cluster" "lab" {
  name                      = "lab"
  cluster_cidr              = "10.244.0.0/16"
  service_cidr              = "10.96.0.0/12"
  api_server_endpoint       = "192.168.40.220:6443"
  schedule_on_control_plane = true

  control_plane_node {
    node_id           = "node-107"
    advertise_address = "192.168.40.107"
    initial_cluster   = true
  }

  cilium {
    load_balancer_ip_pool = ["192.168.40.220/29"]
  }

  # shared: format one volume and install the local-path provisioner.
  local_provisioner {
    enabled            = true
    mode               = "shared"
    storage_class_name = "local-path"

    volume {
      name          = "kcore-kdata"
      node_id       = "node-107"
      storage_class = "lvm"
      size_bytes    = "53687091200"
      fs_type       = "ext4"
      role          = "filesystem"
    }
  }
}

resource "kcore_kubernetes_cluster" "pools" {
  name                = "pools"
  cluster_cidr        = "10.245.0.0/16"
  service_cidr        = "10.112.0.0/12"
  api_server_endpoint = "192.168.40.201:7443"

  control_plane_node {
    node_id           = "node-105"
    advertise_address = "192.168.40.105"
    initial_cluster   = true
  }

  high_availability {
    enabled       = true
    vip_address   = "192.168.40.201"
    vip_interface = "enp6s0"
  }

  # lvm: one thin pool on every member. kcore does not install a volume provisioner.
  local_provisioner {
    enabled            = true
    mode               = "lvm"
    storage_class_name = "kcore-lvm"

    volume {
      name          = "kcore-kdata"
      node_id       = "node-105"
      storage_class = "lvm"
      size_bytes    = "53687091200"
      role          = "pool"
      volume_group  = "vg_kcore"
    }
  }
}
