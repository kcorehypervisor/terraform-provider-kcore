package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceKubernetesCluster() *schema.Resource {
	return &schema.Resource{
		Description:   "Kubernetes cluster (`kctl kubernetes apply`). The local provisioner is `shared` (directories on one formatted volume) or `lvm` (one thin pool per node, no volume provisioner). Destroy deletes the cluster record.",
		CreateContext: resourceKubernetesCreate,
		ReadContext:   resourceKubernetesRead,
		UpdateContext: resourceKubernetesUpdate,
		DeleteContext: resourceKubernetesDelete,
		Schema:        kubernetesClusterSchema(),
	}
}

func kubernetesClusterSchema() map[string]*schema.Schema {
	return map[string]*schema.Schema{
		"name":                      {Type: schema.TypeString, Required: true, ForceNew: true},
		"cluster_cidr":              {Type: schema.TypeString, Required: true},
		"service_cidr":              {Type: schema.TypeString, Required: true},
		"cluster_dns":               {Type: schema.TypeString, Optional: true, Default: "10.96.0.10"},
		"api_server_endpoint":       {Type: schema.TypeString, Required: true, Description: "VIP or node address and port, for example 192.168.40.220:6443."},
		"cni":                       {Type: schema.TypeString, Optional: true, Default: "cilium"},
		"schedule_on_control_plane": {Type: schema.TypeBool, Optional: true, Description: "Control-plane nodes accept pods. Worker nodes may be empty."},
		"force":                     {Type: schema.TypeBool, Optional: true, Description: "Pass force when deleting the cluster."},
		"cilium": {
			Type:     schema.TypeList,
			Optional: true,
			MaxItems: 1,
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"mode":                    {Type: schema.TypeString, Optional: true, Default: "native-routing"},
				"mtu":                     {Type: schema.TypeInt, Optional: true, Default: 1500},
				"tunnel":                  {Type: schema.TypeString, Optional: true, Default: "disabled", ValidateFunc: validation.StringInSlice([]string{"disabled", "vxlan", "geneve"}, false)},
				"auto_direct_node_routes": {Type: schema.TypeBool, Optional: true, Default: true},
				"load_balancer_ip_pool":   {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}, Description: "CIDRs advertised for LoadBalancer services."},
			}},
		},
		"control_plane_node": kubernetesNodeSchema(true),
		"worker_node":        kubernetesNodeSchema(false),
		"high_availability": {
			Type:     schema.TypeList,
			Optional: true,
			MaxItems: 1,
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"enabled":       {Type: schema.TypeBool, Optional: true},
				"vip_address":   {Type: schema.TypeString, Optional: true},
				"vip_interface": {Type: schema.TypeString, Optional: true},
			}},
		},
		"local_provisioner": {
			Type:        schema.TypeList,
			Optional:    true,
			MaxItems:    1,
			Description: "Host storage for PersistentVolumeClaims. mode shared installs the local-path provisioner. mode lvm creates one thin pool on each member and does not install a provisioner.",
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"enabled":            {Type: schema.TypeBool, Optional: true},
				"mode":               {Type: schema.TypeString, Optional: true, ValidateFunc: validation.StringInSlice([]string{"", "shared", "lvm"}, false), Description: "shared or lvm. Empty means shared."},
				"storage_class_name": {Type: schema.TypeString, Optional: true, Description: "Empty means local-path."},
				"volume": {
					Type:     schema.TypeList,
					Optional: true,
					Elem: &schema.Resource{Schema: map[string]*schema.Schema{
						"name":          {Type: schema.TypeString, Required: true},
						"node_id":       {Type: schema.TypeString, Required: true, Description: "Must be a control-plane or worker member of this cluster."},
						"storage_class": {Type: schema.TypeString, Required: true, ValidateFunc: validation.StringInSlice([]string{"lvm", "zfs"}, false)},
						"size_bytes":    {Type: schema.TypeString, Required: true, ValidateFunc: validation.StringMatch(positiveInt, "size_bytes must be a positive integer")},
						"fs_type":       {Type: schema.TypeString, Optional: true, ValidateFunc: validation.StringInSlice([]string{"", "ext4", "xfs"}, false), Description: "ext4 or xfs. Empty means ext4."},
						"mount_path":    {Type: schema.TypeString, Optional: true, Description: "Empty means /mnt/kcore/<cluster>/<name>. Leave empty for an lvm pool."},
						"role":          {Type: schema.TypeString, Optional: true, ValidateFunc: validation.StringInSlice([]string{"", "filesystem", "pool"}, false), Description: "filesystem in shared mode, pool in lvm mode."},
						"volume_group":  {Type: schema.TypeString, Optional: true, Description: "Required when role is pool."},
					}},
				},
			}},
		},
		"generation":           {Type: schema.TypeInt, Computed: true},
		"phase":                {Type: schema.TypeString, Computed: true},
		"status_message":       {Type: schema.TypeString, Computed: true},
		"ready_control_planes": {Type: schema.TypeInt, Computed: true},
		"ready_workers":        {Type: schema.TypeInt, Computed: true},
		"overall_health":       {Type: schema.TypeString, Computed: true},
		"etcd_healthy":         {Type: schema.TypeBool, Computed: true},
		"etcd_member_count":    {Type: schema.TypeInt, Computed: true},
		"api_server_reachable": {Type: schema.TypeBool, Computed: true},
		"node_health": {
			Type:     schema.TypeList,
			Computed: true,
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"node_id":           {Type: schema.TypeString, Computed: true},
				"kubernetes_status": {Type: schema.TypeString, Computed: true},
				"kubelet_active":    {Type: schema.TypeBool, Computed: true},
				"etcd_active":       {Type: schema.TypeBool, Computed: true},
				"api_server_active": {Type: schema.TypeBool, Computed: true},
			}},
		},
	}
}

