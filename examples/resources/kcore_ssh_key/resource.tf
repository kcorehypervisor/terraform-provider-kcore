resource "kcore_ssh_key" "alice" {
  name       = "alice"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyMaterialForDocsOnly alice@workstation"
}
