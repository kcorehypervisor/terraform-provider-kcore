resource "kcore_waf_policy" "crs" {
  name = "crs-default"
  spec = jsonencode({
    engine = "detect" # logs only; switch to block after reading the WAF events
    crs = {
      paranoiaLevel           = 1
      inboundAnomalyThreshold = 5
    }
    requestBodyLimitBytes = 1048576
  })
}

resource "kcore_ingress_gateway" "edge" {
  name = "edge"
  spec = jsonencode({
    nodes = ["node-105"]
    listeners = [{
      address = "203.0.113.10"
      ports   = [80, 443]
      http3   = true
    }]
    tls = {
      acme = {
        email = "ops@example.com"
        ca    = "letsencrypt-staging" # production rate-limits failed validations
      }
    }
    defaultWafPolicy = kcore_waf_policy.crs.name
    accessLog        = true
  })

  timeouts {
    create = "30m"
  }
}

resource "kcore_ingress_route" "shop" {
  name    = "shop"
  gateway = kcore_ingress_gateway.edge.name
  spec = jsonencode({
    hosts = ["shop.example.com"]
    paths = [{
      prefix = "/"
      backends = [
        { kind = "vm", name = "shop-web-1", port = 8080 },
        { kind = "external", address = "192.168.40.50:8443" },
      ]
      loadBalancing = { policy = "least_conn" }
      healthCheck   = { path = "/healthz", interval = "10s", expectStatus = 200 }
    }]
    redirectHttp = true
  })
}
