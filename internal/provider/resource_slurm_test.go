package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func slurmSpecFrom(t *testing.T, raw map[string]interface{}) *pb.SlurmClusterSpec {
	t.Helper()
	d := schema.TestResourceDataRaw(t, resourceSlurmCluster().Schema, raw)
	return slurmClusterSpec(d)
}

func TestSlurmSpecMapsEveryBlock(t *testing.T) {
	spec := slurmSpecFrom(t, map[string]interface{}{
		"name": "hpc-a",
		"controller": []interface{}{
			map[string]interface{}{"node_id": "node-101", "address": "10.0.0.11", "iface": "enp6s0"},
		},
		"backup_controller": []interface{}{
			map[string]interface{}{"node_id": "node-104", "address": "10.0.0.14", "iface": "enp6s0"},
		},
		"login_node_ids": []interface{}{"node-104"},
		"accounting":     []interface{}{map[string]interface{}{"enabled": true, "node_id": "node-104"}},
		"compute": []interface{}{
			map[string]interface{}{
				"node_id": "node-102", "address": "10.0.0.12", "iface": "enp6s0",
				"partitions": []interface{}{"batch"}, "reserve_cores": 2, "reserve_memory_mb": 4096,
			},
			map[string]interface{}{
				"node_id": "node-103", "address": "10.0.0.13", "iface": "enp6s0",
				"partitions": []interface{}{"batch", "gpu"}, "gres": "auto",
			},
		},
		"partition": []interface{}{
			map[string]interface{}{"name": "batch", "default": true, "max_time": "INFINITE", "state": "UP"},
			map[string]interface{}{"name": "gpu"},
		},
		"user": []interface{}{
			map[string]interface{}{"name": "alice", "uid": 2001, "ssh_keys": []interface{}{"ssh-ed25519 AAAA"}},
		},
		"gpu_driver": true,
	})
	if spec.GetClusterName() != "hpc-a" {
		t.Fatalf("cluster name = %q", spec.GetClusterName())
	}
	if spec.GetController().GetNodeId() != "node-101" || spec.GetBackupController().GetAddress() != "10.0.0.14" {
		t.Fatalf("controllers = %+v / %+v", spec.GetController(), spec.GetBackupController())
	}
	if !spec.GetAccounting().GetEnabled() || spec.GetAccounting().GetNodeId() != "node-104" {
		t.Fatalf("accounting = %+v", spec.GetAccounting())
	}
	if len(spec.GetCompute()) != 2 {
		t.Fatalf("compute = %+v", spec.GetCompute())
	}
	first, second := spec.Compute[0], spec.Compute[1]
	if first.GetReserve().GetCores() != 2 || first.GetReserve().GetMemoryMb() != 4096 || first.GetGres() != "none" {
		t.Fatalf("first compute = %+v", first)
	}
	if second.GetGres() != "auto" || len(second.GetPartitions()) != 2 {
		t.Fatalf("second compute = %+v", second)
	}
	if len(spec.GetPartitions()) != 2 || !spec.Partitions[0].GetDefault() || spec.Partitions[0].GetMaxTime() != "INFINITE" {
		t.Fatalf("partitions = %+v", spec.GetPartitions())
	}
	if len(spec.GetUsers()) != 1 || spec.Users[0].GetUid() != 2001 || len(spec.Users[0].GetSshKeys()) != 1 {
		t.Fatalf("users = %+v", spec.GetUsers())
	}
	if !spec.GetGpuDriver() || spec.GetAllowControllerHost() {
		t.Fatalf("flags = %+v", spec)
	}
	if got := spec.GetLogin().GetNodeIds(); len(got) != 1 || got[0] != "node-104" {
		t.Fatalf("login = %v", got)
	}
}

func TestSlurmSpecDefaultsToNfsExports(t *testing.T) {
	spec := slurmSpecFrom(t, map[string]interface{}{
		"name":       "hpc-a",
		"controller": []interface{}{map[string]interface{}{"node_id": "n1", "address": "10.0.0.1", "iface": "e0"}},
		"compute":    []interface{}{map[string]interface{}{"node_id": "n2", "address": "10.0.0.2", "iface": "e0"}},
		"partition":  []interface{}{map[string]interface{}{"name": "batch"}},
	})
	storage := spec.GetStorage()
	if storage.GetMode() != "nfs" || len(storage.GetExports()) != 2 || storage.Exports[0] != "/home" {
		t.Fatalf("storage = %+v", storage)
	}
	if spec.GetAccounting().GetEnabled() {
		t.Fatal("accounting should be off by default")
	}
}

func TestSlurmStorageNoneKeepsNoExports(t *testing.T) {
	spec := slurmSpecFrom(t, map[string]interface{}{
		"name":       "hpc-a",
		"controller": []interface{}{map[string]interface{}{"node_id": "n1", "address": "10.0.0.1", "iface": "e0"}},
		"compute":    []interface{}{map[string]interface{}{"node_id": "n2", "address": "10.0.0.2", "iface": "e0"}},
		"partition":  []interface{}{map[string]interface{}{"name": "batch"}},
		"storage":    []interface{}{map[string]interface{}{"mode": "none"}},
	})
	if spec.GetStorage().GetMode() != "none" || len(spec.GetStorage().GetExports()) != 0 {
		t.Fatalf("storage = %+v", spec.GetStorage())
	}
}

func TestSlurmPartitionAndGresChecks(t *testing.T) {
	spec := &pb.SlurmClusterSpec{
		Partitions: []*pb.SlurmPartitionSpec{{Name: "batch"}},
		Compute:    []*pb.SlurmComputeSpec{{NodeId: "n2", Partitions: []string{"gpu"}, Gres: "none"}},
	}
	if err := validateSlurmPartitions(spec); err == nil {
		t.Fatal("an undeclared partition must be refused")
	}
	spec.Compute[0].Partitions = []string{"batch"}
	spec.Compute[0].Gres = "auto"
	if err := validateSlurmPartitions(spec); err == nil {
		t.Fatal("gres auto without gpu_driver must be refused")
	}
	spec.GpuDriver = true
	if err := validateSlurmPartitions(spec); err != nil {
		t.Fatal(err)
	}
}

func TestSlurmGresRejectsUnknownValue(t *testing.T) {
	compute := resourceSlurmCluster().Schema["compute"].Elem.(*schema.Resource)
	if _, errs := compute.Schema["gres"].ValidateFunc("some", "gres"); len(errs) == 0 {
		t.Fatal("gres must be none or auto")
	}
}
