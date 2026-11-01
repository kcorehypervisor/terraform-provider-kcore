package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

type fakeListController struct {
	pb.UnimplementedControllerServer
	volumeVm string
}

func (f *fakeListController) ListVolumes(_ context.Context, req *pb.ListVolumesRequest) (*pb.ListVolumesResponse, error) {
	f.volumeVm = req.Vm
	return &pb.ListVolumesResponse{Volumes: []*pb.VolumeInfo{
		{Id: "v1", Name: "data", VmName: "web", NodeId: "node-1", StorageBackend: "ceph", StorageSizeBytes: 40 << 30, Encrypted: true},
	}}, nil
}

func (f *fakeListController) ListSecurityGroups(context.Context, *pb.ListSecurityGroupsRequest) (*pb.ListSecurityGroupsResponse, error) {
	return &pb.ListSecurityGroupsResponse{SecurityGroups: []*pb.SecurityGroup{
		{Name: "web", Description: "http", Rules: []*pb.SecurityGroupRule{{}, {}}},
	}}, nil
}

func (f *fakeListController) ListSshKeys(context.Context, *pb.ListSshKeysRequest) (*pb.ListSshKeysResponse, error) {
	return &pb.ListSshKeysResponse{Keys: []*pb.SshKeyInfo{{Name: "alice", PublicKey: "ssh-ed25519 AAAA"}}}, nil
}

func (f *fakeListController) ListOperators(context.Context, *pb.ListOperatorsRequest) (*pb.ListOperatorsResponse, error) {
	return &pb.ListOperatorsResponse{Operators: []*pb.Operator{{
		Name: "alice", CertSerial: "ab12",
		Roles: []pb.OperatorRoleKind{pb.OperatorRoleKind_OPERATOR_ROLE_KIND_VM_ADMIN, pb.OperatorRoleKind_OPERATOR_ROLE_KIND_BACKUP_OPERATOR},
	}}}, nil
}

func (f *fakeListController) ListIngressGateways(context.Context, *pb.ListIngressGatewaysRequest) (*pb.ListIngressGatewaysResponse, error) {
	return &pb.ListIngressGatewaysResponse{Gateways: []*pb.IngressGateway{
		{Name: "edge", Generation: 3, Status: &pb.IngressGatewayStatus{Phase: "Ready"}},
		{Name: "bare"},
	}}, nil
}

func (f *fakeListController) ListIngressRoutes(context.Context, *pb.ListIngressRoutesRequest) (*pb.ListIngressRoutesResponse, error) {
	return &pb.ListIngressRoutesResponse{Routes: []*pb.IngressRoute{
		{Name: "app", Gateway: "edge", Owner: "alice", Status: &pb.IngressRouteStatus{Phase: "Ready"}},
	}}, nil
}

func (f *fakeListController) ListWafPolicies(context.Context, *pb.ListWafPoliciesRequest) (*pb.ListWafPoliciesResponse, error) {
	return &pb.ListWafPoliciesResponse{Policies: []*pb.WafPolicy{{Name: "strict", Generation: 2}}}, nil
}

func (f *fakeListController) ListClusterUpdates(context.Context, *pb.ListClusterUpdatesRequest) (*pb.ListClusterUpdatesResponse, error) {
	return &pb.ListClusterUpdatesResponse{ClusterUpdates: []*pb.ClusterUpdate{
		{Spec: &pb.ClusterUpdateSpec{Name: "0.7.0", UpdateKind: "release"}, Generation: 1},
	}}, nil
}

func readList(t *testing.T, ds *schema.Resource, client *apiClient, raw map[string]interface{}) *schema.ResourceData {
	t.Helper()
	d := schema.TestResourceDataRaw(t, ds.Schema, raw)
	if diags := ds.ReadContext(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	return d
}

func TestListDataSourcesAreRegistered(t *testing.T) {
	sources := New().DataSourcesMap
	for _, name := range []string{
		"kcore_volumes", "kcore_security_groups", "kcore_ssh_keys", "kcore_operators",
		"kcore_ingress_gateways", "kcore_ingress_routes", "kcore_waf_policies", "kcore_cluster_updates",
	} {
		if sources[name] == nil {
			t.Errorf("%s is not registered", name)
		}
	}
}

func TestVolumesDataSourcePassesTheVmFilter(t *testing.T) {
	fake := &fakeListController{}
	client := newControllerClient(t, fake)
	d := readList(t, dataSourceVolumes(), client, map[string]interface{}{"vm": "web"})
	if fake.volumeVm != "web" {
		t.Fatalf("vm filter = %q", fake.volumeVm)
	}
	if d.Get("volumes.0.name") != "data" || d.Get("volumes.0.size_bytes") != 40<<30 || d.Get("volumes.0.encrypted") != true || d.Get("volumes.0.vm_name") != "web" {
		t.Fatalf("state = %v", d.State())
	}
}

func TestSecurityGroupAndSshKeyDataSources(t *testing.T) {
	client := newControllerClient(t, &fakeListController{})
	groups := readList(t, dataSourceSecurityGroups(), client, nil)
	if groups.Get("security_groups.0.name") != "web" || groups.Get("security_groups.0.rule_count") != 2 {
		t.Fatalf("groups = %v", groups.State())
	}
	keys := readList(t, dataSourceSshKeys(), client, nil)
	if keys.Get("keys.0.name") != "alice" || keys.Get("keys.0.public_key") != "ssh-ed25519 AAAA" {
		t.Fatalf("keys = %v", keys.State())
	}
}

func TestOperatorsDataSourceNamesEveryRole(t *testing.T) {
	client := newControllerClient(t, &fakeListController{})
	d := readList(t, dataSourceOperators(), client, nil)
	if d.Get("operators.0.name") != "alice" || d.Get("operators.0.cert_serial") != "ab12" {
		t.Fatalf("state = %v", d.State())
	}
	if d.Get("operators.0.roles.0") != "vm-admin" || d.Get("operators.0.roles.1") != "backup-operator" {
		t.Fatalf("roles = %v", d.Get("operators.0.roles"))
	}
}

func TestIngressAndWafDataSources(t *testing.T) {
	client := newControllerClient(t, &fakeListController{})
	gateways := readList(t, dataSourceIngressGateways(), client, nil)
	if gateways.Get("gateways.0.phase") != "Ready" || gateways.Get("gateways.0.generation") != 3 || gateways.Get("gateways.1.name") != "bare" || gateways.Get("gateways.1.phase") != "" {
		t.Fatalf("gateways = %v", gateways.State())
	}
	routes := readList(t, dataSourceIngressRoutes(), client, nil)
	if routes.Get("routes.0.gateway") != "edge" || routes.Get("routes.0.owner") != "alice" || routes.Get("routes.0.phase") != "Ready" {
		t.Fatalf("routes = %v", routes.State())
	}
	policies := readList(t, dataSourceWafPolicies(), client, nil)
	if policies.Get("policies.0.name") != "strict" || policies.Get("policies.0.generation") != 2 {
		t.Fatalf("policies = %v", policies.State())
	}
}

func TestClusterUpdatesDataSourceReadsTheSpecName(t *testing.T) {
	client := newControllerClient(t, &fakeListController{})
	d := readList(t, dataSourceClusterUpdates(), client, nil)
	if d.Get("updates.0.name") != "0.7.0" || d.Get("updates.0.update_kind") != "release" || d.Get("updates.0.phase") == "" {
		t.Fatalf("state = %v", d.State())
	}
}
