resource "kcore_disk_layout" "worker" {
  name    = "worker-nvme"
  node_id = kcore_node.worker.node_id
  layout_nix = <<-NIX
    disk.nvme0 = {
      device = "/dev/nvme0n1";
      type = "disk";
      content = {
        type = "gpt";
      };
    };
  NIX
}