var positiveInt = regexp.MustCompile(`^[1-9][0-9]*$`)

func kubernetesNodeSchema(controlPlane bool) *schema.Schema {
	node := map[string]*schema.Schema{
		"node_id":           {Type: schema.TypeString, Required: true},
		"advertise_address": {Type: schema.TypeString, Required: true},
		"iface":             {Type: schema.TypeString, Optional: true, Default: "enp6s0"},
	}
	desc := "Worker members. Adding or changing a node calls ScaleKubernetesCluster."
	if controlPlane {
		node["initial_cluster"] = &schema.Schema{Type: schema.TypeBool, Optional: true, Description: "Bootstrap etcd on this node. The controller treats the first control-plane node as the initial member."}
		desc = "Control-plane members. The first node bootstraps etcd. Adding or changing a node calls ScaleKubernetesCluster."
	}
	return &schema.Schema{
		Type:        schema.TypeList,
		Required:    controlPlane,
		Optional:    !controlPlane,
		MinItems:    boolToMin(controlPlane),
		Description: desc,
		Elem:        &schema.Resource{Schema: node},
	}
}

func boolToMin(required bool) int {
	if required {
		return 1
	}
	return 0
}

func dataSourceKubernetesCluster() *schema.Resource {
	return &schema.Resource{
		Description: "Read one Kubernetes cluster and its health.",
		ReadContext: dataSourceKubernetesClusterRead,
		Schema: map[string]*schema.Schema{
			"name":                      {Type: schema.TypeString, Required: true},
			"cluster_cidr":              {Type: schema.TypeString, Computed: true},
			"service_cidr":              {Type: schema.TypeString, Computed: true},
			"cluster_dns":               {Type: schema.TypeString, Computed: true},
			"api_server_endpoint":       {Type: schema.TypeString, Computed: true},
			"cni":                       {Type: schema.TypeString, Computed: true},
			"schedule_on_control_plane": {Type: schema.TypeBool, Computed: true},
			"provisioner_enabled":       {Type: schema.TypeBool, Computed: true},
			"provisioner_mode":          {Type: schema.TypeString, Computed: true},
			"storage_class_name":        {Type: schema.TypeString, Computed: true},
			"control_plane_node_id":     {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"worker_node_id":            {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"generation":                {Type: schema.TypeInt, Computed: true},
			"phase":                     {Type: schema.TypeString, Computed: true},
			"status_message":            {Type: schema.TypeString, Computed: true},
			"ready_control_planes":      {Type: schema.TypeInt, Computed: true},
			"ready_workers":             {Type: schema.TypeInt, Computed: true},
			"overall_health":            {Type: schema.TypeString, Computed: true},
			"etcd_healthy":              {Type: schema.TypeBool, Computed: true},
			"etcd_member_count":         {Type: schema.TypeInt, Computed: true},
			"api_server_reachable":      {Type: schema.TypeBool, Computed: true},
		},
	}
}

func dataSourceKubernetesClusters() *schema.Resource {
	return &schema.Resource{
		Description: "List Kubernetes clusters.",
		ReadContext: dataSourceKubernetesClustersRead,
		Schema: map[string]*schema.Schema{
			"clusters": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"name":                 {Type: schema.TypeString, Computed: true},
					"phase":                {Type: schema.TypeString, Computed: true},
					"api_server_endpoint":  {Type: schema.TypeString, Computed: true},
					"ready_control_planes": {Type: schema.TypeInt, Computed: true},
					"ready_workers":        {Type: schema.TypeInt, Computed: true},
					"provisioner_enabled":  {Type: schema.TypeBool, Computed: true},
					"provisioner_mode":     {Type: schema.TypeString, Computed: true},
				}},
			},
		},
	}
}

