package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceVgpuHost() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceVgpuHostUpsert,
		ReadContext:   resourceVgpuHostRead,
		UpdateContext: resourceVgpuHostUpsert,
		DeleteContext: resourceVgpuHostDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
			Update: schema.DefaultTimeout(30 * time.Minute),
		},
		Schema: map[string]*schema.Schema{
			"node_id": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Node that owns the NVIDIA card.",
			},
			"host_cards": {
				Type:        schema.TypeList,
				Required:    true,
				Description: "Physical functions that stay on the NVIDIA host driver, for example 0000:61:00.0.",
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    1,
			},
			"num_vfs": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     0,
				Description: "Virtual functions to create. 0 asks the card for every function it supports.",
			},
			"manager_url": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "HTTP or HTTPS URL of the NVIDIA Virtual GPU Manager .run file.",
			},
			"manager_sha256": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "SHA256 of the .run file.",
			},
			"manager_file": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Installer filename. The node stores the file under this name.",
			},
			"manager_version": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "595.91.04",
				Description: "Driver version recorded in the host configuration.",
			},
			"reboot": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Reboot the node when the kernel has no IOMMU groups yet or the card is still on nouveau.",
			},
			"store_path": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Nix store path of the staged .run file.",
			},
			"reboot_required": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "True when the last apply needed a reboot before slices could appear.",
			},
		},
	}
}

func resourceVgpuHostUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)
	nodeID := strings.TrimSpace(d.Get("node_id").(string))
	resp, err := client.controller.ConfigureNodeVgpu(ctx, &pb.ConfigureNodeVgpuRequest{
		NodeId:    nodeID,
		Enabled:   true,
		HostCards: schemaStringList(d, "host_cards"),
		NumVfs:    int32(d.Get("num_vfs").(int)),
		Manager: &pb.VgpuManager{
			Url:     strings.TrimSpace(d.Get("manager_url").(string)),
			Sha256:  strings.TrimSpace(d.Get("manager_sha256").(string)),
			File:    strings.TrimSpace(d.Get("manager_file").(string)),
			Version: strings.TrimSpace(d.Get("manager_version").(string)),
		},
	})
	if err != nil {
		return diag.FromErr(fmt.Errorf("configuring vGPU host on %s: %w", nodeID, err))
	}
	if !resp.Success {
		return diag.FromErr(fmt.Errorf("configuring vGPU host on %s: %s", nodeID, resp.Message))
	}
	d.SetId(nodeID)
	d.Set("store_path", resp.StorePath)
	d.Set("reboot_required", resp.RebootRequired)
	if resp.RebootRequired && d.Get("reboot").(bool) {
		reboot, err := client.controller.RebootNode(ctx, &pb.RebootNodeRequest{NodeId: nodeID})
		if err != nil {
			return diag.FromErr(fmt.Errorf("rebooting %s: %w", nodeID, err))
		}
		if !reboot.Success {
			return diag.FromErr(fmt.Errorf("rebooting %s: %s", nodeID, reboot.Message))
		}
	}
	timeout := 30 * time.Minute
	if d.Timeout(schema.TimeoutCreate) > 0 && d.IsNewResource() {
		timeout = d.Timeout(schema.TimeoutCreate)
	} else if d.Timeout(schema.TimeoutUpdate) > 0 {
		timeout = d.Timeout(schema.TimeoutUpdate)
	}
	if err := waitForVgpuSlices(ctx, client, nodeID, timeout); err != nil {
		return diag.FromErr(err)
	}
	return resourceVgpuHostRead(ctx, d, meta)
}

func waitForVgpuSlices(ctx context.Context, client *apiClient, nodeID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for {
		resp, err := client.controller.ListPciDevices(ctx, &pb.ListGpusRequest{NodeId: nodeID})
		if err == nil {
			for _, device := range resp.Gpus {
				if device.Kind == "vgpu" && (device.Profile != "" || len(device.Profiles) > 0) {
					return nil
				}
			}
			last = fmt.Sprintf("%d PCI devices, no vGPU slice yet", len(resp.Gpus))
		} else {
			last = err.Error()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for vGPU slices on %s: %s", nodeID, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
}

func resourceVgpuHostRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)
	var diags diag.Diagnostics
	nodeID := d.Id()
	if nodeID == "" {
		nodeID = strings.TrimSpace(d.Get("node_id").(string))
	}
	resp, err := client.controller.GetNodeVgpu(ctx, &pb.GetNodeVgpuRequest{NodeId: nodeID})
	if err != nil {
		return diag.FromErr(fmt.Errorf("reading vGPU host %s: %w", nodeID, err))
	}
	if !resp.Enabled {
		d.SetId("")
		return diags
	}
	d.SetId(nodeID)
	d.Set("node_id", nodeID)
	d.Set("host_cards", resp.HostCards)
	d.Set("num_vfs", resp.NumVfs)
	d.Set("manager_file", resp.ManagerFile)
	d.Set("manager_sha256", resp.ManagerSha256)
	d.Set("manager_version", resp.ManagerVersion)
	d.Set("store_path", resp.StagedStorePath)
	return diags
}

func resourceVgpuHostDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)
	var diags diag.Diagnostics
	nodeID := d.Id()
	resp, err := client.controller.ConfigureNodeVgpu(ctx, &pb.ConfigureNodeVgpuRequest{
		NodeId:  nodeID,
		Enabled: false,
	})
	if err != nil {
		return diag.FromErr(fmt.Errorf("disabling vGPU host %s: %w", nodeID, err))
	}
	if !resp.Success {
		return diag.FromErr(fmt.Errorf("disabling vGPU host %s: %s", nodeID, resp.Message))
	}
	d.SetId("")
	return diags
}
