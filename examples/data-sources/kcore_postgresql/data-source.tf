data "kcore_postgresql" "app" {
  name = "app"
}

output "database_node" {
  value = data.kcore_postgresql.app.node_id
}
