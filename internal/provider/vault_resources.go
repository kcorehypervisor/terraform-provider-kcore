package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	vaultpb "github.com/kcorehypervisor/terraform-provider-kcore/api/vault"
)

func resourceVaultTarget() *schema.Resource {
	return &schema.Resource{
		CreateContext: vaultTargetApply,
		ReadContext:   vaultTargetRead,
		UpdateContext: vaultTargetApply,
		DeleteContext: vaultTargetDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"backend": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "fs",
			},
			"location": {
				Type:     schema.TypeString,
				Required: true,
			},
			"disable_object_lock": {
				Type:     schema.TypeBool,
				Optional: true,
			},
		},
	}
}

func vaultTargetApply(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.ApplyTarget(ctx, &vaultpb.ApplyTargetRequest{
		Target: &vaultpb.Target{
			Name:              d.Get("name").(string),
			Backend:           d.Get("backend").(string),
			Location:          d.Get("location").(string),
			DisableObjectLock: d.Get("disable_object_lock").(bool),
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if !resp.GetSuccess() {
		return diag.FromErr(fmt.Errorf("apply target: %s", resp.GetMessage()))
	}
	d.SetId(d.Get("name").(string))
	return vaultTargetRead(ctx, d, meta)
}

func vaultTargetRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.GetTarget(ctx, &vaultpb.GetTargetRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	t := resp.GetTarget()
	if t == nil || t.GetName() == "" {
		d.SetId("")
		return nil
	}
	_ = d.Set("name", t.GetName())
	_ = d.Set("backend", t.GetBackend())
	_ = d.Set("location", t.GetLocation())
	_ = d.Set("disable_object_lock", t.GetDisableObjectLock())
	return nil
}

func vaultTargetDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.DeleteTarget(ctx, &vaultpb.DeleteTargetRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if !resp.GetSuccess() {
		return diag.FromErr(fmt.Errorf("delete target: %s", resp.GetMessage()))
	}
	d.SetId("")
	return nil
}

func resourceVaultPolicy() *schema.Resource {
	return &schema.Resource{
		CreateContext: vaultPolicyApply,
		ReadContext:   vaultPolicyRead,
		UpdateContext: vaultPolicyApply,
		DeleteContext: vaultPolicyDelete,
		Schema: map[string]*schema.Schema{
			"name":         {Type: schema.TypeString, Required: true, ForceNew: true},
			"schedule":     {Type: schema.TypeString, Optional: true, Default: "@daily"},
			"target":       {Type: schema.TypeString, Required: true},
			"secondary":    {Type: schema.TypeString, Optional: true},
			"vms":          {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"keep_hourly":  {Type: schema.TypeInt, Optional: true},
			"keep_daily":   {Type: schema.TypeInt, Optional: true},
			"keep_weekly":  {Type: schema.TypeInt, Optional: true},
			"keep_monthly": {Type: schema.TypeInt, Optional: true},
			"keep_yearly":  {Type: schema.TypeInt, Optional: true},
			"keep_recent":  {Type: schema.TypeInt, Optional: true},
			"pre_hook":     {Type: schema.TypeString, Optional: true},
			"post_hook":    {Type: schema.TypeString, Optional: true},
			"enabled":      {Type: schema.TypeBool, Optional: true, Default: true},
		},
	}
}

func vaultPolicyApply(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	keep := &vaultpb.RetentionKeep{
		Hourly:  toInt32(d.Get("keep_hourly").(int)),
		Daily:   toInt32(d.Get("keep_daily").(int)),
		Weekly:  toInt32(d.Get("keep_weekly").(int)),
		Monthly: toInt32(d.Get("keep_monthly").(int)),
		Yearly:  toInt32(d.Get("keep_yearly").(int)),
		Recent:  toInt32(d.Get("keep_recent").(int)),
	}
	var vms []string
	for _, raw := range d.Get("vms").([]interface{}) {
		vms = append(vms, raw.(string))
	}
	targets := []*vaultpb.TargetRetention{{
		TargetName: d.Get("target").(string),
		Keep:       keep,
	}}
	if secondary := d.Get("secondary").(string); secondary != "" {
		targets = append(targets, &vaultpb.TargetRetention{TargetName: secondary, Keep: keep})
	}
	resp, err := c.ApplyBackupPolicy(ctx, &vaultpb.ApplyBackupPolicyRequest{
		Policy: &vaultpb.BackupPolicy{
			Name:            d.Get("name").(string),
			Schedule:        d.Get("schedule").(string),
			Consistency:     vaultpb.ConsistencyLevel_CONSISTENCY_LEVEL_FS,
			SelectorVmNames: vms,
			PreHook:         d.Get("pre_hook").(string),
			PostHook:        d.Get("post_hook").(string),
			Targets:         targets,
			Enabled:         d.Get("enabled").(bool),
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if !resp.GetSuccess() {
		return diag.FromErr(fmt.Errorf("apply policy: %s", resp.GetMessage()))
	}
	d.SetId(d.Get("name").(string))
	return vaultPolicyRead(ctx, d, meta)
}

func vaultPolicyRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.GetBackupPolicy(ctx, &vaultpb.GetBackupPolicyRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	p := resp.GetPolicy()
	if p == nil || p.GetName() == "" {
		d.SetId("")
		return nil
	}
	_ = d.Set("name", p.GetName())
	_ = d.Set("schedule", p.GetSchedule())
	_ = d.Set("enabled", p.GetEnabled())
	_ = d.Set("vms", p.GetSelectorVmNames())
	_ = d.Set("pre_hook", p.GetPreHook())
	_ = d.Set("post_hook", p.GetPostHook())
	if len(p.GetTargets()) > 0 {
		_ = d.Set("target", p.GetTargets()[0].GetTargetName())
		if keep := p.GetTargets()[0].GetKeep(); keep != nil {
			_ = d.Set("keep_hourly", keep.GetHourly())
			_ = d.Set("keep_daily", keep.GetDaily())
			_ = d.Set("keep_weekly", keep.GetWeekly())
			_ = d.Set("keep_monthly", keep.GetMonthly())
			_ = d.Set("keep_yearly", keep.GetYearly())
			_ = d.Set("keep_recent", keep.GetRecent())
		}
	}
	if len(p.GetTargets()) > 1 {
		_ = d.Set("secondary", p.GetTargets()[1].GetTargetName())
	}
	return nil
}

func vaultPolicyDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.DeleteBackupPolicy(ctx, &vaultpb.DeleteBackupPolicyRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if !resp.GetSuccess() {
		return diag.FromErr(fmt.Errorf("delete policy: %s", resp.GetMessage()))
	}
	d.SetId("")
	return nil
}

func resourceVaultReplication() *schema.Resource {
	return &schema.Resource{
		CreateContext: vaultReplicationCreate,
		ReadContext:   vaultReplicationRead,
		DeleteContext: vaultReplicationDelete,
		Schema: map[string]*schema.Schema{
			"source": {Type: schema.TypeString, Required: true, ForceNew: true},
			"dest":   {Type: schema.TypeString, Required: true, ForceNew: true},
			"phase":  {Type: schema.TypeInt, Computed: true},
			"bytes":  {Type: schema.TypeInt, Computed: true},
		},
	}
}

func vaultReplicationCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.CreateReplicationJob(ctx, &vaultpb.CreateReplicationJobRequest{
		SourceTarget: d.Get("source").(string),
		DestTarget:   d.Get("dest").(string),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if !resp.GetSuccess() || resp.GetJob() == nil {
		return diag.FromErr(fmt.Errorf("replicate: %s", resp.GetMessage()))
	}
	d.SetId(resp.GetJob().GetId())
	return vaultReplicationRead(ctx, d, meta)
}

func vaultReplicationRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.GetReplicationJob(ctx, &vaultpb.GetReplicationJobRequest{Id: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	job := resp.GetJob()
	if job == nil || job.GetId() == "" {
		d.SetId("")
		return nil
	}
	_ = d.Set("source", job.GetSourceTarget())
	_ = d.Set("dest", job.GetDestTarget())
	_ = d.Set("phase", int(job.GetPhase()))
	_ = d.Set("bytes", int(job.GetBytesCopied()))
	return nil
}

func vaultReplicationDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c, err := meta.(*apiClient).requireVault()
	if err != nil {
		return diag.FromErr(err)
	}
	resp, err := c.DeleteReplicationJob(ctx, &vaultpb.DeleteReplicationJobRequest{Id: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if !resp.GetSuccess() {
		return diag.FromErr(fmt.Errorf("delete replication: %s", resp.GetMessage()))
	}
	d.SetId("")
	return nil
}