func asMaps(raw interface{}) []map[string]interface{} {
	items, _ := raw.([]interface{})
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if ok && row != nil {
			out = append(out, row)
		}
	}
	return out
}

func mstring(row map[string]interface{}, key string) string {
	v, _ := row[key].(string)
	return v
}

func mbool(row map[string]interface{}, key string) bool {
	v, _ := row[key].(bool)
	return v
}

func mint(row map[string]interface{}, key string) int32 {
	switch n := row[key].(type) {
	case int:
		return toInt32(n)
	case int32:
		return n
	case int64:
		return saturateInt32(n)
	default:
		return 0
	}
}

func mstrings(row map[string]interface{}, key string) []string {
	items, _ := row[key].([]interface{})
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseSizeBytes(raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("size_bytes must be a positive integer, got %q", raw)
	}
	return n, nil
}

func kubernetesNodes(rows []map[string]interface{}, role string) []*pb.KubernetesNodeSpec {
	nodes := make([]*pb.KubernetesNodeSpec, 0, len(rows))
	for _, row := range rows {
		iface := mstring(row, "iface")
		if iface == "" {
			iface = "enp6s0"
		}
		initial := role == "control-plane" && mbool(row, "initial_cluster")
		nodes = append(nodes, &pb.KubernetesNodeSpec{
			NodeId:           mstring(row, "node_id"),
			Role:             role,
			AdvertiseAddress: mstring(row, "advertise_address"),
			Iface:            iface,
			InitialCluster:   initial,
			JoinExisting:     !initial,
		})
	}
	return nodes
}

