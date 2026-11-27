package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceCephCluster() *schema.Resource {
	return &schema.Resource{
		Description:   "Ceph cluster (`kctl create ceph-cluster`). See `docs/ceph-lifecycle.md` in kcore for which optional fields the reconciler acts on in a given release.",
		CreateContext: resourceCephUpsert,
		ReadContext:   resourceCephRead,
		UpdateContext: resourceCephUpsert,
		DeleteContext: resourceCephDelete,
		Schema: map[string]*schema.Schema{
			"name":            {Type: schema.TypeString, Required: true, ForceNew: true},
			"fsid":            {Type: schema.TypeString, Optional: true},
			"public_network":  {Type: schema.TypeString, Required: true},
			"cluster_network": {Type: schema.TypeString, Optional: true},
			"size":            {Type: schema.TypeInt, Optional: true, Default: 3},
			"min_size":        {Type: schema.TypeInt, Optional: true, Default: 2},
			"force_wipe":      {Type: schema.TypeBool, Optional: true},
			"encrypt_osds":    {Type: schema.TypeBool, Optional: true},
			"node": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "Member hosts. Required unless `external` is set.",
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"node_id":       {Type: schema.TypeString, Required: true},
					"mon_addr":      {Type: schema.TypeString, Optional: true},
					"cluster_addr":  {Type: schema.TypeString, Optional: true},
					"public_iface":  {Type: schema.TypeString, Optional: true},
					"cluster_iface": {Type: schema.TypeString, Optional: true},
					"osd_device":    {Type: schema.TypeString, Optional: true, Description: "First OSD disk. Empty on a `mon` role node."},
					"osd_devices":   {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}},
					"role": {
						Type: schema.TypeString, Optional: true,
						Description:  "`full` (mon, mgr, and OSDs; the default), `mon` (mon and mgr only), or `osd`.",
						ValidateFunc: validation.StringInSlice([]string{"", "full", "mon", "osd"}, false),
					},
					"weight": {Type: schema.TypeFloat, Optional: true, Description: "CRUSH weight override for this host's OSDs. 0 leaves Ceph's default."},
					"device_class": {
						Type: schema.TypeString, Optional: true,
						Description:  "Device class override (`ssd`, `hdd`, `nvme`). Empty means Ceph detects it.",
						ValidateFunc: validation.StringInSlice([]string{"", "ssd", "hdd", "nvme"}, false),
					},
					"rack": {Type: schema.TypeString, Optional: true, Description: "CRUSH rack bucket. Empty keeps the host under the default hierarchy."},
					"room": {Type: schema.TypeString, Optional: true, Description: "CRUSH room bucket when failure_domain is room or stretch."},
				}},
			},
			"pool": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "Pools to ensure. Empty means only the default `kcore-vms` RBD pool.",
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"name": {Type: schema.TypeString, Required: true},
					"application": {
						Type: schema.TypeString, Optional: true,
						ValidateFunc: validation.StringInSlice([]string{"", "rbd", "cephfs", "rgw"}, false),
					},
					"size":       {Type: schema.TypeInt, Optional: true},
					"min_size":   {Type: schema.TypeInt, Optional: true},
					"pg_num":     {Type: schema.TypeInt, Optional: true, Description: "0 leaves the PG autoscaler in charge."},
					"crush_rule": {Type: schema.TypeString, Optional: true},
					"compression_mode": {
						Type: schema.TypeString, Optional: true,
						ValidateFunc: validation.StringInSlice([]string{"", "none", "passive", "aggressive", "force"}, false),
					},
					"quota_bytes": {Type: schema.TypeInt, Optional: true},
					"pool_type": {
						Type: schema.TypeString, Optional: true,
						Description:  "`replicated` (default) or `erasure`.",
						ValidateFunc: validation.StringInSlice([]string{"", "replicated", "erasure"}, false),
					},
					"erasure_profile": {Type: schema.TypeString, Optional: true},
				}},
			},
			"recovery": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"max_backfills":       {Type: schema.TypeInt, Optional: true},
					"recovery_max_active": {Type: schema.TypeInt, Optional: true},
					"scrub_begin_hour":    {Type: schema.TypeInt, Optional: true, Default: -1, ValidateFunc: validation.IntBetween(-1, 23), Description: "-1 leaves it unset."},
					"scrub_end_hour":      {Type: schema.TypeInt, Optional: true, Default: -1, ValidateFunc: validation.IntBetween(-1, 23), Description: "-1 leaves it unset."},
					"nearfull_ratio_bp":   {Type: schema.TypeInt, Optional: true, ValidateFunc: validation.IntBetween(0, 9900), Description: "Nearfull ratio as percent x 100, for example 8500 for 85%. 0 leaves Ceph's default."},
				}},
			},
			"ms_mode": {
				Type: schema.TypeString, Optional: true,
				Description:  "`secure` or `prefer-crc` for ms_cluster_mode and ms_service_mode. Empty leaves Ceph's default.",
				ValidateFunc: validation.StringInSlice([]string{"", "secure", "prefer-crc"}, false),
			},
			"image_features": {Type: schema.TypeString, Optional: true, Description: "Features for new RBD images, for example `layering,exclusive-lock`. Existing images are not changed."},
			"cache_policy": {
				Type: schema.TypeString, Optional: true,
				Description:  "`writeback` (default) or `writethrough` for new rbd-nbd maps.",
				ValidateFunc: validation.StringInSlice([]string{"", "writeback", "writethrough"}, false),
			},
			"lost_members": {
				Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString},
				Description: "Node ids that will never answer again. A survivor drains them, then purges their OSDs and removes their mons.",
			},
			"failure_domain": {
				Type: schema.TypeString, Optional: true,
				Description:  "CRUSH failure domain: `host` (default), `rack`, or `room`. Pools without crush_rule use `kcore-<domain>`.",
				ValidateFunc: validation.StringInSlice([]string{"", "host", "rack", "room"}, false),
			},
			"stretch": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "When true with failure_domain=room, enable stretch mode (tiebreaker mon).",
			},
			"mirroring": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Description: "rbd-mirror between this CephCluster and peer_cluster.",
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"enabled":      {Type: schema.TypeBool, Optional: true},
					"mode":         {Type: schema.TypeString, Optional: true, Description: "`pool` (default) or `image`.", ValidateFunc: validation.StringInSlice([]string{"", "pool", "image"}, false)},
					"peer_cluster": {Type: schema.TypeString, Optional: true, Description: "Other CephCluster name to peer with."},
				}},
			},
			"external": {
				Type:        schema.TypeList,
				Optional:    true,
				MaxItems:    1,
				ForceNew:    true,
				Description: "Use a Ceph cluster kcore does not run. Mutually exclusive with `node`.",
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"mon_hosts":      {Type: schema.TypeString, Required: true, Description: "Comma-separated mon_host list."},
					"pool":           {Type: schema.TypeString, Required: true},
					"client_name":    {Type: schema.TypeString, Optional: true},
					"client_keyring": {Type: schema.TypeString, Optional: true, Sensitive: true, Description: "Keyring body. The controller never returns it."},
				}},
			},
			"phase":             {Type: schema.TypeString, Computed: true},
			"health_message":    {Type: schema.TypeString, Computed: true},
			"operation_message": {Type: schema.TypeString, Computed: true, Description: "In-progress day-2 step (member drain, OSD replace, backfill ramp)."},
			"health": {
				Type:        schema.TypeList,
				Computed:    true,
				Description: "Parsed `ceph -s` as the reconciler last saw it.",
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"status":        {Type: schema.TypeString, Computed: true},
					"mon_quorum":    {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
					"mon_count":     {Type: schema.TypeInt, Computed: true},
					"osd_total":     {Type: schema.TypeInt, Computed: true},
					"osd_up":        {Type: schema.TypeInt, Computed: true},
					"osd_in":        {Type: schema.TypeInt, Computed: true},
					"pgs_total":     {Type: schema.TypeInt, Computed: true},
					"pgs_not_clean": {Type: schema.TypeInt, Computed: true},
					"backfill_pgs":  {Type: schema.TypeInt, Computed: true},
					"pgs_by_state":  {Type: schema.TypeMap, Computed: true, Elem: &schema.Schema{Type: schema.TypeInt}},
					"noout":         {Type: schema.TypeBool, Computed: true},
					"norebalance":   {Type: schema.TypeBool, Computed: true},
					"flags":         {Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
					"bytes_total":   {Type: schema.TypeInt, Computed: true},
					"bytes_used":    {Type: schema.TypeInt, Computed: true},
					"bytes_avail":   {Type: schema.TypeInt, Computed: true},
					"check": {
						Type: schema.TypeList, Computed: true,
						Elem: &schema.Resource{Schema: map[string]*schema.Schema{
							"severity": {Type: schema.TypeString, Computed: true},
							"type":     {Type: schema.TypeString, Computed: true},
							"summary":  {Type: schema.TypeString, Computed: true},
						}},
					},
				}},
			},
		},
	}
}

