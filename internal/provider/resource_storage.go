package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceVolume() *schema.Resource {
	return &schema.Resource{
		Description:   "Data volume (`kctl create volume`). Size changes call resize.",
		CreateContext: resourceVolumeCreate,
		ReadContext:   resourceVolumeRead,
		UpdateContext: resourceVolumeUpdate,
		DeleteContext: resourceVolumeDelete,
		Schema: map[string]*schema.Schema{
			"name":           {Type: schema.TypeString, Required: true, ForceNew: true},
			"size_bytes":     {Type: schema.TypeInt, Required: true},
			"storage_class":  {Type: schema.TypeString, Optional: true, ForceNew: true, Default: "ceph"},
			"vm":             {Type: schema.TypeString, Optional: true, Description: "VM to attach. The VM must be stopped."},
			"from_snapshot":  {Type: schema.TypeString, Optional: true, ForceNew: true},
			"encrypt":        {Type: schema.TypeBool, Optional: true, ForceNew: true},
			"id_internal":    {Type: schema.TypeString, Computed: true},
			"backend_handle": {Type: schema.TypeString, Computed: true},
			"attach_state":   {Type: schema.TypeString, Computed: true},
			"pool":           {Type: schema.TypeString, Computed: true},
		},
	}
}

func resourceVolumeCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateVolume(ctx, &pb.CreateVolumeRequest{
		Name:         s(d, "name"),
		SizeBytes:    i64(d, "size_bytes"),
		StorageClass: s(d, "storage_class"),
		Vm:           s(d, "vm"),
		FromSnapshot: s(d, "from_snapshot"),
		Encrypt:      b(d, "encrypt"),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceVolumeRead(ctx, d, meta)
}

func resourceVolumeRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetVolume(ctx, &pb.GetVolumeRequest{Name: d.Id()})
	if err != nil || resp.GetVolume() == nil || resp.Volume.Name == "" {
		d.SetId("")
		return nil
	}
	v := resp.Volume
	d.Set("name", v.Name)
	d.Set("size_bytes", v.StorageSizeBytes)
	d.Set("id_internal", v.Id)
	d.Set("backend_handle", v.BackendHandle)
	d.Set("attach_state", v.AttachState)
	d.Set("pool", v.Pool)
	if v.VmName != "" {
		d.Set("vm", v.VmName)
	}
	return nil
}

func resourceVolumeUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c := api(meta).controller
	if d.HasChange("size_bytes") {
		resp, err := c.ResizeVolume(ctx, &pb.ResizeVolumeRequest{
			Name:      d.Id(),
			SizeBytes: i64(d, "size_bytes"),
		})
		if err != nil {
			return diag.FromErr(err)
		}
		if err := fail(resp.Success, resp.Message); err != nil {
			return diag.FromErr(err)
		}
	}
	if d.HasChange("vm") {
		old, newV := d.GetChange("vm")
		if old.(string) != "" {
			resp, err := c.DetachVolume(ctx, &pb.DetachVolumeRequest{Name: d.Id()})
			if err != nil {
				return diag.FromErr(err)
			}
			if err := fail(resp.Success, resp.Message); err != nil {
				return diag.FromErr(err)
			}
		}
		if newV.(string) != "" {
			resp, err := c.AttachVolume(ctx, &pb.AttachVolumeRequest{Name: d.Id(), Vm: newV.(string)})
			if err != nil {
				return diag.FromErr(err)
			}
			if err := fail(resp.Success, resp.Message); err != nil {
				return diag.FromErr(err)
			}
		}
	}
	return resourceVolumeRead(ctx, d, meta)
}

func resourceVolumeDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteVolume(ctx, &pb.DeleteVolumeRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceVolumeSnapshot() *schema.Resource {
	return &schema.Resource{
		Description:   "Volume snapshot (`kctl create volume-snapshot`).",
		CreateContext: resourceVolSnapCreate,
		ReadContext:   resourceVolSnapRead,
		DeleteContext: resourceVolSnapDelete,
		Schema: map[string]*schema.Schema{
			"volume":      {Type: schema.TypeString, Required: true, ForceNew: true},
			"name":        {Type: schema.TypeString, Required: true, ForceNew: true},
			"consistency": {Type: schema.TypeString, Optional: true, ForceNew: true, Default: "crash"},
			"size_bytes":  {Type: schema.TypeInt, Computed: true},
			"protected":   {Type: schema.TypeBool, Computed: true},
		},
	}
}

func resourceVolSnapCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateVolumeSnapshot(ctx, &pb.CreateVolumeSnapshotRequest{
		Volume:      s(d, "volume"),
		Name:        s(d, "name"),
		Consistency: s(d, "consistency"),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceVolSnapRead(ctx, d, meta)
}

func resourceVolSnapRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.ListVolumeSnapshots(ctx, &pb.ListVolumeSnapshotsRequest{Volume: s(d, "volume")})
	if err != nil {
		return diag.FromErr(err)
	}
	for _, snap := range resp.Snapshots {
		if snap.Name == d.Id() || snap.Id == d.Id() {
			d.Set("name", snap.Name)
			d.Set("size_bytes", snap.SizeBytes)
			d.Set("protected", snap.Protected)
			return nil
		}
	}
	d.SetId("")
	return nil
}

func resourceVolSnapDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteVolumeSnapshot(ctx, &pb.DeleteVolumeSnapshotRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceSnapshotPolicy() *schema.Resource {
	return &schema.Resource{
		Description:   "Snapshot schedule (`kctl create snapshot-policy`).",
		CreateContext: resourceSnapPolUpsert,
		ReadContext:   resourceSnapPolRead,
		UpdateContext: resourceSnapPolUpsert,
		DeleteContext: resourceSnapPolDelete,
		Schema: map[string]*schema.Schema{
			"name":            {Type: schema.TypeString, Required: true, ForceNew: true},
			"selector_vm":     {Type: schema.TypeString, Optional: true},
			"selector_volume": {Type: schema.TypeString, Optional: true},
			"schedule":        {Type: schema.TypeString, Required: true, Description: "@hourly, @daily, or every:<seconds>"},
			"keep":            {Type: schema.TypeInt, Required: true},
			"enabled":         {Type: schema.TypeBool, Optional: true, Default: true},
			"last_message":    {Type: schema.TypeString, Computed: true},
		},
	}
}

func resourceSnapPolUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateSnapshotPolicy(ctx, &pb.CreateSnapshotPolicyRequest{
		Policy: &pb.SnapshotPolicy{
			Name:           s(d, "name"),
			SelectorVm:     s(d, "selector_vm"),
			SelectorVolume: s(d, "selector_volume"),
			Schedule:       s(d, "schedule"),
			Keep:           i32(d, "keep"),
			Enabled:        b(d, "enabled"),
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceSnapPolRead(ctx, d, meta)
}

func resourceSnapPolRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetSnapshotPolicy(ctx, &pb.GetSnapshotPolicyRequest{Name: d.Id()})
	if err != nil || resp.GetPolicy() == nil || resp.Policy.Name == "" {
		d.SetId("")
		return nil
	}
	p := resp.Policy
	d.Set("schedule", p.Schedule)
	d.Set("keep", p.Keep)
	d.Set("enabled", p.Enabled)
	d.Set("selector_vm", p.SelectorVm)
	d.Set("selector_volume", p.SelectorVolume)
	d.Set("last_message", p.LastMessage)
	return nil
}

func resourceSnapPolDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteSnapshotPolicy(ctx, &pb.DeleteSnapshotPolicyRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceDiskLayout() *schema.Resource {
	return &schema.Resource{
		Description:   "Node disk layout (`kctl create disk-layout`). layout_nix is a disko.devices expression.",
		CreateContext: resourceDiskLayoutUpsert,
		ReadContext:   resourceDiskLayoutRead,
		UpdateContext: resourceDiskLayoutUpsert,
		DeleteContext: resourceDiskLayoutDelete,
		Schema: map[string]*schema.Schema{
			"name":       {Type: schema.TypeString, Required: true, ForceNew: true},
			"node_id":    {Type: schema.TypeString, Required: true, ForceNew: true},
			"layout_nix": {Type: schema.TypeString, Required: true},
			"evacuate":   {Type: schema.TypeBool, Optional: true},
			"phase":      {Type: schema.TypeString, Computed: true},
			"message":    {Type: schema.TypeString, Computed: true},
		},
	}
}

func resourceDiskLayoutUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateDiskLayout(ctx, &pb.CreateDiskLayoutRequest{
		DiskLayout: &pb.DiskLayout{
			Name:      s(d, "name"),
			NodeId:    s(d, "node_id"),
			LayoutNix: s(d, "layout_nix"),
			Evacuate:  b(d, "evacuate"),
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceDiskLayoutRead(ctx, d, meta)
}

func resourceDiskLayoutRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetDiskLayout(ctx, &pb.GetDiskLayoutRequest{Name: d.Id()})
	if err != nil || resp.GetDiskLayout() == nil || resp.DiskLayout.Name == "" {
		d.SetId("")
		return nil
	}
	d.Set("layout_nix", resp.DiskLayout.LayoutNix)
	d.Set("evacuate", resp.DiskLayout.Evacuate)
	if resp.Status != nil {
		d.Set("phase", resp.Status.Phase.String())
		d.Set("message", resp.Status.Message)
	}
	return nil
}

func resourceDiskLayoutDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteDiskLayout(ctx, &pb.DeleteDiskLayoutRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
