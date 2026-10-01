package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceClusterUpdate() *schema.Resource {
	return &schema.Resource{
		Description:   "Host OS rollout (`kctl update-cluster`). Destroy cancels the rollout.",
		CreateContext: resourceClusterUpdateCreate,
		ReadContext:   resourceClusterUpdateRead,
		DeleteContext: resourceClusterUpdateDelete,
		Schema: map[string]*schema.Schema{
			"name":             {Type: schema.TypeString, Required: true, ForceNew: true},
			"version":          {Type: schema.TypeString, Required: true, ForceNew: true},
			"flake_ref":        {Type: schema.TypeString, Required: true, ForceNew: true},
			"flake_rev":        {Type: schema.TypeString, Optional: true, ForceNew: true},
			"system_profile":   {Type: schema.TypeString, Optional: true, ForceNew: true},
			"node_ids":         {Type: schema.TypeList, Optional: true, ForceNew: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"labels":           {Type: schema.TypeList, Optional: true, ForceNew: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"all_nodes":        {Type: schema.TypeBool, Optional: true, ForceNew: true},
			"controllers_only": {Type: schema.TypeBool, Optional: true, ForceNew: true},
			"drain_vms":        {Type: schema.TypeBool, Optional: true, ForceNew: true},
			"strategy": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Default:      "one-at-a-time",
				ValidateFunc: validation.StringInSlice([]string{"canary", "one-at-a-time", "batch", "per-dc"}, false),
			},
			"approval": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Default:      "manual",
				ValidateFunc: validation.StringInSlice([]string{"manual", "auto-non-disruptive", "auto"}, false),
			},
			"approve": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Description: "Call approve after create when approval is manual.",
			},
			"phase":           {Type: schema.TypeString, Computed: true},
			"approval_status": {Type: schema.TypeString, Computed: true},
		},
	}
}

func updateStrategy(name string) pb.ClusterUpdateStrategyType {
	switch name {
	case "canary":
		return pb.ClusterUpdateStrategyType_CLUSTER_UPDATE_STRATEGY_CANARY
	case "batch":
		return pb.ClusterUpdateStrategyType_CLUSTER_UPDATE_STRATEGY_BATCH
	case "per-dc":
		return pb.ClusterUpdateStrategyType_CLUSTER_UPDATE_STRATEGY_PER_DC
	default:
		return pb.ClusterUpdateStrategyType_CLUSTER_UPDATE_STRATEGY_ONE_AT_A_TIME
	}
}

func updateApproval(name string) pb.ClusterUpdateApprovalPolicy {
	switch name {
	case "auto-non-disruptive":
		return pb.ClusterUpdateApprovalPolicy_CLUSTER_UPDATE_APPROVAL_AUTO_NON_DISRUPTIVE
	case "auto":
		return pb.ClusterUpdateApprovalPolicy_CLUSTER_UPDATE_APPROVAL_AUTO
	default:
		return pb.ClusterUpdateApprovalPolicy_CLUSTER_UPDATE_APPROVAL_MANUAL
	}
}

func clusterUpdateSpec(d *schema.ResourceData) *pb.ClusterUpdateSpec {
	return &pb.ClusterUpdateSpec{
		Name: s(d, "name"),
		Target: &pb.ClusterUpdateTarget{
			Version:       s(d, "version"),
			FlakeRef:      s(d, "flake_ref"),
			FlakeRev:      s(d, "flake_rev"),
			SystemProfile: s(d, "system_profile"),
		},
		Selector: &pb.ClusterUpdateSelector{
			NodeIds:         strList(d, "node_ids"),
			Labels:          strList(d, "labels"),
			AllNodes:        b(d, "all_nodes"),
			ControllersOnly: b(d, "controllers_only"),
		},
		Strategy:       &pb.ClusterUpdateStrategy{Type: updateStrategy(s(d, "strategy"))},
		DrainVms:       b(d, "drain_vms"),
		ApprovalPolicy: updateApproval(s(d, "approval")),
	}
}

func resourceClusterUpdateCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c := api(meta).controller
	resp, err := c.CreateClusterUpdate(ctx, &pb.CreateClusterUpdateRequest{Spec: clusterUpdateSpec(d)})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	if b(d, "approve") {
		ap, err := c.ApproveClusterUpdate(ctx, &pb.ApproveClusterUpdateRequest{Name: s(d, "name")})
		if err != nil {
			return diag.FromErr(err)
		}
		if err := fail(ap.Success, ""); err != nil {
			return diag.FromErr(err)
		}
	}
	d.SetId(s(d, "name"))
	return resourceClusterUpdateRead(ctx, d, meta)
}

func resourceClusterUpdateRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetClusterUpdate(ctx, &pb.GetClusterUpdateRequest{Name: d.Id()})
	if err != nil || resp.GetClusterUpdate() == nil || resp.ClusterUpdate.GetSpec() == nil {
		d.SetId("")
		return nil
	}
	d.Set("phase", resp.ClusterUpdate.Phase.String())
	d.Set("approval_status", resp.ClusterUpdate.ApprovalStatus.String())
	return nil
}

func resourceClusterUpdateDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CancelClusterUpdate(ctx, &pb.CancelClusterUpdateRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceWorkload() *schema.Resource {
	return &schema.Resource{
		Description:   "Container workload (`kctl workload`). VMs should use kcore_vm.",
		CreateContext: resourceWorkloadCreate,
		ReadContext:   resourceWorkloadRead,
		UpdateContext: resourceWorkloadUpdate,
		DeleteContext: resourceWorkloadDelete,
		Schema: map[string]*schema.Schema{
			"name":          {Type: schema.TypeString, Required: true, ForceNew: true},
			"image":         {Type: schema.TypeString, Required: true, ForceNew: true},
			"network":       {Type: schema.TypeString, Optional: true, ForceNew: true},
			"command":       {Type: schema.TypeList, Optional: true, ForceNew: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"env":           {Type: schema.TypeMap, Optional: true, ForceNew: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"ports":         {Type: schema.TypeList, Optional: true, ForceNew: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"target_node":   {Type: schema.TypeString, Optional: true, ForceNew: true},
			"desired_state": {Type: schema.TypeString, Optional: true, Default: "running", ValidateFunc: validation.StringInSlice([]string{"running", "stopped"}, false)},
			"state":         {Type: schema.TypeString, Computed: true},
			"node_id":       {Type: schema.TypeString, Computed: true},
		},
	}
}

func workloadDesired(name string) pb.WorkloadDesiredState {
	if name == "stopped" {
		return pb.WorkloadDesiredState_WORKLOAD_DESIRED_STATE_STOPPED
	}
	return pb.WorkloadDesiredState_WORKLOAD_DESIRED_STATE_RUNNING
}

func resourceWorkloadCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	env := map[string]string{}
	if raw, ok := d.GetOk("env"); ok {
		for k, v := range raw.(map[string]interface{}) {
			env[k] = fmt.Sprint(v)
		}
	}
	resp, err := api(meta).controller.CreateWorkload(ctx, &pb.CreateWorkloadRequest{
		Kind:       pb.WorkloadKind_WORKLOAD_KIND_CONTAINER,
		TargetNode: s(d, "target_node"),
		ContainerSpec: &pb.ContainerSpec{
			Name:         s(d, "name"),
			Image:        s(d, "image"),
			Network:      s(d, "network"),
			Command:      strList(d, "command"),
			Env:          env,
			Ports:        strList(d, "ports"),
			DesiredState: workloadDesired(s(d, "desired_state")),
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}
	id := resp.GetWorkloadId()
	if id == "" {
		id = s(d, "name")
	}
	d.SetId(id)
	return resourceWorkloadRead(ctx, d, meta)
}

func resourceWorkloadRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetWorkload(ctx, &pb.GetWorkloadRequest{
		Kind:       pb.WorkloadKind_WORKLOAD_KIND_CONTAINER,
		WorkloadId: d.Id(),
	})
	if err != nil || resp == nil || (resp.ContainerInfo == nil && resp.NodeId == "") {
		d.SetId("")
		return nil
	}
	d.Set("node_id", resp.NodeId)
	if info := resp.ContainerInfo; info != nil {
		if info.NodeId != "" {
			d.Set("node_id", info.NodeId)
		}
		if info.State != pb.ContainerState_CONTAINER_STATE_UNKNOWN {
			d.Set("state", info.State.String())
		}
	}
	return nil
}

func resourceWorkloadUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	if d.HasChange("desired_state") {
		_, err := api(meta).controller.SetWorkloadDesiredState(ctx, &pb.SetWorkloadDesiredStateRequest{
			Kind:         pb.WorkloadKind_WORKLOAD_KIND_CONTAINER,
			WorkloadId:   d.Id(),
			DesiredState: workloadDesired(s(d, "desired_state")),
		})
		if err != nil {
			return diag.FromErr(err)
		}
	}
	return resourceWorkloadRead(ctx, d, meta)
}

func resourceWorkloadDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_, err := api(meta).controller.DeleteWorkload(ctx, &pb.DeleteWorkloadRequest{
		Kind:       pb.WorkloadKind_WORKLOAD_KIND_CONTAINER,
		WorkloadId: d.Id(),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
