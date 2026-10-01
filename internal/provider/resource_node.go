package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
	nodepb "github.com/kcorehypervisor/terraform-provider-kcore/api/node"
)

func resourceNode() *schema.Resource {
	return &schema.Resource{
		Description:   "Approve a node that has registered with the controller (`kctl node approve`). Destroy removes it from the cluster.",
		CreateContext: resourceNodeCreate,
		ReadContext:   resourceNodeRead,
		UpdateContext: resourceNodeUpdate,
		DeleteContext: resourceNodeDelete,
		Schema: map[string]*schema.Schema{
			"node_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"cordoned": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "When true, the node is cordoned after approval.",
			},
			"hostname": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"address": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"approval_status": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"status": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceNodeCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c := api(meta).controller
	id := s(d, "node_id")
	resp, err := c.ApproveNode(ctx, &pb.ApproveNodeRequest{NodeId: id})
	if err != nil {
		return diag.FromErr(fmt.Errorf("approve node: %w", err))
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	if b(d, "cordoned") {
		cr, err := c.CordonNode(ctx, &pb.CordonNodeRequest{NodeId: id})
		if err != nil {
			return diag.FromErr(err)
		}
		if err := fail(cr.Success, cr.Message); err != nil {
			return diag.FromErr(err)
		}
	}
	d.SetId(id)
	return resourceNodeRead(ctx, d, meta)
}

func resourceNodeRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetNode(ctx, &pb.GetNodeRequest{NodeId: d.Id()})
	if err != nil || resp.GetNode() == nil || resp.Node.NodeId == "" {
		d.SetId("")
		return nil
	}
	n := resp.Node
	d.Set("node_id", n.NodeId)
	d.Set("hostname", n.Hostname)
	d.Set("address", n.Address)
	d.Set("approval_status", n.ApprovalStatus)
	d.Set("status", n.Status)
	return nil
}

func resourceNodeUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	if d.HasChange("cordoned") {
		c := api(meta).controller
		id := d.Id()
		if b(d, "cordoned") {
			resp, err := c.CordonNode(ctx, &pb.CordonNodeRequest{NodeId: id})
			if err != nil {
				return diag.FromErr(err)
			}
			if err := fail(resp.Success, resp.Message); err != nil {
				return diag.FromErr(err)
			}
		} else {
			resp, err := c.UncordonNode(ctx, &pb.UncordonNodeRequest{NodeId: id})
			if err != nil {
				return diag.FromErr(err)
			}
			if err := fail(resp.Success, resp.Message); err != nil {
				return diag.FromErr(err)
			}
		}
	}
	return resourceNodeRead(ctx, d, meta)
}

func resourceNodeDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteNode(ctx, &pb.DeleteNodeRequest{NodeId: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceNodeInstall() *schema.Resource {
	return &schema.Resource{
		Description:   "Install kcore onto a node that is booted from the ISO (`kctl node install`). This wipes os_disk. Destroy only drops the Terraform state; it does not roll the install back.",
		CreateContext: resourceNodeInstallCreate,
		ReadContext:   resourceNodeInstallRead,
		DeleteContext: resourceNodeInstallDelete,
		Schema: map[string]*schema.Schema{
			"address": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Live installer address. Port defaults to 9091.",
			},
			"os_disk": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"data_disk": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"certs_dir": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Directory produced by kcore_cluster.",
			},
			"run_controller": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
			},
			"join_controller": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Controller host:9090 to join. Required unless run_controller is true.",
			},
			"storage_backend": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Default:      "filesystem",
				ValidateFunc: validation.StringInSlice([]string{"filesystem", "lvm", "zfs", "ceph"}, false),
			},
			"dc_id": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Default:  "DC1",
			},
			"hostname": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"node_id": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"disable_vxlan": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
			},
			"insecure": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Default:     true,
				Description: "Dial the live installer without TLS. The ISO agent has no cluster certificate yet.",
			},
			"luks_method": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"message": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceNodeInstallCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	runController := b(d, "run_controller")
	join := strings.TrimSpace(s(d, "join_controller"))
	if !runController && join == "" {
		return diag.FromErr(fmt.Errorf("join_controller is required when run_controller is false"))
	}
	dir := s(d, "certs_dir")
	addr := s(d, "address")
	if !strings.Contains(addr, ":") {
		addr = addr + ":9091"
	}
	host := hostFromAddress(addr)
	nodeID := strings.TrimSpace(s(d, "node_id"))
	if nodeID == "" {
		nodeID = "kcore-node-" + host
	}

	caPEM, err := readPEM(dir, "ca.crt")
	if err != nil {
		return diag.FromErr(err)
	}
	var nodeCert, nodeKey, ctrlCert, ctrlKey, subCert, subKey string
	if runController {
		ctrlCert, ctrlKey, err = signHostLeaf(dir, "kcore-controller-"+host, host, true, true)
		if err != nil {
			return diag.FromErr(fmt.Errorf("controller certificate: %w", err))
		}
		nodeCert, nodeKey, err = signHostLeaf(dir, "kcore-node-"+host, host, true, true)
		if err != nil {
			return diag.FromErr(fmt.Errorf("node certificate: %w", err))
		}
		subCert, err = readPEM(dir, "sub-ca.crt")
		if err != nil {
			return diag.FromErr(err)
		}
		subKey, err = readPEM(dir, "sub-ca.key")
		if err != nil {
			return diag.FromErr(err)
		}
	} else {
		issued, err := api(meta).controller.IssueNodeBootstrapCert(ctx, &pb.IssueNodeBootstrapCertRequest{
			NodeId:   nodeID,
			NodeHost: host,
		})
		if err != nil {
			return diag.FromErr(fmt.Errorf("issue node bootstrap cert: %w", err))
		}
		if err := fail(issued.Success, issued.Message); err != nil {
			return diag.FromErr(err)
		}
		nodeCert, nodeKey = issued.CertPem, issued.KeyPem
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if !b(d, "insecure") {
		return diag.FromErr(fmt.Errorf("TLS to the live installer is not implemented; set insecure = true"))
	}
	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		return diag.FromErr(fmt.Errorf("dial installer %s: %w", addr, err))
	}
	defer conn.Close()

	backend := nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_FILESYSTEM
	switch s(d, "storage_backend") {
	case "lvm":
		backend = nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_LVM
	case "zfs":
		backend = nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_ZFS
	case "ceph":
		backend = nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_CEPH
	}

	var controllers []string
	if join != "" {
		controllers = []string{join}
	}
	resp, err := nodepb.NewNodeAdminClient(conn).InstallToDisk(ctx, &nodepb.InstallToDiskRequest{
		OsDisk:            s(d, "os_disk"),
		DataDisks:         strList(d, "data_disk"),
		Controller:        join,
		Controllers:       controllers,
		RunController:     runController,
		CaCertPem:         caPEM,
		NodeCertPem:       nodeCert,
		NodeKeyPem:        nodeKey,
		ControllerCertPem: ctrlCert,
		ControllerKeyPem:  ctrlKey,
		SubCaCertPem:      subCert,
		SubCaKeyPem:       subKey,
		DataDiskMode:      s(d, "storage_backend"),
		StorageBackend:    backend,
		DisableVxlan:      b(d, "disable_vxlan"),
		DcId:              s(d, "dc_id"),
		Hostname:          s(d, "hostname"),
		NodeId:            nodeID,
	})
	if err != nil {
		return diag.FromErr(fmt.Errorf("install to disk: %w", err))
	}
	if !resp.Accepted {
		return diag.FromErr(fmt.Errorf("install refused: %s", resp.Message))
	}
	d.SetId(nodeID)
	d.Set("luks_method", resp.LuksMethod)
	d.Set("message", resp.Message)
	return nil
}

func resourceNodeInstallRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	return nil
}

func resourceNodeInstallDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	d.SetId("")
	return diag.Diagnostics{{
		Severity: diag.Warning,
		Summary:  "Node install was removed from Terraform state only",
		Detail:   "The installed disk was not wiped again. Remove the node from the cluster with kcore_node if it joined.",
	}}
}
