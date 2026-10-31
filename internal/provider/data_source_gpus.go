package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func dataSourceGpus() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceGpusRead,
		Schema: map[string]*schema.Schema{
			"node_id": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Limit the inventory to one node. Empty lists every node.",
			},
			"devices": {
				Type:        schema.TypeList,
				Computed:    true,
				Description: "GPUs and vGPU slices reported by the controller.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name":        {Type: schema.TypeString, Computed: true},
						"kind":        {Type: schema.TypeString, Computed: true},
						"role":        {Type: schema.TypeString, Computed: true},
						"address":     {Type: schema.TypeString, Computed: true},
						"parent":      {Type: schema.TypeString, Computed: true},
						"profile":     {Type: schema.TypeString, Computed: true},
						"profiles":    {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
						"assignable":  {Type: schema.TypeBool, Computed: true},
						"assigned_vm": {Type: schema.TypeString, Computed: true},
						"node_id":     {Type: schema.TypeString, Computed: true},
					},
				},
			},
		},
	}
}

func dataSourceGpusRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)
	var diags diag.Diagnostics
	nodeID := strings.TrimSpace(d.Get("node_id").(string))
	resp, err := client.controller.ListPciDevices(ctx, &pb.ListGpusRequest{NodeId: nodeID})
	if err != nil {
		return diag.FromErr(fmt.Errorf("listing GPUs: %w", err))
	}
	devices := make([]map[string]interface{}, 0, len(resp.Gpus))
	for _, device := range resp.Gpus {
		devices = append(devices, map[string]interface{}{
			"name":        device.Name,
			"kind":        device.Kind,
			"role":        device.Role,
			"address":     device.Address,
			"parent":      device.Parent,
			"profile":     device.Profile,
			"profiles":    device.Profiles,
			"assignable":  device.Assignable,
			"assigned_vm": device.AssignedVm,
			"node_id":     device.NodeId,
		})
	}
	if nodeID == "" {
		d.SetId("gpus-all")
	} else {
		d.SetId("gpus-" + nodeID)
	}
	d.Set("devices", devices)
	return diags
}
