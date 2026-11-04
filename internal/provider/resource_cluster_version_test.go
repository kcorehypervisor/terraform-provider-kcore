package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

type fakeVersionController struct {
	pb.UnimplementedControllerServer
	set *pb.SetClusterVersionRequest
}

func (f *fakeVersionController) SetClusterVersion(_ context.Context, req *pb.SetClusterVersionRequest) (*pb.SetClusterVersionResponse, error) {
	f.set = req
	return &pb.SetClusterVersionResponse{Version: req.Version, UpdateName: "version-v0-7-0", CanaryNodeId: "node-b", CanarySoakSeconds: req.CanarySoakSeconds}, nil
}

func (f *fakeVersionController) GetClusterVersion(context.Context, *pb.GetClusterVersionRequest) (*pb.GetClusterVersionResponse, error) {
	return &pb.GetClusterVersionResponse{
		Version:    "v0.7.0",
		FlakeRev:   "0123456789abcdef0123456789abcdef01234567",
		UpdateName: "version-v0-7-0",
		Phase:      pb.ClusterUpdatePhase_CLUSTER_UPDATE_PHASE_ROLLING_OUT,
	}, nil
}

func TestClusterVersionSetsTheTagWithItsCanary(t *testing.T) {
	fake := &fakeVersionController{}
	client := newControllerClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourceClusterVersion().Schema, map[string]interface{}{
		"version":             "0.7.0",
		"canary_soak_seconds": 7200,
		"canary_node":         "node-b",
	})
	if diags := resourceClusterVersionSet(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	if fake.set.Version != "v0.7.0" || fake.set.CanarySoakSeconds != 7200 || fake.set.CanaryNodeId != "node-b" || fake.set.NoCanary {
		t.Fatalf("request = %+v", fake.set)
	}
	if d.Id() != clusterVersionID || d.Get("canary_node_id") != "node-b" || d.Get("update_name") != "version-v0-7-0" {
		t.Fatalf("state = %v", d.State())
	}
	if d.Get("phase") != "CLUSTER_UPDATE_PHASE_ROLLING_OUT" {
		t.Fatalf("phase = %v", d.Get("phase"))
	}
}

func TestClusterUpdateCarriesTheCanaryStrategy(t *testing.T) {
	d := schema.TestResourceDataRaw(t, resourceClusterUpdate().Schema, map[string]interface{}{
		"name":                "release-0-7-0",
		"version":             "0.7.0",
		"flake_ref":           "github:kcorehypervisor/kcore",
		"all_nodes":           true,
		"strategy":            "canary",
		"canary_soak_seconds": 1800,
		"canary_node":         "node-b",
	})
	got := clusterUpdateSpec(d).Strategy
	if got.Type != pb.ClusterUpdateStrategyType_CLUSTER_UPDATE_STRATEGY_CANARY || got.CanarySoakSeconds != 1800 || got.CanaryNodeId != "node-b" {
		t.Fatalf("strategy = %+v", got)
	}
}

func TestReleaseTagAddsTheV(t *testing.T) {
	for in, want := range map[string]string{"0.7.0": "v0.7.0", "v0.7.0": "v0.7.0", " ": ""} {
		if got := releaseTag(in); got != want {
			t.Errorf("releaseTag(%q) = %q, want %q", in, got, want)
		}
	}
}
