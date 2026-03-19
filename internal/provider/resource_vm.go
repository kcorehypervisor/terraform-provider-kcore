package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/rtacconi/terraform-provider-kcore/api/controller"
)

func resourceVM() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceVMCreate,
		ReadContext:   resourceVMRead,
		UpdateContext: resourceVMUpdate,
		DeleteContext: resourceVMDelete,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(10 * time.Minute),
			Update: schema.DefaultTimeout(10 * time.Minute),
			Delete: schema.DefaultTimeout(10 * time.Minute),
		},
		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "Name of the VM",
				ValidateFunc: validation.StringLenBetween(1, 253),
			},
			"cpu": {
				Type:         schema.TypeInt,
				Required:     true,
				Description:  "Number of CPU cores",
				ValidateFunc: validation.IntAtLeast(1),
			},
			"memory_bytes": {
				Type:         schema.TypeInt,
				Required:     true,
				Description:  "Memory in bytes",
				ValidateFunc: validation.IntAtLeast(1024 * 1024),
			},
			"target_node": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Target node to create the VM on (optional, controller will schedule if not specified)",
			},
			"disk": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "Disk configuration",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Disk name",
						},
						"backend_handle": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Storage backend handle/path",
						},
						"bus": {
							Type:         schema.TypeString,
							Optional:     true,
							Default:      "virtio",
							Description:  "Disk bus type (virtio, scsi, ide, sata)",
							ValidateFunc: validation.StringInSlice([]string{"virtio", "scsi", "ide", "sata"}, false),
						},
						"device": {
							Type:        schema.TypeString,
							Optional:    true,
							Default:     "disk",
							Description: "Device type (disk, cdrom)",
						},
					},
				},
			},
			"nic": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "Network interface configuration",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"network": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Network name",
						},
						"model": {
							Type:         schema.TypeString,
							Optional:     true,
							Default:      "virtio",
							Description:  "NIC model (virtio, e1000, rtl8139)",
							ValidateFunc: validation.StringInSlice([]string{"virtio", "e1000", "rtl8139"}, false),
						},
						"mac_address": {
							Type:        schema.TypeString,
							Optional:    true,
							Computed:    true,
							Description: "MAC address (auto-generated if not specified)",
						},
					},
				},
			},
			"state": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Current state of the VM",
			},
			"node_id": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Node ID where the VM is running",
			},
			"created_at": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Creation timestamp",
			},
		},
	}
}

func resourceVMCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)

	spec := &pb.VmSpec{
		Name:        d.Get("name").(string),
		Cpu:         int32(d.Get("cpu").(int)),
		MemoryBytes: int64(d.Get("memory_bytes").(int)),
	}

	if v, ok := d.GetOk("disk"); ok {
		disks := v.([]interface{})
		for _, disk := range disks {
			diskMap := disk.(map[string]interface{})
			spec.Disks = append(spec.Disks, &pb.Disk{
				Name:          diskMap["name"].(string),
				BackendHandle: diskMap["backend_handle"].(string),
				Bus:           diskMap["bus"].(string),
				Device:        diskMap["device"].(string),
			})
		}
	}

	if v, ok := d.GetOk("nic"); ok {
		nics := v.([]interface{})
		for _, nic := range nics {
			nicMap := nic.(map[string]interface{})
			pbNic := &pb.Nic{
				Network: nicMap["network"].(string),
				Model:   nicMap["model"].(string),
			}
			if mac, ok := nicMap["mac_address"].(string); ok && mac != "" {
				pbNic.MacAddress = mac
			}
			spec.Nics = append(spec.Nics, pbNic)
		}
	}

	req := &pb.CreateVmRequest{
		Spec: spec,
	}

	if targetNode, ok := d.GetOk("target_node"); ok {
		req.TargetNode = targetNode.(string)
	}

	resp, err := client.controller.CreateVm(ctx, req)
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to create VM: %w", err))
	}

	d.SetId(resp.VmId)

	return resourceVMRead(ctx, d, meta)
}

func resourceVMRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)
	var diags diag.Diagnostics

	vmID := d.Id()

	req := &pb.GetVmRequest{
		VmId: vmID,
	}

	resp, err := client.controller.GetVm(ctx, req)
	if err != nil {
		d.SetId("")
		return diags
	}

	d.Set("name", resp.Spec.Name)
	d.Set("cpu", resp.Spec.Cpu)
	d.Set("memory_bytes", resp.Spec.MemoryBytes)
	d.Set("node_id", resp.NodeId)
	d.Set("state", resp.Status.State.String())

	if resp.Status.CreatedAt != nil {
		d.Set("created_at", resp.Status.CreatedAt.AsTime().Format(time.RFC3339))
	}

	if len(resp.Spec.Disks) > 0 {
		disks := make([]map[string]interface{}, len(resp.Spec.Disks))
		for i, disk := range resp.Spec.Disks {
			disks[i] = map[string]interface{}{
				"name":           disk.Name,
				"backend_handle": disk.BackendHandle,
				"bus":            disk.Bus,
				"device":         disk.Device,
			}
		}
		d.Set("disk", disks)
	}

	if len(resp.Spec.Nics) > 0 {
		nics := make([]map[string]interface{}, len(resp.Spec.Nics))
		for i, nic := range resp.Spec.Nics {
			nics[i] = map[string]interface{}{
				"network":     nic.Network,
				"model":       nic.Model,
				"mac_address": nic.MacAddress,
			}
		}
		d.Set("nic", nics)
	}

	return diags
}

func resourceVMUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return resourceVMRead(ctx, d, meta)
}

func resourceVMDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)
	var diags diag.Diagnostics

	vmID := d.Id()

	req := &pb.DeleteVmRequest{
		VmId: vmID,
	}

	_, err := client.controller.DeleteVm(ctx, req)
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to delete VM: %w", err))
	}

	d.SetId("")
	return diags
}
