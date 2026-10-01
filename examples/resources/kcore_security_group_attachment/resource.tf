resource "kcore_security_group_attachment" "web_vm" {
  security_group = kcore_security_group.web.name
  target_kind    = "vm"
  target_id      = kcore_vm.app.id
}
