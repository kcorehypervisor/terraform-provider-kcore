package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

type fakePostgresqlController struct {
	pb.UnimplementedControllerServer
	created []*pb.CreatePostgresqlRequest
	deleted []string
	stored  *pb.PostgresqlInfo
}

func (f *fakePostgresqlController) CreatePostgresql(_ context.Context, req *pb.CreatePostgresqlRequest) (*pb.CreatePostgresqlResponse, error) {
	f.created = append(f.created, req)
	database := req.Database
	if database == "" {
		database = req.Name
	}
	pkg := req.Package
	if pkg == "" {
		pkg = "postgresql"
	}
	node := req.TargetNode
	if node == "" {
		node = "node-105"
	}
	f.stored = &pb.PostgresqlInfo{Name: req.Name, Database: database, Package: pkg, Port: req.Port, NodeId: node}
	return &pb.CreatePostgresqlResponse{Success: true, Postgresql: f.stored}, nil
}

func (f *fakePostgresqlController) GetPostgresql(_ context.Context, req *pb.GetPostgresqlRequest) (*pb.GetPostgresqlResponse, error) {
	if f.stored == nil || f.stored.Name != req.Name {
		return nil, status.Error(codes.NotFound, "postgresql not found")
	}
	return &pb.GetPostgresqlResponse{Postgresql: f.stored}, nil
}

func (f *fakePostgresqlController) DeletePostgresql(_ context.Context, req *pb.DeletePostgresqlRequest) (*pb.DeletePostgresqlResponse, error) {
	f.deleted = append(f.deleted, req.Name)
	f.stored = nil
	return &pb.DeletePostgresqlResponse{Success: true}, nil
}

func newPostgresqlClient(t *testing.T, fake *fakePostgresqlController) *apiClient {
	t.Helper()
	return newControllerClient(t, fake)
}

func TestPostgresqlApplyReadsBackTheControllersChoices(t *testing.T) {
	fake := &fakePostgresqlController{}
	client := newPostgresqlClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourcePostgresql().Schema, map[string]interface{}{"name": "app"})
	d.MarkNewResource()
	if diags := resourcePostgresqlUpsert(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	if len(fake.created) != 1 || fake.created[0].Port != 5432 || fake.created[0].Database != "" || fake.created[0].Package != "" {
		t.Fatalf("request = %+v", fake.created)
	}
	if d.Id() != "app" || d.Get("database") != "app" || d.Get("package") != "postgresql" || d.Get("port") != 5432 || d.Get("node_id") != "node-105" {
		t.Fatalf("state = %v", d.State())
	}
}

func TestPostgresqlApplySendsEveryField(t *testing.T) {
	fake := &fakePostgresqlController{}
	client := newPostgresqlClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourcePostgresql().Schema, map[string]interface{}{
		"name": "billing", "database": "billing_db", "package": "postgresql_16", "port": 5433, "target_node": "node-107",
	})
	if diags := resourcePostgresqlUpsert(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	req := fake.created[0]
	if req.Name != "billing" || req.Database != "billing_db" || req.Package != "postgresql_16" || req.Port != 5433 || req.TargetNode != "node-107" {
		t.Fatalf("request = %+v", req)
	}
}

func TestPostgresqlReadDropsAVanishedInstance(t *testing.T) {
	fake := &fakePostgresqlController{}
	client := newPostgresqlClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourcePostgresql().Schema, map[string]interface{}{"name": "gone"})
	d.SetId("gone")
	if diags := resourcePostgresqlRead(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	if d.Id() != "" {
		t.Fatalf("id = %q", d.Id())
	}
}

func TestPostgresqlDeleteCallsTheController(t *testing.T) {
	fake := &fakePostgresqlController{stored: &pb.PostgresqlInfo{Name: "app"}}
	client := newPostgresqlClient(t, fake)
	d := schema.TestResourceDataRaw(t, resourcePostgresql().Schema, map[string]interface{}{"name": "app"})
	d.SetId("app")
	if diags := resourcePostgresqlDelete(context.Background(), d, client); diags.HasError() {
		t.Fatalf("%v", diags)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "app" || d.Id() != "" {
		t.Fatalf("deleted = %v id = %q", fake.deleted, d.Id())
	}
}

func TestPostgresqlSchemaRefusesBadInput(t *testing.T) {
	r := resourcePostgresql()
	for name, raw := range map[string]map[string]interface{}{
		"package":  {"name": "a", "package": "mysql"},
		"database": {"name": "a", "database": "1bad-name"},
		"port":     {"name": "a", "port": 70000},
	} {
		diags := r.Validate(terraformConfig(t, raw))
		if !diags.HasError() {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if diags := r.Validate(terraformConfig(t, map[string]interface{}{"name": "a", "package": "postgresql_17", "port": 5432})); diags.HasError() {
		t.Errorf("valid input refused: %v", diags)
	}
}

func TestPostgresqlOnlyPortChangesInPlace(t *testing.T) {
	for key, attr := range resourcePostgresql().Schema {
		if key == "port" || key == "node_id" {
			if attr.ForceNew {
				t.Errorf("%s must not force a replacement", key)
			}
			continue
		}
		if !attr.ForceNew && !attr.Computed {
			t.Errorf("%s must force a replacement", key)
		}
	}
}

func terraformConfig(t *testing.T, raw map[string]interface{}) *terraform.ResourceConfig {
	t.Helper()
	return terraform.NewResourceConfigRaw(raw)
}
