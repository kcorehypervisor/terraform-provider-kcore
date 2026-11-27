package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func TestCephClusterOldConfigLeavesNewFieldsEmpty(t *testing.T) {
	d := schema.TestResourceDataRaw(t, resourceCephCluster().Schema, map[string]interface{}{
		"name":           "lab",
		"public_network": "10.0.0.0/24",
		"node":           []interface{}{map[string]interface{}{"node_id": "n1", "osd_device": "/dev/sdb"}},
	})
	spec := cephFromState(d).Spec
	if spec.Size != 3 || spec.MinSize != 2 {
		t.Fatalf("size defaults: %d/%d", spec.Size, spec.MinSize)
	}
	if spec.Recovery != nil || spec.External != nil || len(spec.Pools) != 0 || spec.MsMode != "" || spec.CachePolicy != "" {
		t.Fatalf("an old config must not send lifecycle fields: %+v", spec)
	}
	if err := cephValidate(spec); err != nil {
		t.Fatal(err)
	}
}

func TestCephClusterLifecycleFields(t *testing.T) {
	d := schema.TestResourceDataRaw(t, resourceCephCluster().Schema, map[string]interface{}{
		"name":           "lab",
		"public_network": "10.0.0.0/24",
		"ms_mode":        "secure",
		"image_features": "layering,exclusive-lock",
		"cache_policy":   "writethrough",
		"lost_members":   []interface{}{"n4"},
		"node": []interface{}{
			map[string]interface{}{"node_id": "n1", "osd_device": "/dev/sdb", "role": "full", "weight": 1.5, "device_class": "ssd"},
			map[string]interface{}{"node_id": "n2", "role": "mon"},
		},
		"pool": []interface{}{
			map[string]interface{}{"name": "kcore-vms", "application": "rbd", "size": 3, "min_size": 2, "crush_rule": "ssd", "quota_bytes": 1 << 40},
		},
		"recovery": []interface{}{map[string]interface{}{"max_backfills": 1, "scrub_begin_hour": 1, "nearfull_ratio_bp": 8500}},
	})
	spec := cephFromState(d).Spec
	if spec.MsMode != "secure" || spec.CachePolicy != "writethrough" || spec.ImageFeatures != "layering,exclusive-lock" {
		t.Fatalf("scalars: %+v", spec)
	}
	if len(spec.LostMembers) != 1 || spec.LostMembers[0] != "n4" {
		t.Fatalf("lost members: %v", spec.LostMembers)
	}
	if spec.Nodes[0].Weight != 1.5 || spec.Nodes[0].DeviceClass != "ssd" || spec.Nodes[1].Role != "mon" {
		t.Fatalf("nodes: %+v", spec.Nodes)
	}
	if spec.Pools[0].CrushRule != "ssd" || spec.Pools[0].QuotaBytes != 1<<40 {
		t.Fatalf("pools: %+v", spec.Pools)
	}
	r := spec.Recovery
	if r == nil || r.MaxBackfills != 1 || r.ScrubBeginHour != 1 || r.ScrubEndHour != -1 || r.NearfullRatioBp != 8500 {
		t.Fatalf("recovery: %+v", r)
	}
}

func TestCephClusterValidate(t *testing.T) {
	ext := &pb.CephExternalSpec{MonHosts: "10.0.0.1", Pool: "rbd"}
	if err := cephValidate(&pb.CephClusterSpec{External: ext, Nodes: []*pb.CephClusterNodeSpec{{NodeId: "n"}}}); err == nil {
		t.Fatal("external with nodes must be refused")
	}
	if err := cephValidate(&pb.CephClusterSpec{}); err == nil {
		t.Fatal("no nodes and no external must be refused")
	}
	if err := cephValidate(&pb.CephClusterSpec{External: ext}); err != nil {
		t.Fatal(err)
	}
	erasure := &pb.CephClusterSpec{External: ext, Pools: []*pb.CephPoolSpec{{Name: "p", PoolType: "erasure"}}}
	if err := cephValidate(erasure); err == nil {
		t.Fatal("erasure pool without a profile must be refused")
	}
}

func TestCephHealthState(t *testing.T) {
	got := cephHealthState(&pb.CephTypedHealth{
		HealthStatus: "HEALTH_WARN",
		MonQuorum:    []string{"a", "b"},
		PgsByState:   []*pb.CephPgStateCount{{State: "active+clean", Count: 31}},
		Noout:        true,
		Checks:       []*pb.CephHealthCheck{{Severity: "HEALTH_WARN", Type: "OSDMAP_FLAGS", Summary: "noout flag(s) set"}},
	})
	row := got[0].(map[string]interface{})
	if row["status"] != "HEALTH_WARN" || row["noout"] != true || row["pgs_by_state"].(map[string]interface{})["active+clean"] != 31 {
		t.Fatalf("health: %+v", row)
	}
	if cephHealthState(nil) != nil {
		t.Fatal("nil health must map to nil")
	}
	d := schema.TestResourceDataRaw(t, resourceCephCluster().Schema, map[string]interface{}{"name": "x", "public_network": "p"})
	if err := d.Set("health", got); err != nil {
		t.Fatal(err)
	}
}