func cephFromState(d *schema.ResourceData) *pb.CephCluster {
	spec := &pb.CephClusterSpec{
		Fsid:           s(d, "fsid"),
		PublicNetwork:  s(d, "public_network"),
		ClusterNetwork: s(d, "cluster_network"),
		Size:           i32(d, "size"),
		MinSize:        i32(d, "min_size"),
		ForceWipe:      b(d, "force_wipe"),
		EncryptOsds:    b(d, "encrypt_osds"),
		MsMode:         s(d, "ms_mode"),
		ImageFeatures:  s(d, "image_features"),
		CachePolicy:    s(d, "cache_policy"),
		LostMembers:    strList(d, "lost_members"),
		FailureDomain:  s(d, "failure_domain"),
		Stretch:        b(d, "stretch"),
	}
	if mir, ok := d.Get("mirroring").([]interface{}); ok && len(mir) == 1 && mir[0] != nil {
		m := mir[0].(map[string]interface{})
		enabled, _ := m["enabled"].(bool)
		spec.Mirroring = &pb.CephMirroringSpec{
			Enabled:     enabled,
			Mode:        mapStr(m, "mode"),
			PeerCluster: mapStr(m, "peer_cluster"),
		}
	}
	raw, _ := d.Get("node").([]interface{})
	for _, item := range raw {
		m := item.(map[string]interface{})
		n := &pb.CephClusterNodeSpec{
			NodeId:       m["node_id"].(string),
			MonAddr:      m["mon_addr"].(string),
			ClusterAddr:  m["cluster_addr"].(string),
			PublicIface:  m["public_iface"].(string),
			ClusterIface: m["cluster_iface"].(string),
			OsdDevice:    m["osd_device"].(string),
			Role:         mapStr(m, "role"),
			Weight:       mapFloat(m, "weight"),
			DeviceClass:  mapStr(m, "device_class"),
			Rack:         mapStr(m, "rack"),
			Room:         mapStr(m, "room"),
		}
		if devs, ok := m["osd_devices"].([]interface{}); ok {
			for _, dev := range devs {
				n.OsdDevices = append(n.OsdDevices, dev.(string))
			}
		}
		spec.Nodes = append(spec.Nodes, n)
	}
	pools, _ := d.Get("pool").([]interface{})
	for _, item := range pools {
		m := item.(map[string]interface{})
		spec.Pools = append(spec.Pools, &pb.CephPoolSpec{
			Name:            mapStr(m, "name"),
			Application:     mapStr(m, "application"),
			Size:            toInt32(mapInt(m, "size")),
			MinSize:         toInt32(mapInt(m, "min_size")),
			PgNum:           toInt32(mapInt(m, "pg_num")),
			CrushRule:       mapStr(m, "crush_rule"),
			CompressionMode: mapStr(m, "compression_mode"),
			QuotaBytes:      int64(mapInt(m, "quota_bytes")),
			PoolType:        mapStr(m, "pool_type"),
			ErasureProfile:  mapStr(m, "erasure_profile"),
		})
	}
	if rec, ok := d.Get("recovery").([]interface{}); ok && len(rec) == 1 && rec[0] != nil {
		m := rec[0].(map[string]interface{})
		spec.Recovery = &pb.CephRecoverySpec{
			MaxBackfills:      toInt32(mapInt(m, "max_backfills")),
			RecoveryMaxActive: toInt32(mapInt(m, "recovery_max_active")),
			ScrubBeginHour:    toInt32(mapInt(m, "scrub_begin_hour")),
			ScrubEndHour:      toInt32(mapInt(m, "scrub_end_hour")),
			NearfullRatioBp:   toInt32(mapInt(m, "nearfull_ratio_bp")),
		}
	}
	if ext, ok := d.Get("external").([]interface{}); ok && len(ext) == 1 && ext[0] != nil {
		m := ext[0].(map[string]interface{})
		spec.External = &pb.CephExternalSpec{
			MonHosts:      mapStr(m, "mon_hosts"),
			Pool:          mapStr(m, "pool"),
			ClientName:    mapStr(m, "client_name"),
			ClientKeyring: mapStr(m, "client_keyring"),
		}
	}
	return &pb.CephCluster{Name: s(d, "name"), Spec: spec}
}

