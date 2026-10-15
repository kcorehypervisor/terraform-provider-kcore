package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func TestKubernetesSpecIncludesSharedProvisioner(t *testing.T) {
	spec := kubernetesSpecFrom(t, map[string]interface{}{
		"name":                      "lab",
		"cluster_cidr":              "10.244.0.0/16",
		"service_cidr":              "10.96.0.0/12",
		"api_server_endpoint":       "192.168.40.220:6443",
		"schedule_on_control_plane": true,
		"control_plane_node": []interface{}{
			map[string]interface{}{
				"node_id":           "node-107",
				"advertise_address": "192.168.40.107",
				"iface":             "enp6s0",
				"initial_cluster":   true,
			},
		},
		"cilium": []interface{}{
			map[string]interface{}{
				"mode":                    "native-routing",
				"mtu":                     1500,
				"tunnel":                  "disabled",
				"auto_direct_node_routes": true,
				"load_balancer_ip_pool":   []interface{}{"192.168.40.220/29"},
			},
		},
		"local_provisioner": []interface{}{
			map[string]interface{}{
				"enabled":            true,
				"mode":               "shared",
				"storage_class_name": "local-path",
				"volume": []interface{}{
					map[string]interface{}{
						"name":          "kcore-kdata",
						"node_id":       "node-107",
						"storage_class": "lvm",
						"size_bytes":    "53687091200",
						"fs_type":       "ext4",
						"role":          "filesystem",
					},
				},
			},
		},
	})
	if !spec.GetScheduleOnControlPlane() || spec.GetCni() != "cilium" || spec.GetClusterDns() != "10.96.0.10" {
		t.Fatalf("spec defaults = %+v", spec)
	}
	if spec.GetCiliumConfig().GetTunnel() != "disabled" || len(spec.GetCiliumConfig().GetLoadBalancerIpPools()) != 1 {
		t.Fatalf("cilium = %+v", spec.GetCiliumConfig())
	}
	if !spec.GetControlPlaneNodes()[0].GetInitialCluster() || spec.GetControlPlaneNodes()[0].GetJoinExisting() {
		t.Fatalf("control plane = %+v", spec.GetControlPlaneNodes()[0])
	}
	local := spec.GetLocalProvisioner()
	if local == nil || !local.GetEnabled() || local.GetMode() != "shared" || local.GetStorageClassName() != "local-path" {
		t.Fatalf("provisioner = %+v", local)
	}
	if len(local.GetVolumes()) != 1 || local.Volumes[0].GetSizeBytes() != 53_687_091_200 || local.Volumes[0].GetNodeId() != "node-107" {
		t.Fatalf("volume = %+v", local.GetVolumes())
	}
}

func TestKubernetesSpecIncludesLvmPool(t *testing.T) {
	spec := kubernetesSpecFrom(t, map[string]interface{}{
		"name":                "lab",
		"cluster_cidr":        "10.245.0.0/16",
		"service_cidr":        "10.112.0.0/12",
		"api_server_endpoint": "192.168.40.201:7443",
		"control_plane_node": []interface{}{
			map[string]interface{}{
				"node_id":           "node-105",
				"advertise_address": "192.168.40.105",
			},
		},
		"high_availability": []interface{}{
			map[string]interface{}{
				"enabled":       true,
				"vip_address":   "192.168.40.201",
				"vip_interface": "enp6s0",
			},
		},
		"local_provisioner": []interface{}{
			map[string]interface{}{
				"enabled":            true,
				"mode":               "lvm",
				"storage_class_name": "kcore-lvm",
				"volume": []interface{}{
					map[string]interface{}{
						"name":          "kcore-kdata",
						"node_id":       "node-105",
						"storage_class": "lvm",
						"size_bytes":    "53687091200",
						"role":          "pool",
						"volume_group":  "vg_kcore",
					},
				},
			},
		},
	})
	if spec.GetHighAvailability().GetMethod() != "keepalived" || spec.GetHighAvailability().GetVipAddress() != "192.168.40.201" {
		t.Fatalf("ha = %+v", spec.GetHighAvailability())
	}
	local := spec.GetLocalProvisioner()
	if local.GetMode() != "lvm" || local.Volumes[0].GetRole() != "pool" || local.Volumes[0].GetVolumeGroup() != "vg_kcore" {
		t.Fatalf("lvm provisioner = %+v", local)
	}
	if spec.GetControlPlaneNodes()[0].GetIface() != "enp6s0" {
		t.Fatalf("iface = %s", spec.GetControlPlaneNodes()[0].GetIface())
	}
}

func TestKubernetesScaleDeltaAddsAndRemovesMembers(t *testing.T) {
	oldCP := []map[string]interface{}{{"node_id": "node-107", "advertise_address": "192.168.40.107", "iface": "enp6s0"}}
	newCP := []map[string]interface{}{
		{"node_id": "node-107", "advertise_address": "192.168.40.107", "iface": "enp6s0"},
		{"node_id": "node-119", "advertise_address": "192.168.40.119", "iface": "enp6s0"},
	}
	oldWorkers := []map[string]interface{}{{"node_id": "node-105", "advertise_address": "192.168.40.105", "iface": "enp6s0"}}
	add, remove := kubernetesScaleDelta(oldCP, newCP, oldWorkers, nil)
	if len(add) != 1 || add[0].GetNodeId() != "node-119" || add[0].GetRole() != "control-plane" || add[0].GetInitialCluster() || !add[0].GetJoinExisting() {
		t.Fatalf("add = %+v", add)
	}
	if len(remove) != 1 || remove[0] != "node-105" {
		t.Fatalf("remove = %+v", remove)
	}
}

func TestKubernetesSchemaRejectsUnknownProvisionerMode(t *testing.T) {
	resource := resourceKubernetesCluster()
	provisioner := resource.Schema["local_provisioner"].Elem.(*schema.Resource)
	if _, errs := provisioner.Schema["mode"].ValidateFunc("ceph", "mode"); len(errs) == 0 {
		t.Fatal("ceph is not a local provisioner mode")
	}
	if _, errs := provisioner.Schema["mode"].ValidateFunc("lvm", "mode"); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func kubernetesSpecFrom(t *testing.T, raw map[string]interface{}) *pb.KubernetesClusterSpec {
	t.Helper()
	raw["cluster_dns"] = "10.96.0.10"
	raw["cni"] = "cilium"
	d := schema.TestResourceDataRaw(t, resourceKubernetesCluster().Schema, raw)
	spec, err := kubernetesSpec(d)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
