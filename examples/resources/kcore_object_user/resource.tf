resource "kcore_object_user" "app" {
  name  = "app"
  store = kcore_object_store.s3.name
}
