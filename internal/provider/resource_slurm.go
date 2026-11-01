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

func resourceSlurmCluster() *schema.Resource {
	return &schema.Resource{
		Description: "Slurm cluster on kcoreOS nodes (`kctl apply -f` with `kind: SlurmCluster`). Compute members are cordoned so the VM scheduler leaves them to slurmd. A node belongs to at most one cluster. " +
			"Changing `name`, `cluster_name`, or the controller node replaces the cluster. Destroy disables Slurm on the members and uncordons the compute nodes.",
		CreateContext: resourceSlurmClusterUpsert,
		ReadContext:   resourceSlurmClusterRead,
		UpdateContext: resourceSlurmClusterUpsert,
		DeleteContext: resourceSlurmClusterDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(90 * time.Minute),
			Update: schema.DefaultTimeout(90 * time.Minute),
			Delete: schema.DefaultTimeout(30 * time.Minute),
		},
		Schema: slurmClusterSchema(),
	}
}

func slurmMemberSchema(nodeForcesNew bool) map[string]*schema.Schema {
	return map[string]*schema.Schema{
		"node_id": {Type: schema.TypeString, Required: true, ForceNew: nodeForcesNew, Description: "kcore node id."},
		"address": {Type: schema.TypeString, Required: true, Description: "Management address other members use to reach this node."},
		"iface":   {Type: schema.TypeString, Required: true, Description: "Management interface, for example enp6s0."},
	}
}

