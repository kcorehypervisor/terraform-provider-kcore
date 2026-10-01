resource "kcore_cluster" "lab" {
  name       = "lab"
  controller = "10.0.0.8:9090"
  certs_dir  = "${path.module}/certs"
}
