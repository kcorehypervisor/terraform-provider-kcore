terraform {
  required_providers {
    kcore = {
      source  = "kcorehypervisor/kcore"
      version = "0.3.0"
    }
  }
}

provider "kcore" {
  controller_address = var.controller_address

  # mTLS (recommended)
  tls_cert_path = var.tls_cert_path
  tls_key_path  = var.tls_key_path
  tls_ca_path   = var.tls_ca_path

  # Dev only — do not use in production
  # insecure = true
}
