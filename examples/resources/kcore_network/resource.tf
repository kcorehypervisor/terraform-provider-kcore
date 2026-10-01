resource "kcore_network" "app" {
  name               = "app"
  network_type       = "nat"
  gateway_ip         = "10.20.0.1"
  internal_netmask   = "255.255.255.0"
  enable_outbound_nat = true
  allowed_tcp_ports  = [22, 80, 443]
}