func cephValidate(spec *pb.CephClusterSpec) error {
	if spec.External != nil && len(spec.Nodes) > 0 {
		return fmt.Errorf("external and node are mutually exclusive")
	}
	if spec.External == nil && len(spec.Nodes) == 0 {
		return fmt.Errorf("at least one node is required unless external is set")
	}
	for _, p := range spec.Pools {
		if p.PoolType == "erasure" && p.ErasureProfile == "" {
			return fmt.Errorf("pool %q: an erasure pool needs erasure_profile", p.Name)
		}
	}
	return nil
}

func cephHealthState(h *pb.CephTypedHealth) []interface{} {
	if h == nil {
		return nil
	}
	byState := map[string]interface{}{}
	for _, row := range h.PgsByState {
		byState[row.State] = int(row.Count)
	}
	checks := make([]interface{}, 0, len(h.Checks))
	for _, c := range h.Checks {
		checks = append(checks, map[string]interface{}{"severity": c.Severity, "type": c.Type, "summary": c.Summary})
	}
	return []interface{}{map[string]interface{}{
		"status":        h.HealthStatus,
		"mon_quorum":    h.MonQuorum,
		"mon_count":     int(h.MonCount),
		"osd_total":     int(h.OsdTotal),
		"osd_up":        int(h.OsdUp),
		"osd_in":        int(h.OsdIn),
		"pgs_total":     int(h.PgsTotal),
		"pgs_not_clean": int(h.PgsNotClean),
		"backfill_pgs":  int(h.BackfillPgs),
		"pgs_by_state":  byState,
		"noout":         h.Noout,
		"norebalance":   h.Norebalance,
		"flags":         h.Flags,
		"bytes_total":   int(h.BytesTotal),
		"bytes_used":    int(h.BytesUsed),
		"bytes_avail":   int(h.BytesAvail),
		"check":         checks,
	}}
}

