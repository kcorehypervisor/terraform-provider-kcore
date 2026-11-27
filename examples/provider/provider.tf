terraform {
  required_providers {
    kcore = {
      source  = "kcorehypervisor/kcore"
      version = "0.8.3"
    }
  }
}

provider "kcore" {
  controller_address = var.controller_address

  # mTLS (recommended). Paths may be created by kcore_cluster in the same apply;
  # the provider loads them at handshake time.
  tls_cert_path = var.tls_cert_path
  tls_key_path  = var.tls_key_path
  tls_ca_path   = var.tls_ca_path

  # Dev only — do not use in production
  # insecure = true
}
