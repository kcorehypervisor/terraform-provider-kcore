package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceCephCluster() *schema.Resource {
	return &schema.Resource{
		Description:   "Ceph cluster (`kctl create ceph-cluster`).",
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
				Type:     schema.TypeList,
				Required: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"node_id":       {Type: schema.TypeString, Required: true},
					"mon_addr":      {Type: schema.TypeString, Optional: true},
					"cluster_addr":  {Type: schema.TypeString, Optional: true},
					"public_iface":  {Type: schema.TypeString, Optional: true},
					"cluster_iface": {Type: schema.TypeString, Optional: true},
					"osd_device":    {Type: schema.TypeString, Required: true},
					"osd_devices":   {Type: schema.TypeList, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}},
				}},
			},
			"phase":          {Type: schema.TypeString, Computed: true},
			"health_message": {Type: schema.TypeString, Computed: true},
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
		}
		if devs, ok := m["osd_devices"].([]interface{}); ok {
			for _, dev := range devs {
				n.OsdDevices = append(n.OsdDevices, dev.(string))
			}
		}
		spec.Nodes = append(spec.Nodes, n)
	}
	return &pb.CephCluster{Name: s(d, "name"), Spec: spec}
}

func resourceCephUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateCephCluster(ctx, &pb.CreateCephClusterRequest{CephCluster: cephFromState(d)})
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
	}
	if c.Status != nil {
		d.Set("phase", c.Status.Phase.String())
		d.Set("health_message", c.Status.HealthMessage)
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
