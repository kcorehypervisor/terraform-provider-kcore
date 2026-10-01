resource "kcore_volume_snapshot" "data" {
  volume = kcore_volume.data.name
  name   = "web-data-1"
}
