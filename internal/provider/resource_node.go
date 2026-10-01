package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
	nodepb "github.com/kcorehypervisor/terraform-provider-kcore/api/node"
)

func resourceNode() *schema.Resource {
	return &schema.Resource{
		Description: "Install a node from the live ISO (`kctl node install`). " +
			"Set bootstrap on the first controller. Other nodes set join_controller to that node's controller_address so Terraform creates the bootstrap node first. " +
			"Destroy drops Terraform state and leaves the installed disk in place.",
		CreateContext: resourceNodeCreate,
		ReadContext:   resourceNodeRead,
		DeleteContext: resourceNodeDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
		},
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
			"bootstrap": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Description: "Install this node as the first controller. It does not join an existing controller.",
			},
			"run_controller": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Description: "Run a controller on this node. Implied by bootstrap. Additional controllers must also set join_controller.",
			},
			"join_controller": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Controller host:9090 to join. Set this to the bootstrap node's controller_address. Required unless bootstrap is true.",
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
				Computed: true,
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
			"controller_address": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "host:9090 for this node's controller. Joiners reference the bootstrap node's controller_address.",
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

func resourceNodeCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	bootstrap := b(d, "bootstrap")
	runController := bootstrap || b(d, "run_controller")
	join := strings.TrimSpace(s(d, "join_controller"))
	if bootstrap && join != "" {
		return diag.FromErr(fmt.Errorf("bootstrap node does not set join_controller"))
	}
	if !bootstrap && join == "" {
		return diag.FromErr(fmt.Errorf("join_controller is required unless bootstrap is true"))
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

	if !b(d, "insecure") {
		return diag.FromErr(fmt.Errorf("TLS to the live installer is not implemented; set insecure = true"))
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
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
	controllerAddr := join
	if runController {
		controllerAddr = host + ":9090"
	}
	if bootstrap {
		if err := waitForController(ctx, controllerAddr, dir); err != nil {
			return diag.FromErr(fmt.Errorf("installer accepted node %s; controller %s did not become ready: %w", nodeID, controllerAddr, err))
		}
	}
	d.SetId(nodeID)
	d.Set("node_id", nodeID)
	d.Set("controller_address", controllerAddr)
	d.Set("luks_method", resp.LuksMethod)
	d.Set("message", resp.Message)
	return nil
}

func waitForController(ctx context.Context, addr, dir string) error {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "kctl.crt"), filepath.Join(dir, "kctl.key"))
	if err != nil {
		return fmt.Errorf("load kctl certificate: %w", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("parse ca.crt")
	}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   hostFromAddress(addr),
		MinVersion:   tls.VersionTLS12,
	})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var last error
	for {
		dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := grpc.DialContext(dialCtx, addr, grpc.WithTransportCredentials(creds), grpc.WithBlock())
		if err == nil {
			_, err = pb.NewControllerClient(conn).GetClusterHealth(dialCtx, &pb.GetClusterHealthRequest{})
			conn.Close()
		}
		cancel()
		if err == nil {
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last dial: %v)", ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

func resourceNodeRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	return nil
}

func resourceNodeDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	d.SetId("")
	return diag.Diagnostics{{
		Severity: diag.Warning,
		Summary:  "Node was removed from Terraform state only",
		Detail:   "The installed disk was not wiped. The node stays on the cluster until it is removed there.",
	}}
}