func slurmClusterSchema() map[string]*schema.Schema {
	compute := slurmMemberSchema(false)
	compute["partitions"] = &schema.Schema{
		Type:        schema.TypeList,
		Optional:    true,
		Elem:        &schema.Schema{Type: schema.TypeString},
		Description: "Partitions this node serves. Each must be declared in a partition block.",
	}
	compute["reserve_cores"] = &schema.Schema{Type: schema.TypeInt, Optional: true, Description: "Cores held back from jobs for the host."}
	compute["reserve_memory_mb"] = &schema.Schema{Type: schema.TypeInt, Optional: true, Description: "Memory in MiB held back from jobs for the host."}
	compute["gres"] = &schema.Schema{
		Type:         schema.TypeString,
		Optional:     true,
		Default:      "none",
		ValidateFunc: validation.StringInSlice([]string{"none", "auto"}, false),
		Description:  "none or auto. auto reports the node's GPUs to Slurm and needs gpu_driver = true.",
	}

	return map[string]*schema.Schema{
		"name": {
			Type:        schema.TypeString,
			Required:    true,
			ForceNew:    true,
			Description: "Resource name (`metadata.name`).",
		},
		"cluster_name": {
			Type:        schema.TypeString,
			Optional:    true,
			Computed:    true,
			ForceNew:    true,
			Description: "Slurm ClusterName. Defaults to name.",
		},
		"controller": {
			Type:        schema.TypeList,
			Required:    true,
			MaxItems:    1,
			Description: "Node that runs slurmctld. Changing the node replaces the cluster.",
			Elem:        &schema.Resource{Schema: slurmMemberSchema(true)},
		},
		"backup_controller": {
			Type:        schema.TypeList,
			Optional:    true,
			MaxItems:    1,
			Description: "Optional second slurmctld node.",
			Elem:        &schema.Resource{Schema: slurmMemberSchema(false)},
		},
		"login_node_ids": {
			Type:        schema.TypeList,
			Optional:    true,
			Elem:        &schema.Schema{Type: schema.TypeString},
			Description: "Nodes that get the login role. They may also host guests.",
		},
		"accounting": {
			Type:        schema.TypeList,
			Optional:    true,
			MaxItems:    1,
			Description: "slurmdbd and MariaDB. node_id defaults to the controller node.",
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"enabled": {Type: schema.TypeBool, Optional: true},
				"node_id": {Type: schema.TypeString, Optional: true},
			}},
		},
		"compute": {
			Type:        schema.TypeList,
			Required:    true,
			MinItems:    1,
			Description: "Compute members. Planning refuses a node that still hosts guest VMs.",
			Elem:        &schema.Resource{Schema: compute},
		},
		"partition": {
			Type:        schema.TypeList,
			Required:    true,
			MinItems:    1,
			Description: "Slurm partitions.",
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"name":     {Type: schema.TypeString, Required: true},
				"default":  {Type: schema.TypeBool, Optional: true},
				"max_time": {Type: schema.TypeString, Optional: true, Description: "For example INFINITE or 24:00:00."},
				"state":    {Type: schema.TypeString, Optional: true, Description: "For example UP."},
			}},
		},
		"user": {
			Type:        schema.TypeList,
			Optional:    true,
			Description: "Cluster users. The uid is fixed so it matches on every member.",
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"name":     {Type: schema.TypeString, Required: true},
				"uid":      {Type: schema.TypeInt, Required: true},
				"ssh_keys": {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}, Description: "SSH public keys."},
			}},
		},
		"storage": {
			Type:        schema.TypeList,
			Optional:    true,
			MaxItems:    1,
			Description: "Shared storage. With mode nfs the controller node exports the paths, which makes it a single point of failure for them.",
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"mode": {
					Type:         schema.TypeString,
					Optional:     true,
					Default:      "nfs",
					ValidateFunc: validation.StringInSlice([]string{"nfs", "shared-filesystem", "none"}, false),
					Description:  "nfs (default), shared-filesystem, or none. The controller refuses shared-filesystem for now.",
				},
				"exports": {
					Type:        schema.TypeList,
					Optional:    true,
					Elem:        &schema.Schema{Type: schema.TypeString},
					Description: "Paths exported with nfs. Empty means /home and /scratch.",
				},
				"filesystem": {Type: schema.TypeString, Optional: true, Description: "SharedFilesystem name when mode is shared-filesystem."},
			}},
		},
		"extra_config":          {Type: schema.TypeString, Optional: true, Description: "Lines appended to slurm.conf."},
		"allow_controller_host": {Type: schema.TypeBool, Optional: true, Description: "Let the controller node also be a compute member."},
		"gpu_driver":            {Type: schema.TypeBool, Optional: true, Description: "Install the host NVIDIA driver on compute members. gres = \"auto\" needs it."},
		"wait_for_ready": {
			Type:        schema.TypeBool,
			Optional:    true,
			Default:     true,
			Description: "Wait until the reconciler reports the cluster ready before apply returns.",
		},
		"force": {
			Type:        schema.TypeBool,
			Optional:    true,
			Description: "Pass force when deleting. Rows are dropped even when a member is unreachable, and the units are not removed from it.",
		},
		"generation":     {Type: schema.TypeInt, Computed: true},
		"phase":          {Type: schema.TypeString, Computed: true, Description: "pending, applying, ready, degraded, deleting, or failed."},
		"status_message": {Type: schema.TypeString, Computed: true},
		"node_status": {
			Type:     schema.TypeList,
			Computed: true,
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"node_id":            {Type: schema.TypeString, Computed: true},
				"roles":              {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
				"slurm_state":        {Type: schema.TypeString, Computed: true},
				"applied_generation": {Type: schema.TypeInt, Computed: true},
			}},
		},
	}
}

func slurmController(rows []map[string]interface{}) *pb.SlurmControllerSpec {
	if len(rows) == 0 {
		return nil
	}
	return &pb.SlurmControllerSpec{
		NodeId:  strings.TrimSpace(mstring(rows[0], "node_id")),
		Address: strings.TrimSpace(mstring(rows[0], "address")),
		Iface:   strings.TrimSpace(mstring(rows[0], "iface")),
	}
}

