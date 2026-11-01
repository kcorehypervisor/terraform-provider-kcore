package provider

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

type fakeIngressController struct {
	pb.UnimplementedControllerServer
	planErrors   []string
	planned      []*pb.PlanIngressRequest
	routeSpecs   []string
	gatewaySpecs []string
	policySpecs  []string
	gatewayReady bool
}

func (f *fakeIngressController) PlanIngress(_ context.Context, req *pb.PlanIngressRequest) (*pb.PlanIngressResponse, error) {
	f.planned = append(f.planned, req)
	return &pb.PlanIngressResponse{Errors: f.planErrors}, nil
}

func (f *fakeIngressController) CreateIngressGateway(_ context.Context, req *pb.CreateIngressGatewayRequest) (*pb.CreateIngressGatewayResponse, error) {
	f.gatewaySpecs = append(f.gatewaySpecs, req.SpecJson)
	return &pb.CreateIngressGatewayResponse{
		Success: true,
		Action:  pb.ApplyAction_APPLY_ACTION_CREATED,
		Gateway: &pb.IngressGateway{Name: req.Name, Generation: 3},
	}, nil
}

func (f *fakeIngressController) GetIngressGateway(_ context.Context, req *pb.GetIngressGatewayRequest) (*pb.GetIngressGatewayResponse, error) {
	return &pb.GetIngressGatewayResponse{Gateway: &pb.IngressGateway{
		Name:       req.Name,
		Generation: 3,
		Status: &pb.IngressGatewayStatus{
			ObservedGeneration: 3,
			Phase:              "ready",
			Nodes:              []*pb.IngressNodeStatus{{NodeId: "node-105", Healthy: true, AppliedHash: "abc"}},
		},
	}}, nil
}

func (f *fakeIngressController) CreateIngressRoute(_ context.Context, req *pb.CreateIngressRouteRequest) (*pb.CreateIngressRouteResponse, error) {
	f.routeSpecs = append(f.routeSpecs, req.SpecJson)
	return &pb.CreateIngressRouteResponse{Success: true}, nil
}

func (f *fakeIngressController) GetIngressRoute(_ context.Context, req *pb.GetIngressRouteRequest) (*pb.GetIngressRouteResponse, error) {
	return &pb.GetIngressRouteResponse{Route: &pb.IngressRoute{
		Name:    req.Name,
		Gateway: "edge",
		Status: &pb.IngressRouteStatus{
			Phase:     "ready",
			Certs:     []*pb.IngressCertStatus{{Host: "shop.example.com", Issuer: "R11"}},
			Upstreams: []*pb.IngressUpstreamHealth{{Address: "10.0.0.5:8080", Healthy: true}},
		},
	}}, nil
}

func (f *fakeIngressController) CreateWafPolicy(_ context.Context, req *pb.CreateWafPolicyRequest) (*pb.CreateWafPolicyResponse, error) {
	f.policySpecs = append(f.policySpecs, req.SpecJson)
	return &pb.CreateWafPolicyResponse{Success: true}, nil
}

func (f *fakeIngressController) GetWafPolicy(_ context.Context, req *pb.GetWafPolicyRequest) (*pb.GetWafPolicyResponse, error) {
	return &pb.GetWafPolicyResponse{Policy: &pb.WafPolicy{Name: req.Name, Generation: 2}}, nil
}

func newIngressClient(t *testing.T, fake *fakeIngressController) *apiClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterControllerServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		server.Stop()
	})
	return &apiClient{conn: conn, controller: pb.NewControllerClient(conn)}
}

func TestIngressGatewayApplyPlansThenWaitsForReady(t *testing.T) {
	fake := &fakeIngressController{}
	client := newIngressClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourceIngressGateway().Schema, map[string]interface{}{
		"name": "edge",
		"spec": `{"nodes":["node-105"],"listeners":[{"address":"203.0.113.10","ports":[80,443]}]}`,
	})
	d.MarkNewResource()
	if diags := resourceIngressGatewayUpsert(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	if d.Id() != "edge" || d.Get("phase") != "ready" || d.Get("generation") != 3 {
		t.Fatalf("id=%q phase=%v generation=%v", d.Id(), d.Get("phase"), d.Get("generation"))
	}
	if len(fake.planned) != 1 || fake.planned[0].Gateway != "edge" || fake.planned[0].Documents[0].Kind != "IngressGateway" {
		t.Fatalf("plan = %+v", fake.planned)
	}
	nodes := d.Get("node_status").([]interface{})
	if len(nodes) != 1 || nodes[0].(map[string]interface{})["node_id"] != "node-105" {
		t.Fatalf("node_status = %v", nodes)
	}
}

