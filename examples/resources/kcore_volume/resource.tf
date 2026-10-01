resource "kcore_volume" "data" {
  name          = "web-data"
  size_bytes    = 10737418240
  storage_class = "ceph"
  vm            = kcore_vm.app.id
}
