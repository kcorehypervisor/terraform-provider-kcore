resource "kcore_operator" "ci" {
  name       = "ci"
  roles      = ["vm-admin"]
  issue_cert = true
}
