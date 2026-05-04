package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
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
			"storage_backend": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "VM storage backend for compatibility checks (filesystem, lvm, zfs)",
				ValidateFunc: validation.StringInSlice([]string{"filesystem", "file", "lvm", "zfs"}, false),
			},
			"storage_size_bytes": {
				Type:         schema.TypeInt,
				Required:     true,
				Description:  "Provisioned root volume size in bytes",
				ValidateFunc: validation.IntAtLeast(1),
			},
			"target_node": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Target node to create the VM on (optional, controller will schedule if not specified)",
			},
			"target_dc": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Datacenter preference for scheduling (optional)",
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
			"image_url": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "HTTPS URL of the VM disk image to download on the target node (use with image_sha256)",
			},
			"image_sha256": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "SHA256 checksum when using image_url",
			},
			"image_path": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Node-local image path (alternative to image_url/image_sha256)",
			},
			"image_format": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Description:  "Format when using image_path (qcow2 or raw)",
				ValidateFunc: validation.StringInSlice([]string{"qcow2", "raw"}, false),
			},
			"cloud_init_user_data": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Optional cloud-init user-data (YAML)",
			},
			"ssh_key_names": {
				Type:        schema.TypeList,
				Optional:    true,
				ForceNew:    true,
				Description: "SSH key names registered with the controller to inject into the guest",
				Elem:        &schema.Schema{Type: schema.TypeString},
			},
			"desired_state": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "running",
				Description:  "Desired state of the VM (running or stopped)",
				ValidateFunc: validation.StringInSlice([]string{"running", "stopped"}, false),
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
			"assigned_ip": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Controller-assigned VM IP when available",
			},
			"created_at": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Creation timestamp",
			},
		},
	}
}

func vmOptionalTargetNode(d *schema.ResourceData) string {
	if v, ok := d.GetOk("target_node"); ok {
		return strings.TrimSpace(v.(string))
	}
	return ""
}

func resourceVMCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)

	imageURL := strings.TrimSpace(d.Get("image_url").(string))
	imageSHA := strings.TrimSpace(d.Get("image_sha256").(string))
	imagePath := strings.TrimSpace(d.Get("image_path").(string))
	imageFormat := strings.TrimSpace(d.Get("image_format").(string))

	useURL := imageURL != "" && imageSHA != ""
	usePath := imagePath != ""
	if useURL == usePath {
		if !useURL && !usePath {
			return diag.FromErr(fmt.Errorf("provide either (image_url and image_sha256) or image_path"))
		}
		return diag.FromErr(fmt.Errorf("provide either URL-based (image_url + image_sha256) or path-based (image_path) image fields, not both"))
	}
	if usePath && imageFormat == "" {
		return diag.FromErr(fmt.Errorf("image_format is required when image_path is set (qcow2 or raw)"))
	}

	backend := parseStorageBackend(d.Get("storage_backend").(string))
	if backend == pb.StorageBackendType_STORAGE_BACKEND_TYPE_UNSPECIFIED {
		return diag.FromErr(fmt.Errorf("invalid storage_backend (use filesystem, lvm, or zfs)"))
	}

	sizeBytes := int64(d.Get("storage_size_bytes").(int))
	spec := &pb.VmSpec{
		Name:             d.Get("name").(string),
		Cpu:              int32(d.Get("cpu").(int)),
		MemoryBytes:      int64(d.Get("memory_bytes").(int)),
		StorageBackend:   normalizeStorageBackendSpec(d.Get("storage_backend").(string)),
		StorageSizeBytes: sizeBytes,
		DesiredState:     vmDesiredStateProto(d.Get("desired_state").(string)),
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
		Spec:             spec,
		StorageBackend:   backend,
		StorageSizeBytes: sizeBytes,
	}

	if useURL {
		req.ImageUrl = imageURL
		req.ImageSha256 = imageSHA
	} else {
		req.ImagePath = imagePath
		req.ImageFormat = imageFormat
	}

	if v, ok := d.GetOk("cloud_init_user_data"); ok {
		req.CloudInitUserData = v.(string)
	}

	if v, ok := d.GetOk("ssh_key_names"); ok {
		raw := v.([]interface{})
		for _, x := range raw {
			req.SshKeyNames = append(req.SshKeyNames, x.(string))
		}
	}

	if tn := vmOptionalTargetNode(d); tn != "" {
		req.TargetNode = tn
	}

	if v, ok := d.GetOk("target_dc"); ok {
		req.TargetDc = strings.TrimSpace(v.(string))
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
	if tn := vmOptionalTargetNode(d); tn != "" {
		req.TargetNode = tn
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
	d.Set("assigned_ip", resp.AssignedIp)

	if resp.Spec.StorageBackend != "" {
		d.Set("storage_backend", normalizeStorageBackendSpec(resp.Spec.StorageBackend))
	}
	d.Set("storage_size_bytes", resp.Spec.StorageSizeBytes)

	if ds := vmDesiredStateTerraform(resp.Spec.DesiredState); ds != "" {
		d.Set("desired_state", ds)
	}

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
	client := meta.(*apiClient)

	if d.HasChange("desired_state") {
		desiredState := d.Get("desired_state").(string)
		req := &pb.SetVmDesiredStateRequest{
			VmId:         d.Id(),
			DesiredState: vmDesiredStateProto(desiredState),
		}
		if tn := vmOptionalTargetNode(d); tn != "" {
			req.TargetNode = tn
		}

		_, err := client.controller.SetVmDesiredState(ctx, req)
		if err != nil {
			return diag.FromErr(fmt.Errorf("failed to set VM desired state: %w", err))
		}
	}

	return resourceVMRead(ctx, d, meta)
}

func resourceVMDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*apiClient)
	var diags diag.Diagnostics

	vmID := d.Id()

	req := &pb.DeleteVmRequest{
		VmId: vmID,
	}
	if tn := vmOptionalTargetNode(d); tn != "" {
		req.TargetNode = tn
	}

	_, err := client.controller.DeleteVm(ctx, req)
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to delete VM: %w", err))
	}

	d.SetId("")
	return diags
}