func kubernetesSpec(d *schema.ResourceData) (*pb.KubernetesClusterSpec, error) {
	cilium := &pb.CiliumConfig{
		Mode:                 "native-routing",
		Mtu:                  1500,
		Tunnel:               "disabled",
		AutoDirectNodeRoutes: true,
	}
	if rows := asMaps(d.Get("cilium")); len(rows) > 0 {
		row := rows[0]
		cilium.Mode = mstring(row, "mode")
		cilium.Mtu = mint(row, "mtu")
		cilium.Tunnel = mstring(row, "tunnel")
		cilium.AutoDirectNodeRoutes = mbool(row, "auto_direct_node_routes")
		cilium.LoadBalancerIpPools = mstrings(row, "load_balancer_ip_pool")
	}
	var ha *pb.HighAvailabilityConfig
	if rows := asMaps(d.Get("high_availability")); len(rows) > 0 && mbool(rows[0], "enabled") {
		ha = &pb.HighAvailabilityConfig{
			Enabled:      true,
			VipAddress:   mstring(rows[0], "vip_address"),
			VipInterface: mstring(rows[0], "vip_interface"),
			Method:       "keepalived",
		}
	}
	var local *pb.KubernetesLocalProvisioner
	if rows := asMaps(d.Get("local_provisioner")); len(rows) > 0 {
		row := rows[0]
		volumes, err := hostVolumes(asMaps(row["volume"]))
		if err != nil {
			return nil, err
		}
		if mbool(row, "enabled") || len(volumes) > 0 {
			local = &pb.KubernetesLocalProvisioner{
				Enabled:          mbool(row, "enabled"),
				StorageClassName: mstring(row, "storage_class_name"),
				Mode:             mstring(row, "mode"),
				Volumes:          volumes,
			}
		}
	}
	return &pb.KubernetesClusterSpec{
		ClusterCidr:            s(d, "cluster_cidr"),
		ServiceCidr:            s(d, "service_cidr"),
		ClusterDns:             s(d, "cluster_dns"),
		ApiServerEndpoint:      s(d, "api_server_endpoint"),
		Cni:                    s(d, "cni"),
		CiliumConfig:           cilium,
		ControlPlaneNodes:      kubernetesNodes(asMaps(d.Get("control_plane_node")), "control-plane"),
		WorkerNodes:            kubernetesNodes(asMaps(d.Get("worker_node")), "worker"),
		HighAvailability:       ha,
		LocalProvisioner:       local,
		ScheduleOnControlPlane: b(d, "schedule_on_control_plane"),
	}, nil
}

func hostVolumes(rows []map[string]interface{}) ([]*pb.KubernetesHostVolume, error) {
	volumes := make([]*pb.KubernetesHostVolume, 0, len(rows))
	for _, row := range rows {
		size, err := parseSizeBytes(mstring(row, "size_bytes"))
		if err != nil {
			return nil, fmt.Errorf("volume %s: %w", mstring(row, "name"), err)
		}
		volumes = append(volumes, &pb.KubernetesHostVolume{
			Name:         mstring(row, "name"),
			NodeId:       mstring(row, "node_id"),
			StorageClass: mstring(row, "storage_class"),
			SizeBytes:    size,
			FsType:       mstring(row, "fs_type"),
			MountPath:    mstring(row, "mount_path"),
			Role:         mstring(row, "role"),
			VolumeGroup:  mstring(row, "volume_group"),
		})
	}
	return volumes, nil
}

func changedNodes(oldRows, newRows []map[string]interface{}, role string) []*pb.KubernetesNodeSpec {
	previous := map[string]map[string]interface{}{}
	for _, row := range oldRows {
		previous[mstring(row, "node_id")] = row
	}
	var added []map[string]interface{}
	for _, row := range newRows {
		id := mstring(row, "node_id")
		if id == "" {
			continue
		}
		before, ok := previous[id]
		if !ok || mstring(before, "advertise_address") != mstring(row, "advertise_address") || mstring(before, "iface") != mstring(row, "iface") {
			added = append(added, row)
		}
	}
	nodes := kubernetesNodes(added, role)
	for _, node := range nodes {
		node.InitialCluster = false
		node.JoinExisting = true
	}
	return nodes
}

func removedIDs(oldRows, newRows []map[string]interface{}) []string {
	next := map[string]bool{}
	for _, row := range newRows {
		next[mstring(row, "node_id")] = true
	}
	var gone []string
	for _, row := range oldRows {
		id := mstring(row, "node_id")
		if id != "" && !next[id] {
			gone = append(gone, id)
		}
	}
	return gone
}

func kubernetesScaleDelta(oldCP, newCP, oldWorkers, newWorkers []map[string]interface{}) ([]*pb.KubernetesNodeSpec, []string) {
	add := append(changedNodes(oldCP, newCP, "control-plane"), changedNodes(oldWorkers, newWorkers, "worker")...)
	remove := append(removedIDs(oldCP, newCP), removedIDs(oldWorkers, newWorkers)...)
	return add, remove
}