func slurmClusterSpec(d *schema.ResourceData) *pb.SlurmClusterSpec {
	clusterName := strings.TrimSpace(s(d, "cluster_name"))
	if clusterName == "" {
		clusterName = strings.TrimSpace(s(d, "name"))
	}
	spec := &pb.SlurmClusterSpec{
		ClusterName:         clusterName,
		Controller:          slurmController(asMaps(d.Get("controller"))),
		BackupController:    slurmController(asMaps(d.Get("backup_controller"))),
		Login:               &pb.SlurmLoginSpec{NodeIds: strList(d, "login_node_ids")},
		Accounting:          &pb.SlurmAccountingSpec{},
		ExtraConfig:         s(d, "extra_config"),
		AllowControllerHost: b(d, "allow_controller_host"),
		GpuDriver:           b(d, "gpu_driver"),
	}
	if rows := asMaps(d.Get("accounting")); len(rows) > 0 {
		spec.Accounting = &pb.SlurmAccountingSpec{
			Enabled: mbool(rows[0], "enabled"),
			NodeId:  strings.TrimSpace(mstring(rows[0], "node_id")),
		}
	}
	for _, row := range asMaps(d.Get("compute")) {
		gres := mstring(row, "gres")
		if gres == "" {
			gres = "none"
		}
		spec.Compute = append(spec.Compute, &pb.SlurmComputeSpec{
			NodeId:     strings.TrimSpace(mstring(row, "node_id")),
			Address:    strings.TrimSpace(mstring(row, "address")),
			Iface:      strings.TrimSpace(mstring(row, "iface")),
			Partitions: mstrings(row, "partitions"),
			Reserve: &pb.SlurmReserveSpec{
				Cores:    mint(row, "reserve_cores"),
				MemoryMb: int64(mint(row, "reserve_memory_mb")),
			},
			Gres: gres,
		})
	}
	for _, row := range asMaps(d.Get("partition")) {
		spec.Partitions = append(spec.Partitions, &pb.SlurmPartitionSpec{
			Name:    mstring(row, "name"),
			Default: mbool(row, "default"),
			MaxTime: mstring(row, "max_time"),
			State:   mstring(row, "state"),
		})
	}
	for _, row := range asMaps(d.Get("user")) {
		spec.Users = append(spec.Users, &pb.SlurmUserSpec{
			Name:    mstring(row, "name"),
			Uid:     mint(row, "uid"),
			SshKeys: mstrings(row, "ssh_keys"),
		})
	}
	storage := &pb.SlurmStorageSpec{Mode: "nfs", Exports: []string{"/home", "/scratch"}}
	if rows := asMaps(d.Get("storage")); len(rows) > 0 {
		row := rows[0]
		storage.Mode = mstring(row, "mode")
		if storage.Mode == "" {
			storage.Mode = "nfs"
		}
		storage.Exports = mstrings(row, "exports")
		if len(storage.Exports) == 0 && storage.Mode == "nfs" {
			storage.Exports = []string{"/home", "/scratch"}
		}
		storage.Filesystem = mstring(row, "filesystem")
	}
	spec.Storage = storage
	return spec
}

func validateSlurmPartitions(spec *pb.SlurmClusterSpec) error {
	declared := map[string]bool{}
	for _, partition := range spec.GetPartitions() {
		declared[partition.GetName()] = true
	}
	for _, compute := range spec.GetCompute() {
		for _, partition := range compute.GetPartitions() {
			if !declared[partition] {
				return fmt.Errorf("compute node %s serves partition %q, which has no partition block", compute.GetNodeId(), partition)
			}
		}
		if compute.GetGres() == "auto" && !spec.GetGpuDriver() {
			return fmt.Errorf("compute node %s sets gres = \"auto\", which needs gpu_driver = true", compute.GetNodeId())
		}
	}
	return nil
}

