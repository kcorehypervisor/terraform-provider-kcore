# Terraform Provider: kcore

Manage VMs and inventory via the **kcore controller** gRPC API (ISO / controller **0.2.0+**).

Registry: [`registry.terraform.io/kcorehypervisor/kcore`](https://registry.terraform.io/providers/kcorehypervisor/kcore/latest)

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
      version = "0.3.0"
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

### Minimal VM (API 0.2.0 fields)

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

Schema reference pages are generated with [`terraform-plugin-docs`](https://github.com/hashicorp/terraform-plugin-docs) and checked into **`docs/`** for Terraform Registry and GitHub browsing:

- [Provider configuration](docs/index.md)
- [Resource `kcore_vm`](docs/resources/vm.md)
- [Data source `kcore_vm`](docs/data-sources/vm.md)
- [Data source `kcore_node`](docs/data-sources/node.md)
- [Data source `kcore_nodes`](docs/data-sources/nodes.md)

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
