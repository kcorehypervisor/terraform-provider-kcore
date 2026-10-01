package provider

import "testing"

func TestResourcesCoverKctlCreateSurface(t *testing.T) {
	p := New()
	want := []string{
		"kcore_cluster",
		"kcore_node",
		"kcore_vm",
		"kcore_network",
		"kcore_security_group",
		"kcore_security_group_attachment",
		"kcore_volume",
		"kcore_volume_snapshot",
		"kcore_snapshot_policy",
		"kcore_disk_layout",
		"kcore_ceph_cluster",
		"kcore_shared_filesystem",
		"kcore_object_store",
		"kcore_object_user",
		"kcore_ssh_key",
		"kcore_operator",
		"kcore_cluster_update",
		"kcore_workload",
	}
	for _, name := range want {
		if _, ok := p.ResourcesMap[name]; !ok {
			t.Errorf("missing resource %s", name)
		}
	}
}
