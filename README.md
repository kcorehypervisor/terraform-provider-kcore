# Terraform Provider: kcore

Manage a kcore cluster through the same create path as `kctl`: cluster TLS, node install and approval, VMs, networks, security groups, volumes, Ceph, object storage, operators, SSH keys, host updates, and container workloads.

The controller API matches kcore **0.3.0**. Provider **0.3.2** publishes the resources below. [`kcore_cluster`](docs/resources/cluster.md) writes CA, sub-CA, controller, and kctl certificates on the machine running Terraform. Apply it before any node is installed.

Registry: [`registry.terraform.io/kcorehypervisor/kcore`](https://registry.terraform.io/providers/kcorehypervisor/kcore/latest).

## Resources

| Resource | kctl |
|----------|------|
| `kcore_cluster` | `kctl create cluster` (local PKI) |
| `kcore_node_install` | `kctl node install` |
| `kcore_node` | `kctl node approve` / `delete` |
| `kcore_vm` | `kctl create vm` |
| `kcore_workload` | `kctl create container` |
| `kcore_network` | `kctl create network` |
| `kcore_security_group` | `kctl security-group` |
| `kcore_security_group_attachment` | attach a group to a VM or network |
| `kcore_volume` | `kctl create volume` |
| `kcore_volume_snapshot` | `kctl create volume-snapshot` |
| `kcore_snapshot_policy` | `kctl create snapshot-policy` |
| `kcore_disk_layout` | `kctl create disk-layout` |
| `kcore_ceph_cluster` | `kctl create ceph-cluster` |
| `kcore_shared_filesystem` | `kctl create shared-filesystem` |
| `kcore_object_store` | `kctl create object-store` |
| `kcore_object_user` | `kctl create object-user` (secret returned once) |
| `kcore_ssh_key` | `kctl ssh-key add` |
| `kcore_operator` | `kctl operator add` |
| `kcore_cluster_update` | `kctl update-cluster` (destroy cancels) |

Console, drain, migrate, backup, restore, cert rotation, and certificate revoke stay as `kctl` commands. Destroying `kcore_node_install` drops it from state and leaves the installed disk in place.

## Requirements

- Terraform >= 1.0
- A reachable kcore controller implementing the API from [`proto/controller.proto`](proto/controller.proto)
- For production, TLS client certificates (**mTLS**) matching how your controller trusts nodes/clients

## Using the provider

Declare the provider and pass connection settings (environment variables are supported—see [docs/index.md](docs/index.md)).

```hcl
terraform {
  required_providers {
    kcore = {
      source  = "kcorehypervisor/kcore"
      version = "0.3.2"
    }
  }
}

provider "kcore" {
  controller_address = "controller.example.com:9090"
  tls_cert_path      = "/path/to/client.crt"
  tls_key_path       = "/path/to/client.key"
  tls_ca_path        = "/path/to/ca.crt"
}
```

Environment defaults:

| Variable | Maps to |
|----------|---------|
| `KCORE_CONTROLLER_ADDRESS` | `controller_address` |
| `KCORE_TLS_CERT` | `tls_cert_path` |
| `KCORE_TLS_KEY` | `tls_key_path` |
| `KCORE_TLS_CA` | `tls_ca_path` |
| `KCORE_INSECURE` | `insecure` (development only) |

## Examples

Runnable snippets live under [`examples/`](examples/):

| Path | Purpose |
|------|---------|
| [`examples/provider/`](examples/provider/) | `terraform` block + `provider "kcore"` |
| [`examples/resources/kcore_vm/`](examples/resources/kcore_vm/) | Create a VM with HTTPS image + checksum |
| [`examples/data-sources/kcore_vm/`](examples/data-sources/kcore_vm/) | Read an existing VM |
| [`examples/data-sources/kcore_node/`](examples/data-sources/kcore_node/) | Read a hypervisor node |
| [`examples/data-sources/kcore_nodes/`](examples/data-sources/kcore_nodes/) | List all nodes |

### Minimal VM

`storage_backend` and `storage_size_bytes` are required. Provide either **`image_url` + `image_sha256`** or **`image_path` + `image_format`**, not both.

```hcl
resource "kcore_vm" "web" {
  name          = "web-1"
  cpu           = 2
  memory_bytes  = 4294967296

  storage_backend    = "filesystem"
  storage_size_bytes = 21474836480

  image_url    = "https://images.example.com/base.qcow2"
  image_sha256 = "<64-char hex digest>"

  disk {
    name           = "root"
    backend_handle = "/var/lib/kcore/volumes/web-1.qcow2"
    bus            = "virtio"
  }

  nic {
    network = "default"
    model   = "virtio"
  }
}
```

### Data sources

```hcl
data "kcore_nodes" "all" {}

data "kcore_node" "worker" {
  id = "node-id-from-controller"
}

data "kcore_vm" "inspect" {
  id = kcore_vm.web.id
}
```

## Generated documentation

Schema reference pages are generated with [`terraform-plugin-docs`](https://github.com/hashicorp/terraform-plugin-docs) and checked into [`docs/`](docs/index.md). Resource pages live under [`docs/resources/`](docs/resources/).

### Regenerate `docs/`

Requires Go **1.24+** (uses `go tool`), Terraform CLI available or downloadable by the generator:

```bash
make docs
# equivalent:
go tool tfplugindocs generate --rendered-provider-name kcore
```

Commit the updated `docs/` when provider schemas change.

## Development

```bash
go build .
go test ./...
```

The provider binary is typically named `terraform-provider-kcore`; Terraform 1.0+ discovers development builds via `.terraformrc` dev overrides.

## License

See the repository license file (if present).
