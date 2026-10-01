resource "kcore_security_group" "web" {
  name        = "web"
  description = "public web"

  rule {
    protocol    = "tcp"
    host_port   = 443
    target_port = 443
    source_cidr = "0.0.0.0/0"
  }
}
