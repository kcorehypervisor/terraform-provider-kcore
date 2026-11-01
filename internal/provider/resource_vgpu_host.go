package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceVgpuHost() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceVgpuHostUpsert,
		ReadContext:   resourceVgpuHostRead,
		UpdateContext: resourceVgpuHostUpsert,
		DeleteContext: resourceVgpuHostDelete,
		CustomizeDiff: resourceVgpuHostCustomizeDiff,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
			Update: schema.DefaultTimeout(30 * time.Minute),
		},
		Schema: map[string]*schema.Schema{
			"node_id": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Node that owns the card.",
			},
			"vendor": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "nvidia",
				ValidateFunc: validation.StringInSlice([]string{"nvidia", "intel", "amd"}, false),
				Description:  "Card vendor. nvidia stages the Virtual GPU Manager. intel (xe) and amd (GIM) split the card with SR-IOV into num_vfs slices named like intel-2vf or radeon-4vf.",
			},
			"host_cards": {
				Type:        schema.TypeList,
				Required:    true,
				Description: "Physical functions that stay on the host driver, for example 0000:61:00.0. An intel or amd card can carry its own virtual function count as 0000:c3:00.0=4; a card without a count uses num_vfs.",
				DiffSuppressFunc: func(_, oldValue, newValue string, d *schema.ResourceData) bool {
					numVfs := d.Get("num_vfs").(int)
					return splitCardKey(oldValue, numVfs) == splitCardKey(newValue, numVfs)
				},
				Elem:     &schema.Schema{Type: schema.TypeString},
				MinItems: 1,
			},
			"num_vfs": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     0,
				Description: "Virtual functions to create on each card. Required for intel and amd. For nvidia, 0 asks the card for every function it supports.",
			},
			"manager_url": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "HTTP or HTTPS URL of the NVIDIA Virtual GPU Manager .run file. Required for nvidia.",
			},
			"manager_sha256": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "SHA256 of the .run file. Required for nvidia.",
			},
			"manager_file": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Installer filename. The node stores the file under this name. Required for nvidia.",
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
	vendor := d.Get("vendor").(string)
	req := &pb.ConfigureNodeVgpuRequest{
		NodeId:    nodeID,
		Enabled:   true,
		HostCards: schemaStringList(d, "host_cards"),
		NumVfs:    int32(d.Get("num_vfs").(int)),
		Vendor:    vendor,
	}
	if vendor == "nvidia" {
		req.Manager = &pb.VgpuManager{
			Url:     strings.TrimSpace(d.Get("manager_url").(string)),
			Sha256:  strings.TrimSpace(d.Get("manager_sha256").(string)),
			File:    strings.TrimSpace(d.Get("manager_file").(string)),
			Version: strings.TrimSpace(d.Get("manager_version").(string)),
		}
	}
	resp, err := client.controller.ConfigureNodeVgpu(ctx, req)
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

func resourceVgpuHostCustomizeDiff(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
	return validateVgpuHost(
		d.Get("vendor").(string),
		d.Get("num_vfs").(int),
		schemaStringListFromDiff(d, "host_cards"),
		d.Get("manager_url").(string),
		d.Get("manager_sha256").(string),
		d.Get("manager_file").(string),
	)
}

func schemaStringListFromDiff(d *schema.ResourceDiff, key string) []string {
	raw, _ := d.Get(key).([]interface{})
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

// splitCardKey is the form the controller stores: the address and the
// virtual function count it will use, so a bare address and address=N with the
// same count compare equal.
func splitCardKey(card string, defaultVfs int) string {
	address, count, hasCount := strings.Cut(card, "=")
	address = strings.ToLower(strings.TrimSpace(address))
	count = strings.TrimSpace(count)
	if !hasCount || count == "" {
		if defaultVfs > 0 {
			return fmt.Sprintf("%s=%d", address, defaultVfs)
		}
		return address
	}
	return address + "=" + count
}

func validateVgpuHost(vendor string, numVfs int, hostCards []string, url, sha256, file string) error {
	if vendor == "nvidia" || vendor == "" {
		for _, field := range [][2]string{{"manager_url", url}, {"manager_sha256", sha256}, {"manager_file", file}} {
			if strings.TrimSpace(field[1]) == "" {
				return fmt.Errorf("%s is required when vendor is nvidia", field[0])
			}
		}
		return nil
	}
	for _, card := range hostCards {
		if _, count, hasCount := strings.Cut(card, "="); hasCount {
			if n, err := strconv.Atoi(strings.TrimSpace(count)); err != nil || n < 1 {
				return fmt.Errorf("host card %q: the count after '=' must be a positive number", card)
			}
		} else if numVfs < 1 {
			return fmt.Errorf("num_vfs is required when vendor is %s, or give %s a count as %s=N", vendor, card, card)
		}
	}
	if url != "" || sha256 != "" || file != "" {
		return fmt.Errorf("manager_url, manager_sha256, and manager_file stage the NVIDIA manager; %s cards do not use them", vendor)
	}
	return nil
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
	vendor := resp.Vendor
	if vendor == "" {
		vendor = "nvidia"
	}
	d.Set("vendor", vendor)
	if vendor == "nvidia" {
		d.Set("manager_file", resp.ManagerFile)
		d.Set("manager_sha256", resp.ManagerSha256)
		d.Set("manager_version", resp.ManagerVersion)
		d.Set("store_path", resp.StagedStorePath)
	}
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
