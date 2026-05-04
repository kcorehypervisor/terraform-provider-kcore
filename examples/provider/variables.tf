variable "controller_address" {
  type        = string
  description = "kcore controller gRPC address, e.g. controller.example.com:9090"
}

variable "tls_cert_path" {
  type        = string
  description = "Client certificate PEM path"
  default     = ""
}

variable "tls_key_path" {
  type        = string
  description = "Client private key PEM path"
  default     = ""
}

variable "tls_ca_path" {
  type        = string
  description = "CA bundle for verifying the controller (optional if system trust is enough)"
  default     = ""
}
