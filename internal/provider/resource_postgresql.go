package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

var postgresqlPackages = []string{"postgresql", "postgresql_14", "postgresql_15", "postgresql_16", "postgresql_17"}

var postgresqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func resourcePostgresql() *schema.Resource {
	return &schema.Resource{
		Description: "One PostgreSQL database from the NixOS `services.postgresql` module (`kctl apply -f` with `kind: PostgreSQL`). " +
			"v1 is a single database per node, reached through the local Unix socket. Only `port` can change in place. " +
			"Destroy removes the database from the node configuration and leaves `/var/lib/postgresql` on disk.",
		CreateContext: resourcePostgresqlUpsert,
		ReadContext:   resourcePostgresqlRead,
		UpdateContext: resourcePostgresqlUpsert,
		DeleteContext: resourcePostgresqlDelete,
		Importer:      &schema.ResourceImporter{StateContext: schema.ImportStatePassthroughContext},
		Schema: map[string]*schema.Schema{
			"name": {
				Type: schema.TypeString, Required: true, ForceNew: true,
				Description: "Instance name.",
			},
			"database": {
				Type: schema.TypeString, Optional: true, Computed: true, ForceNew: true,
				ValidateFunc: validation.StringMatch(postgresqlIdentifier, "must be a PostgreSQL identifier: a letter or underscore, then letters, digits, or underscores"),
				Description:  "Database name. Defaults to `name` when `name` is a PostgreSQL identifier.",
			},
			"package": {
				Type: schema.TypeString, Optional: true, Computed: true, ForceNew: true,
				ValidateFunc: validation.StringInSlice(postgresqlPackages, false),
				Description:  "Nixpkgs attribute: " + strings.Join(postgresqlPackages, ", ") + ". Defaults to `postgresql`.",
			},
			"port": {
				Type: schema.TypeInt, Optional: true, Default: 5432,
				ValidateFunc: validation.IntBetween(1, 65535),
				Description:  "TCP port. Can change in place.",
			},
			"target_node": {
				Type: schema.TypeString, Optional: true, Computed: true, ForceNew: true,
				Description: "Node that runs the database. The scheduler picks one when empty.",
			},
			"node_id": {
				Type: schema.TypeString, Computed: true,
				Description: "Node the controller placed the database on.",
			},
		},
	}
}

func postgresqlRequest(d *schema.ResourceData) *pb.CreatePostgresqlRequest {
	return &pb.CreatePostgresqlRequest{
		Name:       strings.TrimSpace(s(d, "name")),
		Database:   s(d, "database"),
		Package:    s(d, "package"),
		Port:       i32(d, "port"),
		TargetNode: s(d, "target_node"),
	}
}

func resourcePostgresqlUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	req := postgresqlRequest(d)
	resp, err := api(meta).controller.CreatePostgresql(ctx, req)
	if err != nil {
		return diag.FromErr(fmt.Errorf("applying postgresql %s: %w", req.GetName(), err))
	}
	if err := fail(resp.GetSuccess(), resp.GetMessage()); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(req.GetName())
	return resourcePostgresqlRead(ctx, d, meta)
}

func setPostgresqlState(d *schema.ResourceData, info *pb.PostgresqlInfo) error {
	for key, value := range map[string]interface{}{
		"name":        info.GetName(),
		"database":    info.GetDatabase(),
		"package":     info.GetPackage(),
		"port":        int(info.GetPort()),
		"target_node": info.GetNodeId(),
		"node_id":     info.GetNodeId(),
	} {
		if err := d.Set(key, value); err != nil {
			return err
		}
	}
	return nil
}

func resourcePostgresqlRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetPostgresql(ctx, &pb.GetPostgresqlRequest{Name: d.Id()})
	if err != nil {
		if grpcNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}
	info := resp.GetPostgresql()
	if info == nil || info.GetName() == "" {
		d.SetId("")
		return nil
	}
	return diag.FromErr(setPostgresqlState(d, info))
}

func resourcePostgresqlDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeletePostgresql(ctx, &pb.DeletePostgresqlRequest{Name: d.Id()})
	if err != nil {
		if grpcNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}
	if err := fail(resp.GetSuccess(), resp.GetMessage()); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func dataSourcePostgresql() *schema.Resource {
	return &schema.Resource{
		Description: "Read one PostgreSQL database.",
		ReadContext: dataSourcePostgresqlRead,
		Schema: map[string]*schema.Schema{
			"name":     {Type: schema.TypeString, Required: true},
			"database": {Type: schema.TypeString, Computed: true},
			"package":  {Type: schema.TypeString, Computed: true},
			"port":     {Type: schema.TypeInt, Computed: true},
			"node_id":  {Type: schema.TypeString, Computed: true},
		},
	}
}

func dataSourcePostgresqlRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	name := s(d, "name")
	resp, err := api(meta).controller.GetPostgresql(ctx, &pb.GetPostgresqlRequest{Name: name})
	if err != nil {
		return diag.FromErr(fmt.Errorf("reading postgresql %s: %w", name, err))
	}
	info := resp.GetPostgresql()
	if info == nil || info.GetName() == "" {
		return diag.Errorf("postgresql %s not found", name)
	}
	d.SetId(info.GetName())
	for key, value := range map[string]interface{}{
		"database": info.GetDatabase(),
		"package":  info.GetPackage(),
		"port":     int(info.GetPort()),
		"node_id":  info.GetNodeId(),
	} {
		if err := d.Set(key, value); err != nil {
			return diag.FromErr(err)
		}
	}
	return nil
}
