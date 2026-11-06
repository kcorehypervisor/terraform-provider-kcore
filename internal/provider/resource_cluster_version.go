package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

const clusterVersionID = "cluster-version"

func resourceClusterVersion() *schema.Resource {
	return &schema.Resource{
		Description: "The kcore release every node runs (`kctl update version set`). " +
			"Changing `version` starts a rollout: by default a canary node updates and soaks while the controller compares it with the other nodes, " +
			"then the rest update one at a time. Another version is refused while a rollout runs. " +
			"Destroy only forgets the resource; nodes keep their version.",
		CreateContext: resourceClusterVersionSet,
		ReadContext:   resourceClusterVersionRead,
		UpdateContext: resourceClusterVersionSet,
		DeleteContext: resourceClusterVersionDelete,
		Schema: map[string]*schema.Schema{
			"version": {
				Type:             schema.TypeString,
				Required:         true,
				Description:      "Release tag, `v0.7.0` or `0.7.0`.",
				DiffSuppressFunc: func(_, old, new string, _ *schema.ResourceData) bool { return releaseTag(old) == releaseTag(new) },
			},
			"flake_ref": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Repository the tag is resolved in. Empty means `github:kcorehypervisor/kcore`; an air-gapped site names its mirror.",
			},
			"drain_vms": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "Migrate guests off a node before it reboots into a new kernel.",
			},
			"canary_soak_seconds": {
				Type:          schema.TypeInt,
				Optional:      true,
				Description:   "How long the canary soaks. 0 means 24 hours when the release moves to another NixOS release and one hour otherwise. At most 7 days.",
				ValidateFunc:  validation.IntBetween(0, maxCanarySoakSeconds),
				ConflictsWith: []string{"no_canary"},
			},
			"canary_node": {
				Type:          schema.TypeString,
				Optional:      true,
				Description:   "The canary. Empty picks a node labelled `kcore.io/update-canary`, else the first node in rollout order. A node that runs a controller is refused unless every node does.",
				ConflictsWith: []string{"no_canary"},
			},
			"no_canary": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "Update every node one at a time with no canary and no soak.",
			},
			"flake_rev":   {Type: schema.TypeString, Computed: true, Description: "Commit the tag resolved to."},
			"update_name": {Type: schema.TypeString, Computed: true, Description: "ClusterUpdate of the latest rollout."},
			"phase":       {Type: schema.TypeString, Computed: true, Description: "Phase of that rollout."},
			"canary_node_id": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The canary the last change picked. Empty without a canary or when nothing changed.",
			},
		},
	}
}

func releaseTag(version string) string {
	v := strings.TrimSpace(version)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func resourceClusterVersionSet(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.SetClusterVersion(ctx, &pb.SetClusterVersionRequest{
		Version:           releaseTag(s(d, "version")),
		FlakeRef:          s(d, "flake_ref"),
		DrainVms:          b(d, "drain_vms"),
		CanarySoakSeconds: toInt32(d.Get("canary_soak_seconds").(int)),
		NoCanary:          b(d, "no_canary"),
		CanaryNodeId:      s(d, "canary_node"),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(clusterVersionID)
	_ = d.Set("canary_node_id", resp.CanaryNodeId)
	return resourceClusterVersionRead(ctx, d, meta)
}

func resourceClusterVersionRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetClusterVersion(ctx, &pb.GetClusterVersionRequest{})
	if err != nil {
		return diag.FromErr(err)
	}
	if resp.Version != "" {
		_ = d.Set("version", resp.Version)
	}
	_ = d.Set("flake_rev", resp.FlakeRev)
	_ = d.Set("update_name", resp.UpdateName)
	_ = d.Set("phase", resp.Phase.String())
	return nil
}

func resourceClusterVersionDelete(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
	d.SetId("")
	return nil
}
