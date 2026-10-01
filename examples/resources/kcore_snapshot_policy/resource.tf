resource "kcore_snapshot_policy" "daily" {
  name            = "daily"
  selector_volume = kcore_volume.data.name
  schedule        = "@daily"
  keep            = 7
}
