resource "kcore_vm" "app" {
  name          = "my-app-vm"
  cpu           = 2
  memory_bytes  = 4 * 1024 * 1024 * 1024 # 4 GiB

  # Required for controller API 0.2.0+
  storage_backend    = "filesystem"
  storage_size_bytes = 20 * 1024 * 1024 * 1024 # 20 GiB root volume

  # Image from HTTPS (provide checksum)
  image_url    = "https://images.example.com/base/qcow2/nixos.qcow2"
  image_sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

  # Optional scheduling hints
  # target_dc    = "DC1"
  # target_node  = "192.168.1.10:9091"

  disk {
    name           = "root"
    backend_handle = "/var/lib/kcore/volumes/my-app-vm-root.qcow2"
    bus            = "virtio"
    device         = "disk"
  }

  nic {
    network = "default"
    model   = "virtio"
  }

  desired_state = "running"

  # Optional cloud-init / SSH keys registered with the controller
  # cloud_init_user_data = file("${path.module}/cloud-init.yaml")
  # ssh_key_names        = ["alice-workstation"]
}

# Node-local image (alternative to image_url + image_sha256; do not mix with URL mode):
#   image_path   = "/var/lib/kcore/images/guest.qcow2"
#   image_format = "qcow2"
