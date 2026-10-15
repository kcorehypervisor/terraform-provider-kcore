package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

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
		"kcore_kubernetes_cluster",
	}
	for _, name := range want {
		if _, ok := p.ResourcesMap[name]; !ok {
			t.Errorf("missing resource %s", name)
		}
	}
}

func TestClusterUpdateSpecMatchesReleaseManifest(t *testing.T) {
	r := resourceClusterUpdate()
	d := schema.TestResourceDataRaw(t, r.Schema, map[string]interface{}{
		"name":                    "release-0-5-0",
		"version":                 "0.5.0",
		"flake_ref":               "github:kcorehypervisor/kcore",
		"flake_rev":               "b7da7c99f385fec391d0a9fb975fe4791d06ae94",
		"nixpkgs_rev":             "b40629efe5d6ec48dd1efba650c797ddbd39ace0",
		"all_nodes":               true,
		"strategy":                "one-at-a-time",
		"max_unavailable":         1,
		"batch_size":              1,
		"activation":              "auto",
		"update_kind":             "kcore",
		"schema_version":          59,
		"previous_schema_version": 56,
		"min_compatible_reader":   56,
		"approval":                "manual",
		"tested_os_pins": []interface{}{
			map[string]interface{}{
				"nixpkgs_rev":        "b40629efe5d6ec48dd1efba650c797ddbd39ace0",
				"ceph_rev":           "e7439b6b14ad3cc35d05608ebca9bce01a25f5f8",
				"ceph_sha256":        "19y4py973w62z4qb7gzn0chjs67ihxy8rbjb7g71iyk8yz7z5k8z",
				"kubernetes_version": "1.37.0",
			},
		},
	})
	spec := clusterUpdateSpec(d)
	if spec.GetTarget().GetVersion() != "0.5.0" || spec.GetTarget().GetFlakeRev() != "b7da7c99f385fec391d0a9fb975fe4791d06ae94" {
		t.Fatalf("target = %+v", spec.GetTarget())
	}
	if spec.GetStrategy().GetType() != pb.ClusterUpdateStrategyType_CLUSTER_UPDATE_STRATEGY_ONE_AT_A_TIME ||
		spec.GetStrategy().GetMaxUnavailable() != 1 || spec.GetStrategy().GetBatchSize() != 1 {
		t.Fatalf("strategy = %+v", spec.GetStrategy())
	}
	if spec.GetActivationMode() != pb.ClusterUpdateActivationMode_CLUSTER_UPDATE_ACTIVATION_AUTO {
		t.Fatalf("activation = %s", spec.GetActivationMode())
	}
	if spec.GetUpdateKind() != "kcore" || spec.GetSchemaVersion() != 59 ||
		spec.GetPreviousSchemaVersion() != 56 || spec.GetMinCompatibleReader() != 56 {
		t.Fatalf("release fields = kind %s schema %d previous %d reader %d",
			spec.GetUpdateKind(), spec.GetSchemaVersion(), spec.GetPreviousSchemaVersion(), spec.GetMinCompatibleReader())
	}
	pins := spec.GetTestedOsPins()
	if len(pins) != 1 || pins[0].GetKubernetesVersion() != "1.37.0" || pins[0].GetCephRev() == "" {
		t.Fatalf("pins = %+v", pins)
	}
}

func TestClusterUpdateRejectsUnreleasedStrategies(t *testing.T) {
	r := resourceClusterUpdate()
	for _, name := range []string{"canary", "batch", "per-dc"} {
		_, errs := r.Schema["strategy"].ValidateFunc(name, "strategy")
		if len(errs) == 0 {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, errs := r.Schema["strategy"].ValidateFunc("one-at-a-time", "strategy"); len(errs) != 0 {
		t.Fatal(errs)
	}
	if _, errs := r.Schema["activation"].ValidateFunc("", "activation"); len(errs) != 0 {
		t.Fatal(errs)
	}
	if _, errs := r.Schema["activation"].ValidateFunc("reboot", "activation"); len(errs) == 0 {
		t.Fatal("reboot is a kctl command, not an activation mode")
	}
}