func mapStr(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func mapInt(m map[string]interface{}, key string) int {
	v, _ := m[key].(int)
	return v
}

func mapFloat(m map[string]interface{}, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func resourceCephUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	cluster := cephFromState(d)
	if err := cephValidate(cluster.Spec); err != nil {
		return diag.FromErr(err)
	}
	resp, err := api(meta).controller.CreateCephCluster(ctx, &pb.CreateCephClusterRequest{CephCluster: cluster})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceCephRead(ctx, d, meta)
}

func resourceCephRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetCephCluster(ctx, &pb.GetCephClusterRequest{Name: d.Id()})
	if err != nil || resp.GetCephCluster() == nil || resp.CephCluster.Name == "" {
		d.SetId("")
		return nil
	}
	c := resp.CephCluster
	if c.Spec != nil {
		d.Set("fsid", c.Spec.Fsid)
		d.Set("public_network", c.Spec.PublicNetwork)
		d.Set("cluster_network", c.Spec.ClusterNetwork)
		d.Set("size", c.Spec.Size)
		d.Set("min_size", c.Spec.MinSize)
		d.Set("encrypt_osds", c.Spec.EncryptOsds)
		d.Set("ms_mode", c.Spec.MsMode)
		d.Set("image_features", c.Spec.ImageFeatures)
		d.Set("cache_policy", c.Spec.CachePolicy)
		d.Set("lost_members", c.Spec.LostMembers)
		d.Set("failure_domain", c.Spec.FailureDomain)
		d.Set("stretch", c.Spec.Stretch)
		if c.Spec.Mirroring != nil {
			d.Set("mirroring", []interface{}{map[string]interface{}{
				"enabled":      c.Spec.Mirroring.Enabled,
				"mode":         c.Spec.Mirroring.Mode,
				"peer_cluster": c.Spec.Mirroring.PeerCluster,
			}})
		}
	}
	if c.Status != nil {
		d.Set("phase", c.Status.Phase.String())
		d.Set("health_message", c.Status.HealthMessage)
		d.Set("operation_message", c.Status.OperationMessage)
		d.Set("health", cephHealthState(c.Status.TypedHealth))
	}
	return nil
}

func resourceCephDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteCephCluster(ctx, &pb.DeleteCephClusterRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceSharedFilesystem() *schema.Resource {
	return &schema.Resource{
		Description:   "CephFS share (`kctl create shared-filesystem`).",
		CreateContext: resourceFSUpsert,
		ReadContext:   resourceFSRead,
		UpdateContext: resourceFSUpsert,
		DeleteContext: resourceFSDelete,
		Schema: map[string]*schema.Schema{
			"name":         {Type: schema.TypeString, Required: true, ForceNew: true},
			"ceph_cluster": {Type: schema.TypeString, Required: true},
			"quota_bytes":  {Type: schema.TypeInt, Optional: true},
			"fs_name":      {Type: schema.TypeString, Optional: true},
			"client": {
				Type: schema.TypeList, Optional: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"name":  {Type: schema.TypeString, Required: true},
					"paths": {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}},
				}},
			},
			"phase":          {Type: schema.TypeString, Computed: true},
			"health_message": {Type: schema.TypeString, Computed: true},
		},
	}
}

func fsFromState(d *schema.ResourceData) *pb.SharedFilesystem {
	spec := &pb.SharedFilesystemSpec{
		CephCluster: s(d, "ceph_cluster"),
		QuotaBytes:  i64(d, "quota_bytes"),
		FsName:      s(d, "fs_name"),
	}
	raw, _ := d.Get("client").([]interface{})
	for _, item := range raw {
		m := item.(map[string]interface{})
		c := &pb.SharedFilesystemClientSpec{Name: m["name"].(string)}
		if paths, ok := m["paths"].([]interface{}); ok {
			for _, p := range paths {
				c.Paths = append(c.Paths, p.(string))
			}
		}
		spec.Clients = append(spec.Clients, c)
	}
	return &pb.SharedFilesystem{Name: s(d, "name"), Spec: spec}
}

func resourceFSUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateSharedFilesystem(ctx, &pb.CreateSharedFilesystemRequest{SharedFilesystem: fsFromState(d)})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceFSRead(ctx, d, meta)
}

func resourceFSRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetSharedFilesystem(ctx, &pb.GetSharedFilesystemRequest{Name: d.Id()})
	if err != nil || resp.GetSharedFilesystem() == nil || resp.SharedFilesystem.Name == "" {
		d.SetId("")
		return nil
	}
	fs := resp.SharedFilesystem
	if fs.Spec != nil {
		d.Set("ceph_cluster", fs.Spec.CephCluster)
		d.Set("quota_bytes", fs.Spec.QuotaBytes)
		d.Set("fs_name", fs.Spec.FsName)
	}
	if fs.Status != nil {
		d.Set("phase", fs.Status.Phase.String())
		d.Set("health_message", fs.Status.HealthMessage)
	}
	return nil
}

func resourceFSDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteSharedFilesystem(ctx, &pb.DeleteSharedFilesystemRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceObjectStore() *schema.Resource {
	return &schema.Resource{
		Description:   "RGW object store (`kctl create object-store`).",
		CreateContext: resourceObjStoreUpsert,
		ReadContext:   resourceObjStoreRead,
		UpdateContext: resourceObjStoreUpsert,
		DeleteContext: resourceObjStoreDelete,
		Schema: map[string]*schema.Schema{
			"name":           {Type: schema.TypeString, Required: true, ForceNew: true},
			"ceph_cluster":   {Type: schema.TypeString, Required: true},
			"members":        {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}},
			"port":           {Type: schema.TypeInt, Optional: true},
			"tls":            {Type: schema.TypeBool, Optional: true},
			"rgw_id":         {Type: schema.TypeString, Optional: true},
			"phase":          {Type: schema.TypeString, Computed: true},
			"health_message": {Type: schema.TypeString, Computed: true},
		},
	}
}

func resourceObjStoreUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateObjectStore(ctx, &pb.CreateObjectStoreRequest{
		ObjectStore: &pb.ObjectStore{
			Name: s(d, "name"),
			Spec: &pb.ObjectStoreSpec{
				CephCluster: s(d, "ceph_cluster"),
				Members:     strList(d, "members"),
				Port:        i32(d, "port"),
				Tls:         b(d, "tls"),
				RgwId:       s(d, "rgw_id"),
			},
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceObjStoreRead(ctx, d, meta)
}

func resourceObjStoreRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetObjectStore(ctx, &pb.GetObjectStoreRequest{Name: d.Id()})
	if err != nil || resp.GetObjectStore() == nil || resp.ObjectStore.Name == "" {
		d.SetId("")
		return nil
	}
	o := resp.ObjectStore
	if o.Spec != nil {
		d.Set("ceph_cluster", o.Spec.CephCluster)
		d.Set("port", o.Spec.Port)
		d.Set("tls", o.Spec.Tls)
		d.Set("rgw_id", o.Spec.RgwId)
		d.Set("members", o.Spec.Members)
	}
	if o.Status != nil {
		d.Set("phase", o.Status.Phase.String())
		d.Set("health_message", o.Status.HealthMessage)
	}
	return nil
}

func resourceObjStoreDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteObjectStore(ctx, &pb.DeleteObjectStoreRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceObjectUser() *schema.Resource {
	return &schema.Resource{
		Description:   "RGW user (`kctl create object-user`). The secret is only returned at create time.",
		CreateContext: resourceObjUserCreate,
		ReadContext:   resourceObjUserRead,
		DeleteContext: resourceObjUserDelete,
		Schema: map[string]*schema.Schema{
			"name":       {Type: schema.TypeString, Required: true, ForceNew: true},
			"store":      {Type: schema.TypeString, Required: true, ForceNew: true},
			"access_key": {Type: schema.TypeString, Computed: true},
			"secret":     {Type: schema.TypeString, Computed: true, Sensitive: true},
		},
	}
}

func resourceObjUserCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateObjectUser(ctx, &pb.CreateObjectUserRequest{
		ObjectUser: &pb.ObjectUser{Name: s(d, "name"), Spec: &pb.ObjectUserSpec{Store: s(d, "store")}},
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "store") + "/" + s(d, "name"))
	if resp.ObjectUser != nil {
		d.Set("access_key", resp.ObjectUser.AccessKey)
	}
	d.Set("secret", resp.Secret)
	return nil
}

func resourceObjUserRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	return nil
}

func resourceObjUserDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteObjectUser(ctx, &pb.DeleteObjectUserRequest{
		Name:  s(d, "name"),
		Store: s(d, "store"),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
