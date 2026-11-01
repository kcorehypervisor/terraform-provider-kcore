data "kcore_volumes" "web" {
  vm = "web-01"
}

output "web_volumes" {
  value = [for v in data.kcore_volumes.web.volumes : v.name]
}