func planSlurmCluster(ctx context.Context, client pb.ControllerClient, name string, spec *pb.SlurmClusterSpec) diag.Diagnostics {
	plan, err := client.PlanSlurmCluster(ctx, &pb.PlanSlurmClusterRequest{Name: name, Spec: spec})
	if err != nil {
		return diag.FromErr(fmt.Errorf("planning slurm cluster %s: %w", name, err))
	}
	var diags diag.Diagnostics
	for _, warning := range plan.GetWarnings() {
		diags = append(diags, diag.Diagnostic{Severity: diag.Warning, Summary: "Slurm plan warning", Detail: warning})
	}
	issues := plan.GetBlockingIssues()
	for _, node := range plan.GetNodePlans() {
		for _, issue := range node.GetIssues() {
			issues = append(issues, fmt.Sprintf("%s: %s", node.GetNodeId(), issue))
		}
	}
	if len(issues) > 0 {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Slurm plan blocked",
			Detail:   strings.Join(issues, "; "),
		})
	}
	if plan.GetAction() == "replace" {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Slurm cluster needs to be replaced",
			Detail:   "An immutable field changed. Destroy the cluster and create it again.",
		})
	}
	return diags
}

func resourceSlurmClusterUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := api(meta).controller
	name := strings.TrimSpace(s(d, "name"))
	spec := slurmClusterSpec(d)
	if err := validateSlurmPartitions(spec); err != nil {
		return diag.FromErr(err)
	}
	diags := planSlurmCluster(ctx, client, name, spec)
	if diags.HasError() {
		return diags
	}
	resp, err := client.CreateSlurmCluster(ctx, &pb.CreateSlurmClusterRequest{Name: name, Spec: spec, Confirm: true})
	if err != nil {
		return append(diags, diag.FromErr(fmt.Errorf("applying slurm cluster %s: %w", name, err))...)
	}
	if !resp.GetSuccess() {
		return append(diags, diag.Errorf("applying slurm cluster %s: %s", name, resp.GetMessage())...)
	}
	d.SetId(name)
	if b(d, "wait_for_ready") && resp.GetAction() != pb.ApplyAction_APPLY_ACTION_UNCHANGED {
		timeout := d.Timeout(schema.TimeoutUpdate)
		if d.IsNewResource() {
			timeout = d.Timeout(schema.TimeoutCreate)
		}
		if err := waitForSlurmReady(ctx, client, name, resp.GetCluster().GetGeneration(), timeout); err != nil {
			return append(diags, diag.FromErr(err)...)
		}
	}
	return append(diags, resourceSlurmClusterRead(ctx, d, meta)...)
}

func waitForSlurmReady(ctx context.Context, client pb.ControllerClient, name string, generation int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for {
		resp, err := client.GetSlurmCluster(ctx, &pb.GetSlurmClusterRequest{Name: name})
		if err != nil {
			last = err.Error()
		} else {
			status := resp.GetCluster().GetStatus()
			last = fmt.Sprintf("phase %s, observed generation %d of %d: %s", status.GetPhase(), status.GetObservedGeneration(), generation, status.GetMessage())
			if status.GetPhase() == "failed" {
				return fmt.Errorf("slurm cluster %s failed: %s", name, status.GetMessage())
			}
			if status.GetPhase() == "ready" && status.GetObservedGeneration() >= generation {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for slurm cluster %s to be ready (%s)", name, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
}

func resourceSlurmClusterRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetSlurmCluster(ctx, &pb.GetSlurmClusterRequest{Name: d.Id()})
	if err != nil {
		if grpcNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}
	cluster := resp.GetCluster()
	if cluster == nil || cluster.GetName() == "" {
		d.SetId("")
		return nil
	}
	d.Set("name", cluster.GetName())
	if clusterName := cluster.GetSpec().GetClusterName(); clusterName != "" {
		d.Set("cluster_name", clusterName)
	}
	d.Set("generation", cluster.GetGeneration())
	d.Set("phase", cluster.GetStatus().GetPhase())
	d.Set("status_message", cluster.GetStatus().GetMessage())
	return diag.FromErr(d.Set("node_status", slurmNodeStatus(cluster.GetStatus().GetNodes())))
}

func slurmNodeStatus(nodes []*pb.SlurmNodeStatus) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(nodes))
	for _, node := range nodes {
		roles := make([]interface{}, 0, len(node.GetRoles()))
		for _, role := range node.GetRoles() {
			roles = append(roles, role)
		}
		rows = append(rows, map[string]interface{}{
			"node_id":            node.GetNodeId(),
			"roles":              roles,
			"slurm_state":        node.GetSlurmState(),
			"applied_generation": int(node.GetAppliedGeneration()),
		})
	}
	return rows
}

func resourceSlurmClusterDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteSlurmCluster(ctx, &pb.DeleteSlurmClusterRequest{
		Name:  d.Id(),
		Force: b(d, "force"),
	})
	if err != nil {
		if grpcNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(fmt.Errorf("deleting slurm cluster %s: %w", d.Id(), err))
	}
	if !resp.GetSuccess() {
		return diag.Errorf("deleting slurm cluster %s: %s", d.Id(), resp.GetMessage())
	}
	d.SetId("")
	return nil
}

func dataSourceSlurmCluster() *schema.Resource {
	return &schema.Resource{
		Description: "Read one Slurm cluster and the state of its members.",
		ReadContext: dataSourceSlurmClusterRead,
		Schema: map[string]*schema.Schema{
			"name":                {Type: schema.TypeString, Required: true},
			"cluster_name":        {Type: schema.TypeString, Computed: true},
			"controller_node_id":  {Type: schema.TypeString, Computed: true},
			"controller_address":  {Type: schema.TypeString, Computed: true},
			"compute_node_ids":    {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"login_node_ids":      {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"accounting_enabled":  {Type: schema.TypeBool, Computed: true},
			"partition_names":     {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"generation":          {Type: schema.TypeInt, Computed: true},
			"observed_generation": {Type: schema.TypeInt, Computed: true},
			"phase":               {Type: schema.TypeString, Computed: true},
			"status_message":      {Type: schema.TypeString, Computed: true},
			"node_status": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"node_id":            {Type: schema.TypeString, Computed: true},
					"roles":              {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
					"slurm_state":        {Type: schema.TypeString, Computed: true},
					"applied_generation": {Type: schema.TypeInt, Computed: true},
				}},
			},
		},
	}
}

func dataSourceSlurmClusterRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	name := s(d, "name")
	resp, err := api(meta).controller.GetSlurmCluster(ctx, &pb.GetSlurmClusterRequest{Name: name})
	if err != nil {
		return diag.FromErr(err)
	}
	cluster := resp.GetCluster()
	if cluster == nil || cluster.GetName() == "" {
		return diag.Errorf("slurm cluster %s not found", name)
	}
	d.SetId(cluster.GetName())
	spec := cluster.GetSpec()
	d.Set("cluster_name", spec.GetClusterName())
	d.Set("controller_node_id", spec.GetController().GetNodeId())
	d.Set("controller_address", spec.GetController().GetAddress())
	d.Set("login_node_ids", spec.GetLogin().GetNodeIds())
	d.Set("accounting_enabled", spec.GetAccounting().GetEnabled())
	var computeIDs, partitionNames []string
	for _, compute := range spec.GetCompute() {
		computeIDs = append(computeIDs, compute.GetNodeId())
	}
	for _, partition := range spec.GetPartitions() {
		partitionNames = append(partitionNames, partition.GetName())
	}
	d.Set("compute_node_ids", computeIDs)
	d.Set("partition_names", partitionNames)
	d.Set("generation", cluster.GetGeneration())
	d.Set("observed_generation", cluster.GetStatus().GetObservedGeneration())
	d.Set("phase", cluster.GetStatus().GetPhase())
	d.Set("status_message", cluster.GetStatus().GetMessage())
	return diag.FromErr(d.Set("node_status", slurmNodeStatus(cluster.GetStatus().GetNodes())))
}
