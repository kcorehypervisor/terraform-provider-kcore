resource "kcore_workload" "nginx" {
  name  = "nginx"
  image = "docker.io/library/nginx:1.27"
  ports = ["80"]

  env = {
    NGINX_ENTRYPOINT_QUIET_LOGS = "1"
  }
}
