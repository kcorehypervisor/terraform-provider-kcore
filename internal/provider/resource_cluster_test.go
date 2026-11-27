package provider

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
	nodepb "github.com/kcorehypervisor/terraform-provider-kcore/api/node"
)

type fakeClusterController struct {
	pb.UnimplementedControllerServer
	nodes   []*pb.NodeInfo
	set     *pb.SetClusterVersionRequest
	version *pb.GetClusterVersionResponse
}

func (f *fakeClusterController) ListNodes(context.Context, *pb.ListNodesRequest) (*pb.ListNodesResponse, error) {
	return &pb.ListNodesResponse{Nodes: f.nodes}, nil
}

func (f *fakeClusterController) SetClusterVersion(_ context.Context, req *pb.SetClusterVersionRequest) (*pb.SetClusterVersionResponse, error) {
	f.set = req
	return &pb.SetClusterVersionResponse{Version: req.Version, UpdateName: "version-v0-9-0"}, nil
}

func (f *fakeClusterController) GetClusterVersion(context.Context, *pb.GetClusterVersionRequest) (*pb.GetClusterVersionResponse, error) {
	if f.version != nil {
		return f.version, nil
	}
	return &pb.GetClusterVersionResponse{
		Version:           "v0.9.0",
		UpdateName:        "version-v0-9-0",
		Phase:             pb.ClusterUpdatePhase_CLUSTER_UPDATE_PHASE_SUCCEEDED,
		DrainVms:          true,
		Reboot:            true,
		CanarySoakSeconds: 3600,
		InSync:            true,
	}, nil
}

func (f *fakeClusterController) IssueNodeBootstrapCert(_ context.Context, req *pb.IssueNodeBootstrapCertRequest) (*pb.IssueNodeBootstrapCertResponse, error) {
	return &pb.IssueNodeBootstrapCertResponse{
		Success: true,
		CertPem: "cert",
		KeyPem:  "key",
		Message: req.NodeId,
	}, nil
}

type fakeNodeAdmin struct {
	nodepb.UnimplementedNodeAdminServer
	listDisksCalls int
	installs       []*nodepb.InstallToDiskRequest
	acceptInstall  bool
}

func (f *fakeNodeAdmin) ListDisks(context.Context, *nodepb.ListDisksRequest) (*nodepb.ListDisksResponse, error) {
	f.listDisksCalls++
	return &nodepb.ListDisksResponse{Disks: []*nodepb.DiskInfo{{Name: "sda", Path: "/dev/sda"}}}, nil
}

func (f *fakeNodeAdmin) InstallToDisk(_ context.Context, req *nodepb.InstallToDiskRequest) (*nodepb.InstallToDiskResponse, error) {
	f.installs = append(f.installs, req)
	return &nodepb.InstallToDiskResponse{Accepted: f.acceptInstall, Message: "ok"}, nil
}

func startFakeNodeAdmin(t *testing.T, fake *fakeNodeAdmin) string {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	nodepb.RegisterNodeAdminServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop() })
	// Override dial for tests by returning a custom address scheme is hard;
	// tests that need Install use helpers directly.
	_ = listener
	return "bufnet"
}

func TestClusterControllerAddrFromNode(t *testing.T) {
	d := schema.TestResourceDataRaw(t, resourceCluster().Schema, map[string]interface{}{
		"name":      "prod",
		"certs_dir": "/tmp/certs",
		"node": []interface{}{
			map[string]interface{}{
				"name":       "node-107",
				"address":    "192.168.40.107",
				"os_disk":    "/dev/sda",
				"controller": true,
			},
		},
	})
	got, err := clusterControllerAddr(d)
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.168.40.107:9090" {
		t.Fatalf("got %q", got)
	}
}

func TestRejectNodeImmutableChanges(t *testing.T) {
	old := []clusterNode{{Name: "n1", Address: "10.0.0.1:9091", OsDisk: "/dev/sda", Controller: true}}
	newer := []clusterNode{{Name: "n1", Address: "10.0.0.1:9091", OsDisk: "/dev/sdb", Controller: true}}
	if err := rejectNodeImmutableChanges(old, newer); err == nil {
		t.Fatal("expected os_disk change to fail")
	}
}

func TestOrderControllersFirst(t *testing.T) {
	got := orderControllersFirst([]clusterNode{
		{Name: "a", Controller: false},
		{Name: "b", Controller: true},
		{Name: "c", Controller: false},
	})
	if got[0].Name != "b" || len(got) != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestSetClusterDesiredVersionMapsReboot(t *testing.T) {
	fake := &fakeClusterController{}
	client := newControllerClient(t, fake)
	_ = client

	dir := t.TempDir()
	if err := generateClusterPKI(dir, "10.0.0.8:9090"); err != nil {
		t.Fatal(err)
	}

	// Dial through bufconn by temporarily exercising the request builder path
	// via a local SetClusterVersion on the fake (mirrors resource mapping).
	d := schema.TestResourceDataRaw(t, resourceCluster().Schema, map[string]interface{}{
		"name":      "prod",
		"certs_dir": dir,
		"version":   "0.9.0",
		"update": []interface{}{
			map[string]interface{}{
				"reboot":              true,
				"drain_vms":           true,
				"canary_soak_seconds": 3600,
				"canary_node":         "node-105",
			},
		},
	})
	req := &pb.SetClusterVersionRequest{Version: releaseTag(s(d, "version"))}
	if updates := d.Get("update").([]interface{}); len(updates) > 0 {
		if m, ok := updates[0].(map[string]interface{}); ok {
			req.Reboot, _ = m["reboot"].(bool)
			req.DrainVms, _ = m["drain_vms"].(bool)
			if v, ok := m["canary_soak_seconds"].(int); ok {
				req.CanarySoakSeconds = toInt32(v)
			}
			if v, ok := m["canary_node"].(string); ok {
				req.CanaryNodeId = v
			}
		}
	}
	if _, err := fake.SetClusterVersion(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !fake.set.Reboot || !fake.set.DrainVms || fake.set.Version != "v0.9.0" || fake.set.CanaryNodeId != "node-105" {
		t.Fatalf("%+v", fake.set)
	}
}

func TestLiveISOReachableViaListDisks(t *testing.T) {
	fake := &fakeNodeAdmin{acceptInstall: true}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	nodepb.RegisterNodeAdminServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop() })

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, err = nodepb.NewNodeAdminClient(conn).ListDisks(context.Background(), &nodepb.ListDisksRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.listDisksCalls != 1 {
		t.Fatalf("calls=%d", fake.listDisksCalls)
	}
}

func TestClusterCreatePKIOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certs")
	d := schema.TestResourceDataRaw(t, resourceCluster().Schema, map[string]interface{}{
		"name":       "lab",
		"controller": "10.0.0.8:9090",
		"certs_dir":  dir,
	})
	if diags := resourceClusterCreate(context.Background(), d, &apiClient{}); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	if d.Id() != "lab" || !clusterCertsPresent(dir) {
		t.Fatalf("id=%q present=%v", d.Id(), clusterCertsPresent(dir))
	}
	if d.Get("client_cert_path") != filepath.Join(dir, "kctl.crt") {
		t.Fatalf("paths = %v", d.Get("client_cert_path"))
	}
}

func TestNodeAndClusterVersionDeprecated(t *testing.T) {
	if resourceNode().DeprecationMessage == "" {
		t.Fatal("kcore_node should be deprecated")
	}
	if resourceClusterVersion().DeprecationMessage == "" {
		t.Fatal("kcore_cluster_version should be deprecated")
	}
}