func TestIngressPlanErrorsBlockApply(t *testing.T) {
	fake := &fakeIngressController{planErrors: []string{"node node-9 is not registered"}}
	client := newIngressClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourceIngressGateway().Schema, map[string]interface{}{
		"name": "edge",
		"spec": `{"nodes":["node-9"]}`,
	})
	diags := resourceIngressGatewayUpsert(context.Background(), d, client)
	if !diags.HasError() || !strings.Contains(diags[0].Detail, "node-9") {
		t.Fatalf("diags = %v", diags)
	}
	if len(fake.gatewaySpecs) != 0 {
		t.Fatal("a gateway that failed validation must not be created")
	}
}

func TestIngressRouteInjectsTheGateway(t *testing.T) {
	fake := &fakeIngressController{}
	client := newIngressClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourceIngressRoute().Schema, map[string]interface{}{
		"name":    "shop",
		"gateway": "edge",
		"spec":    `{"hosts":["shop.example.com"],"paths":[{"prefix":"/","backends":[{"kind":"external","address":"10.0.0.5:8080"}]}]}`,
	})
	d.MarkNewResource()
	if diags := resourceIngressRouteUpsert(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	var sent map[string]interface{}
	if err := json.Unmarshal([]byte(fake.routeSpecs[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if sent["gateway"] != "edge" || sent["hosts"] == nil {
		t.Fatalf("sent spec = %v", sent)
	}
	if fake.planned[0].Gateway != "edge" {
		t.Fatalf("plan gateway = %q", fake.planned[0].Gateway)
	}
	if d.Get("phase") != "ready" || len(d.Get("certificates").([]interface{})) != 1 || len(d.Get("upstreams").([]interface{})) != 1 {
		t.Fatalf("status not read: %v", d.State())
	}
}

func TestIngressRouteRefusesAConflictingGateway(t *testing.T) {
	if _, err := routeSpecJSON("edge", `{"gateway":"other","hosts":["a.example.com"]}`); err == nil {
		t.Fatal("a gateway in spec that differs from the attribute must be refused")
	}
	out, err := routeSpecJSON("edge", `{"gateway":"edge","hosts":["a.example.com"]}`)
	if err != nil || !strings.Contains(out, `"gateway":"edge"`) {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestWafPolicyAcceptsAnEmptySpec(t *testing.T) {
	fake := &fakeIngressController{}
	client := newIngressClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourceWafPolicy().Schema, map[string]interface{}{
		"name": "crs-default",
		"spec": `{}`,
	})
	if diags := resourceWafPolicyUpsert(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	if d.Id() != "crs-default" || d.Get("generation") != 2 {
		t.Fatalf("id=%q generation=%v", d.Id(), d.Get("generation"))
	}
}

func TestIngressSpecDiffIgnoresKeyOrderAndWhitespace(t *testing.T) {
	spec := resourceIngressGateway().Schema["spec"]
	if !spec.DiffSuppressFunc("spec", `{"a":1,"b":[1,2]}`, "{\n  \"b\": [1, 2],\n  \"a\": 1\n}", nil) {
		t.Fatal("equivalent JSON must not show a diff")
	}
	if spec.DiffSuppressFunc("spec", `{"a":1}`, `{"a":2}`, nil) {
		t.Fatal("different JSON must show a diff")
	}
	if got := spec.StateFunc(`{ "b": 1, "a": 2 }`); got != `{"a":2,"b":1}` {
		t.Fatalf("state = %s", got)
	}
	if _, errs := spec.ValidateFunc("{nope", "spec"); len(errs) == 0 {
		t.Fatal("invalid JSON must be refused")
	}
}
