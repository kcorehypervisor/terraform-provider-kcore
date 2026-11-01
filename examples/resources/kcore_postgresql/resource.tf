resource "kcore_postgresql" "app" {
  name        = "app"
  package     = "postgresql_16"
  port        = 5432
  target_node = "node-105"
}
