resource "kcore_slurm_cluster" "hpc" {
  name = "hpc-a"

  controller {
    node_id = "node-101"
    address = "10.0.0.11"
    iface   = "enp6s0"
  }

  login_node_ids = ["node-101"]

  compute {
    node_id    = "node-102"
    address    = "10.0.0.12"
    iface      = "enp6s0"
    partitions = ["batch"]
  }

  compute {
    node_id    = "node-103"
    address    = "10.0.0.13"
    iface      = "enp6s0"
    partitions = ["batch"]
  }

  partition {
    name     = "batch"
    default  = true
    max_time = "INFINITE"
    state    = "UP"
  }

  user {
    name     = "alice"
    uid      = 2001
    ssh_keys = ["ssh-ed25519 AAAA... alice@example"]
  }

  storage {
    mode    = "nfs"
    exports = ["/home", "/scratch"]
  }

  timeouts {
    create = "90m"
  }
}

data "kcore_slurm_cluster" "hpc" {
  name = kcore_slurm_cluster.hpc.name
}

output "hpc_phase" {
  value = data.kcore_slurm_cluster.hpc.phase
}