func planKubernetes(ctx context.Context, client pb.ControllerClient, name string, spec *pb.KubernetesClusterSpec) diag.Diagnostics {
	plan, err := client.PlanKubernetesCluster(ctx, &pb.PlanKubernetesClusterRequest{Name: name, Spec: spec})
	if err != nil {
		return diag.FromErr(err)
	}
	var diags diag.Diagnostics
	for _, warning := range plan.GetWarnings() {
		diags = append(diags, diag.Diagnostic{Severity: diag.Warning, Summary: "Kubernetes plan warning", Detail: warning})
	}
	if issues := plan.GetBlockingIssues(); len(issues) > 0 {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Kubernetes plan blocked",
			Detail:   strings.Join(issues, "; "),
		})
	}
	return diags
}

func resourceKubernetesCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	spec, err := kubernetesSpec(d)
	if err != nil {
		return diag.FromErr(err)
	}
	client := api(meta).controller
	name := s(d, "name")
	diags := planKubernetes(ctx, client, name, spec)
	if diags.HasError() {
		return diags
	}
	resp, err := client.CreateKubernetesCluster(ctx, &pb.CreateKubernetesClusterRequest{Name: name, Spec: spec, Confirm: true})
	if err != nil {
		return diag.FromErr(err)
	}
	if resp.GetCluster() == nil || resp.Cluster.GetName() == "" {
		return diag.Errorf("controller did not return kubernetes cluster %s", name)
	}
	d.SetId(name)
	return append(diags, resourceKubernetesRead(ctx, d, meta)...)
}

func resourceKubernetesUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	spec, err := kubernetesSpec(d)
	if err != nil {
		return diag.FromErr(err)
	}
	client := api(meta).controller
	name := d.Id()
	diags := planKubernetes(ctx, client, name, spec)
	if diags.HasError() {
		return diags
	}
	if _, err := client.UpdateKubernetesCluster(ctx, &pb.UpdateKubernetesClusterRequest{Name: name, Spec: spec, Confirm: true}); err != nil {
		return diag.FromErr(err)
	}
	add, remove := kubernetesScaleDelta(
		asMaps(changeOld(d, "control_plane_node")),
		asMaps(d.Get("control_plane_node")),
		asMaps(changeOld(d, "worker_node")),
		asMaps(d.Get("worker_node")),
	)
	if len(add) > 0 || len(remove) > 0 {
		if _, err := client.ScaleKubernetesCluster(ctx, &pb.ScaleKubernetesClusterRequest{
			Name:          name,
			AddNodes:      add,
			RemoveNodeIds: remove,
			Confirm:       true,
		}); err != nil {
			return diag.FromErr(err)
		}
	}
	return append(diags, resourceKubernetesRead(ctx, d, meta)...)
}

func changeOld(d *schema.ResourceData, key string) interface{} {
	old, _ := d.GetChange(key)
	return old
}

func resourceKubernetesRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := api(meta).controller
	resp, err := client.GetKubernetesCluster(ctx, &pb.GetKubernetesClusterRequest{Name: d.Id()})
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
	d.Set("generation", cluster.GetGeneration())
	if status := cluster.GetStatus(); status != nil {
		d.Set("phase", status.GetPhase())
		d.Set("status_message", status.GetMessage())
		d.Set("ready_control_planes", status.GetReadyControlPlanes())
		d.Set("ready_workers", status.GetReadyWorkers())
	}
	health, err := client.GetKubernetesClusterHealth(ctx, &pb.GetKubernetesClusterHealthRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	return diag.FromErr(setKubernetesHealth(d, health))
}

func setKubernetesHealth(d *schema.ResourceData, health *pb.GetKubernetesClusterHealthResponse) error {
	if health == nil {
		return nil
	}
	d.Set("overall_health", health.GetOverallHealth())
	d.Set("etcd_healthy", health.GetEtcdHealthy())
	d.Set("etcd_member_count", health.GetEtcdMemberCount())
	d.Set("api_server_reachable", health.GetApiServerReachable())
	d.Set("ready_control_planes", health.GetReadyControlPlanes())
	d.Set("ready_workers", health.GetReadyWorkers())
	rows := make([]map[string]interface{}, 0, len(health.GetNodeHealth()))
	for _, node := range health.GetNodeHealth() {
		rows = append(rows, map[string]interface{}{
			"node_id":           node.GetNodeId(),
			"kubernetes_status": node.GetKubernetesStatus(),
			"kubelet_active":    node.GetKubeletActive(),
			"etcd_active":       node.GetEtcdActive(),
			"api_server_active": node.GetApiServerActive(),
		})
	}
	return d.Set("node_health", rows)
}

func resourceKubernetesDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_, err := api(meta).controller.DeleteKubernetesCluster(ctx, &pb.DeleteKubernetesClusterRequest{
		Name:    d.Id(),
		Confirm: true,
		Force:   b(d, "force"),
	})
	if err != nil && !grpcNotFound(err) {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func grpcNotFound(err error) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.NotFound
}

func dataSourceKubernetesClusterRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := api(meta).controller
	name := s(d, "name")
	resp, err := client.GetKubernetesCluster(ctx, &pb.GetKubernetesClusterRequest{Name: name})
	if err != nil {
		return diag.FromErr(err)
	}
	cluster := resp.GetCluster()
	if cluster == nil {
		return diag.Errorf("kubernetes cluster %s not found", name)
	}
	d.SetId(cluster.GetName())
	d.Set("generation", cluster.GetGeneration())
	if spec := cluster.GetSpec(); spec != nil {
		d.Set("cluster_cidr", spec.GetClusterCidr())
		d.Set("service_cidr", spec.GetServiceCidr())
		d.Set("cluster_dns", spec.GetClusterDns())
		d.Set("api_server_endpoint", spec.GetApiServerEndpoint())
		d.Set("cni", spec.GetCni())
		d.Set("schedule_on_control_plane", spec.GetScheduleOnControlPlane())
		d.Set("control_plane_node_id", nodeIDs(spec.GetControlPlaneNodes()))
		d.Set("worker_node_id", nodeIDs(spec.GetWorkerNodes()))
		if local := spec.GetLocalProvisioner(); local != nil {
			d.Set("provisioner_enabled", local.GetEnabled())
			d.Set("provisioner_mode", local.GetMode())
			d.Set("storage_class_name", local.GetStorageClassName())
		}
	}
	if status := cluster.GetStatus(); status != nil {
		d.Set("phase", status.GetPhase())
		d.Set("status_message", status.GetMessage())
		d.Set("ready_control_planes", status.GetReadyControlPlanes())
		d.Set("ready_workers", status.GetReadyWorkers())
	}
	health, err := client.GetKubernetesClusterHealth(ctx, &pb.GetKubernetesClusterHealthRequest{Name: name})
	if err != nil {
		return diag.FromErr(err)
	}
	d.Set("overall_health", health.GetOverallHealth())
	d.Set("etcd_healthy", health.GetEtcdHealthy())
	d.Set("etcd_member_count", health.GetEtcdMemberCount())
	d.Set("api_server_reachable", health.GetApiServerReachable())
	d.Set("ready_control_planes", health.GetReadyControlPlanes())
	d.Set("ready_workers", health.GetReadyWorkers())
	return nil
}

func nodeIDs(nodes []*pb.KubernetesNodeSpec) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.GetNodeId())
	}
	return ids
}

func dataSourceKubernetesClustersRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.ListKubernetesClusters(ctx, &pb.ListKubernetesClustersRequest{})
	if err != nil {
		return diag.FromErr(err)
	}
	rows := make([]map[string]interface{}, 0, len(resp.GetClusters()))
	for _, cluster := range resp.GetClusters() {
		row := map[string]interface{}{"name": cluster.GetName()}
		if spec := cluster.GetSpec(); spec != nil {
			row["api_server_endpoint"] = spec.GetApiServerEndpoint()
			if local := spec.GetLocalProvisioner(); local != nil {
				row["provisioner_enabled"] = local.GetEnabled()
				row["provisioner_mode"] = local.GetMode()
			}
		}
		if status := cluster.GetStatus(); status != nil {
			row["phase"] = status.GetPhase()
			row["ready_control_planes"] = status.GetReadyControlPlanes()
			row["ready_workers"] = status.GetReadyWorkers()
		}
		rows = append(rows, row)
	}
	if err := d.Set("clusters", rows); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("kubernetes-clusters")
	return nil
}
