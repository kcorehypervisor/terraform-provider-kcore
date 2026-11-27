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

func resourceCluster() *schema.Resource {
	return &schema.Resource{
		Description: "Declare a kcore cluster: local PKI, nodes installed from the live ISO, and the desired release. " +
			"Same shape as a `kind: Cluster` manifest. Changing `version` or the `update` block calls SetClusterVersion. " +
			"Destroy removes local certificates only; installed nodes stay on disk.",
		CreateContext: resourceClusterCreate,
		ReadContext:   resourceClusterRead,
		UpdateContext: resourceClusterApplyUpdate,
		DeleteContext: resourceClusterDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(60 * time.Minute),
			Update: schema.DefaultTimeout(60 * time.Minute),
		},
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Cluster context name.",
			},
			"controller": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Controller address for the local controller certificate (host or host:9090). Defaults to the first node with controller = true.",
			},
			"certs_dir": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Directory for ca.crt, controller.crt, kctl.crt, and their keys.",
			},
			"force": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Description: "Overwrite certificates that already exist in certs_dir.",
			},
			"version": {
				Type:             schema.TypeString,
				Optional:         true,
				Description:      "Desired release tag (`v0.9.0` or `0.9.0`). Changing it starts a canary rollout.",
				DiffSuppressFunc: func(_, old, new string, _ *schema.ResourceData) bool { return releaseTag(old) == releaseTag(new) },
			},
			"update": {
				Type:        schema.TypeList,
				Optional:    true,
				MaxItems:    1,
				Description: "Desired update policy stored with the version.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"reboot": {
							Type:        schema.TypeBool,
							Optional:    true,
							Description: "Force every node a rollout updates to stage a boot and reboot. False keeps auto activation.",
						},
						"drain_vms": {
							Type:        schema.TypeBool,
							Optional:    true,
							Description: "Migrate guests off a node before it reboots into a new kernel.",
						},
						"canary_soak_seconds": {
							Type:          schema.TypeInt,
							Optional:      true,
							Description:   "How long the canary soaks. 0 means the controller default. At most 7 days.",
							ValidateFunc:  validation.IntBetween(0, maxCanarySoakSeconds),
							ConflictsWith: []string{"update.0.no_canary"},
						},
						"canary_node": {
							Type:          schema.TypeString,
							Optional:      true,
							Description:   "Canary node id. Empty picks a labelled node or the first in rollout order.",
							ConflictsWith: []string{"update.0.no_canary"},
						},
						"no_canary": {
							Type:        schema.TypeBool,
							Optional:    true,
							Description: "Update every node one at a time with no canary and no soak.",
						},
						"flake_ref": {
							Type:        schema.TypeString,
							Optional:    true,
							Description: "Repository the tag is resolved in. Empty means github:kcorehypervisor/kcore.",
						},
						"channel": {
							Type:        schema.TypeString,
							Optional:    true,
							Description: "`community`, `enterprise`, or empty for the controller default.",
						},
					},
				},
			},
			"node": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "Nodes to install from the live ISO when not already registered. Controllers install first.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Node id registered with the controller.",
						},
						"address": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Live installer address. Port defaults to 9091.",
						},
						"os_disk": {
							Type:     schema.TypeString,
							Required: true,
						},
						"controller": {
							Type:        schema.TypeBool,
							Optional:    true,
							Description: "Install and run a controller on this node.",
						},
						"data_disk": {
							Type:     schema.TypeList,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
						"storage_backend": {
							Type:         schema.TypeString,
							Optional:     true,
							Default:      "filesystem",
							ValidateFunc: validation.StringInSlice([]string{"filesystem", "lvm", "zfs", "ceph"}, false),
						},
						"dc_id": {
							Type:     schema.TypeString,
							Optional: true,
							Default:  "DC1",
						},
						"hostname": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"disable_vxlan": {
							Type:     schema.TypeBool,
							Optional: true,
						},
					},
				},
			},
			"ca_cert_path": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"client_cert_path": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "kctl client certificate. Pass this to the provider tls_cert_path.",
			},
			"client_key_path": {
				Type:      schema.TypeString,
				Computed:  true,
				Sensitive: true,
			},
			"controller_cert_path": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"controller_key_path": {
				Type:      schema.TypeString,
				Computed:  true,
				Sensitive: true,
			},
			"flake_rev": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Commit the desired tag resolved to.",
			},
			"update_name": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "ClusterUpdate of the latest version rollout.",
			},
			"phase": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Phase of the latest version rollout.",
			},
			"in_sync": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "True when every approved heartbeating node reports the desired version.",
			},
			"status_message": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Why the cluster is not converging, empty when in sync.",
			},
		},
	}
}

type clusterNode struct {
	Name           string
	Address        string
	OsDisk         string
	Controller     bool
	DataDisks      []string
	StorageBackend string
	DcID           string
	Hostname       string
	DisableVxlan   bool
}

func resourceClusterCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	dir := s(d, "certs_dir")
	controllerAddr, err := clusterControllerAddr(d)
	if err != nil {
		return diag.FromErr(err)
	}
	_ = d.Set("controller", controllerAddr)

	if clusterCertsPresent(dir) && !b(d, "force") {
		// Idempotent: keep existing PKI.
	} else if !clusterCertsPresent(dir) || b(d, "force") {
		if err := generateClusterPKI(dir, controllerAddr); err != nil {
			return diag.FromErr(err)
		}
	}

	d.SetId(s(d, "name"))
	setClusterCertPaths(d, dir)

	nodes := clusterNodesFromConfig(d)
	if diags := ensureClusterNodes(ctx, d, dir, nodes); diags.HasError() {
		return diags
	}
	if version := strings.TrimSpace(s(d, "version")); version != "" {
		if diags := setClusterDesiredVersion(ctx, d, dir); diags.HasError() {
			return diags
		}
	}
	return resourceClusterRead(ctx, d, meta)
}

func resourceClusterApplyUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	dir := s(d, "certs_dir")
	setClusterCertPaths(d, dir)

	oldRaw, newRaw := d.GetChange("node")
	oldNodes := clusterNodesFromRaw(oldRaw)
	newNodes := clusterNodesFromRaw(newRaw)
	if err := rejectNodeImmutableChanges(oldNodes, newNodes); err != nil {
		return diag.FromErr(err)
	}

	var diags diag.Diagnostics
	removed := nodesMissingFrom(oldNodes, newNodes)
	for _, n := range removed {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("node %s removed from Terraform state only", n.Name),
			Detail:   "The installed disk was not wiped. The node stays on the cluster until it is removed there.",
		})
	}

	added := nodesMissingFrom(newNodes, oldNodes)
	if len(added) > 0 {
		if diags2 := ensureClusterNodes(ctx, d, dir, orderControllersFirst(added)); diags2.HasError() {
			return append(diags, diags2...)
		}
	}

	if d.HasChange("version") || d.HasChange("update") {
		if strings.TrimSpace(s(d, "version")) == "" {
			return append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "version is required when changing the update policy",
			})
		}
		if diags2 := setClusterDesiredVersion(ctx, d, dir); diags2.HasError() {
			return append(diags, diags2...)
		}
	}

	readDiags := resourceClusterRead(ctx, d, meta)
	return append(diags, readDiags...)
}

func resourceClusterRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = meta
	dir := s(d, "certs_dir")
	if !clusterCertsPresent(dir) {
		d.SetId("")
		return nil
	}
	setClusterCertPaths(d, dir)

	if strings.TrimSpace(s(d, "version")) == "" && len(clusterNodesFromConfig(d)) == 0 {
		return nil
	}
	ctrl, err := dialControllerFromCerts(ctx, dir, s(d, "controller"))
	if err != nil {
		// Certs exist but the controller is down; keep state.
		return nil
	}
	defer func() { _ = ctrl.conn.Close() }()

	resp, err := ctrl.client.GetClusterVersion(ctx, &pb.GetClusterVersionRequest{})
	if err != nil {
		return nil
	}
	if resp.Version != "" {
		_ = d.Set("version", resp.Version)
	}
	_ = d.Set("flake_rev", resp.FlakeRev)
	_ = d.Set("update_name", resp.UpdateName)
	_ = d.Set("phase", resp.Phase.String())
	_ = d.Set("in_sync", resp.InSync)
	_ = d.Set("status_message", resp.StatusMessage)
	if _, ok := d.GetOk("update"); ok {
		_ = d.Set("update", []interface{}{
			map[string]interface{}{
				"reboot":              resp.Reboot,
				"drain_vms":           resp.DrainVms,
				"canary_soak_seconds": int(resp.CanarySoakSeconds),
				"canary_node":         resp.CanaryNodeId,
				"no_canary":           resp.NoCanary,
				"flake_ref":           resp.FlakeRef,
				"channel":             resp.Channel,
			},
		})
	}
	return nil
}

func resourceClusterDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	dir := s(d, "certs_dir")
	for _, name := range clusterCertFiles {
		_ = os.Remove(filepath.Join(dir, name))
	}
	d.SetId("")
	return nil
}

func setClusterCertPaths(d *schema.ResourceData, dir string) {
	_ = d.Set("ca_cert_path", filepath.Join(dir, "ca.crt"))
	_ = d.Set("client_cert_path", filepath.Join(dir, "kctl.crt"))
	_ = d.Set("client_key_path", filepath.Join(dir, "kctl.key"))
	_ = d.Set("controller_cert_path", filepath.Join(dir, "controller.crt"))
	_ = d.Set("controller_key_path", filepath.Join(dir, "controller.key"))
}

func clusterControllerAddr(d *schema.ResourceData) (string, error) {
	if addr := strings.TrimSpace(s(d, "controller")); addr != "" {
		return addr, nil
	}
	for _, n := range clusterNodesFromConfig(d) {
		if n.Controller {
			return hostFromAddress(normalizeInstallerAddr(n.Address)) + ":9090", nil
		}
	}
	return "", fmt.Errorf("controller is required unless a node block sets controller = true")
}

func clusterNodesFromConfig(d *schema.ResourceData) []clusterNode {
	return clusterNodesFromRaw(d.Get("node"))
}

func clusterNodesFromRaw(raw interface{}) []clusterNode {
	items, _ := raw.([]interface{})
	out := make([]clusterNode, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		var disks []string
		if rawDisks, ok := m["data_disk"].([]interface{}); ok {
			for _, d := range rawDisks {
				if s, ok := d.(string); ok && s != "" {
					disks = append(disks, s)
				}
			}
		}
		backend, _ := m["storage_backend"].(string)
		if backend == "" {
			backend = "filesystem"
		}
		dc, _ := m["dc_id"].(string)
		if dc == "" {
			dc = "DC1"
		}
		out = append(out, clusterNode{
			Name:           strings.TrimSpace(fmt.Sprint(m["name"])),
			Address:        strings.TrimSpace(fmt.Sprint(m["address"])),
			OsDisk:         strings.TrimSpace(fmt.Sprint(m["os_disk"])),
			Controller:     m["controller"] == true,
			DataDisks:      disks,
			StorageBackend: backend,
			DcID:           dc,
			Hostname:       strings.TrimSpace(fmt.Sprint(m["hostname"])),
			DisableVxlan:   m["disable_vxlan"] == true,
		})
	}
	return out
}

func orderControllersFirst(nodes []clusterNode) []clusterNode {
	var controllers, agents []clusterNode
	for _, n := range nodes {
		if n.Controller {
			controllers = append(controllers, n)
		} else {
			agents = append(agents, n)
		}
	}
	return append(controllers, agents...)
}

func nodesMissingFrom(have, against []clusterNode) []clusterNode {
	set := map[string]clusterNode{}
	for _, n := range against {
		set[n.Name] = n
	}
	var missing []clusterNode
	for _, n := range have {
		if _, ok := set[n.Name]; !ok {
			missing = append(missing, n)
		}
	}
	return missing
}

func rejectNodeImmutableChanges(oldNodes, newNodes []clusterNode) error {
	oldByName := map[string]clusterNode{}
	for _, n := range oldNodes {
		oldByName[n.Name] = n
	}
	for _, n := range newNodes {
		prev, ok := oldByName[n.Name]
		if !ok {
			continue
		}
		if prev.OsDisk != n.OsDisk {
			return fmt.Errorf("changing os_disk on node %s is not supported (was %s, now %s)", n.Name, prev.OsDisk, n.OsDisk)
		}
		if prev.Controller != n.Controller {
			return fmt.Errorf("changing controller on node %s is not supported", n.Name)
		}
		if normalizeInstallerAddr(prev.Address) != normalizeInstallerAddr(n.Address) {
			return fmt.Errorf("changing address on node %s is not supported", n.Name)
		}
	}
	return nil
}

func ensureClusterNodes(ctx context.Context, d *schema.ResourceData, dir string, nodes []clusterNode) diag.Diagnostics {
	nodes = orderControllersFirst(nodes)
	joinControllers := controllerJoinAddresses(nodes)
	bootstrapDone := false
	for _, n := range nodes {
		registered, err := clusterNodeRegistered(ctx, dir, s(d, "controller"), n)
		if err == nil && registered {
			continue
		}
		live, liveErr := liveISOReachable(ctx, n.Address)
		if liveErr != nil || !live {
			return diag.FromErr(fmt.Errorf(
				"node %s at %s is not registered and the live ISO agent did not answer; boot it from the kcore ISO",
				n.Name, normalizeInstallerAddr(n.Address),
			))
		}
		bootstrap := n.Controller && !bootstrapDone
		if err := installClusterNode(ctx, dir, n, joinControllers, bootstrap); err != nil {
			return diag.FromErr(err)
		}
		if n.Controller {
			bootstrapDone = true
			ctrlAddr := hostFromAddress(normalizeInstallerAddr(n.Address)) + ":9090"
			_ = d.Set("controller", ctrlAddr)
			if err := waitForController(ctx, ctrlAddr, dir); err != nil {
				return diag.FromErr(fmt.Errorf("installer accepted node %s; controller %s did not become ready: %w", n.Name, ctrlAddr, err))
			}
		} else {
			if err := waitForClusterNodeRegistration(ctx, dir, s(d, "controller"), n); err != nil {
				return diag.FromErr(err)
			}
		}
	}
	return nil
}

func controllerJoinAddresses(nodes []clusterNode) []string {
	var out []string
	for _, n := range nodes {
		if n.Controller {
			out = append(out, hostFromAddress(normalizeInstallerAddr(n.Address))+":9090")
		}
	}
	return out
}

func normalizeInstallerAddr(addr string) string {
	if !strings.Contains(addr, ":") {
		return addr + ":9091"
	}
	return addr
}

func setClusterDesiredVersion(ctx context.Context, d *schema.ResourceData, dir string) diag.Diagnostics {
	ctrl, err := dialControllerFromCerts(ctx, dir, s(d, "controller"))
	if err != nil {
		return diag.FromErr(err)
	}
	defer func() { _ = ctrl.conn.Close() }()

	req := &pb.SetClusterVersionRequest{
		Version: releaseTag(s(d, "version")),
	}
	if updates := d.Get("update").([]interface{}); len(updates) > 0 {
		if m, ok := updates[0].(map[string]interface{}); ok {
			req.Reboot, _ = m["reboot"].(bool)
			req.DrainVms, _ = m["drain_vms"].(bool)
			req.NoCanary, _ = m["no_canary"].(bool)
			if v, ok := m["canary_soak_seconds"].(int); ok {
				req.CanarySoakSeconds = toInt32(v)
			}
			if v, ok := m["canary_node"].(string); ok {
				req.CanaryNodeId = v
			}
			if v, ok := m["flake_ref"].(string); ok {
				req.FlakeRef = v
			}
			if v, ok := m["channel"].(string); ok {
				req.Channel = v
			}
		}
	}
	resp, err := ctrl.client.SetClusterVersion(ctx, req)
	if err != nil {
		return diag.FromErr(err)
	}
	_ = d.Set("update_name", resp.UpdateName)
	_ = d.Set("phase", "CLUSTER_UPDATE_PHASE_PENDING")
	return nil
}

type dialedController struct {
	conn   *grpc.ClientConn
	client pb.ControllerClient
}

func dialControllerFromCerts(ctx context.Context, dir, controllerAddr string) (*dialedController, error) {
	_ = ctx
	addr := strings.TrimSpace(controllerAddr)
	if addr == "" {
		return nil, fmt.Errorf("controller address is empty")
	}
	if !strings.Contains(addr, ":") {
		addr = addr + ":9090"
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "kctl.crt"), filepath.Join(dir, "kctl.key"))
	if err != nil {
		return nil, fmt.Errorf("load kctl certificate: %w", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt")) //nolint:gosec // dir is the operator-chosen certificate directory
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse ca.crt")
	}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   hostFromAddress(addr),
		MinVersion:   tls.VersionTLS12,
	})
	conn, err := grpc.NewClient(dialTarget(addr), grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &dialedController{conn: conn, client: pb.NewControllerClient(conn)}, nil
}

func clusterNodeRegistered(ctx context.Context, dir, controllerAddr string, n clusterNode) (bool, error) {
	ctrl, err := dialControllerFromCerts(ctx, dir, controllerAddr)
	if err != nil {
		return false, err
	}
	defer func() { _ = ctrl.conn.Close() }()
	resp, err := ctrl.client.ListNodes(ctx, &pb.ListNodesRequest{})
	if err != nil {
		return false, err
	}
	host := hostFromAddress(normalizeInstallerAddr(n.Address))
	addr := normalizeInstallerAddr(n.Address)
	for _, node := range resp.Nodes {
		if node.NodeId == n.Name || node.Hostname == n.Name || node.Address == addr || strings.HasPrefix(node.Address, host+":") {
			return true, nil
		}
	}
	return false, nil
}

func waitForClusterNodeRegistration(ctx context.Context, dir, controllerAddr string, n clusterNode) error {
	deadline := time.Now().Add(15 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var last error
	for {
		ok, err := clusterNodeRegistered(ctx, dir, controllerAddr, n)
		if err == nil && ok {
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			if last != nil {
				return fmt.Errorf("node %s did not register within 15m: %w", n.Name, last)
			}
			return fmt.Errorf("node %s did not register within 15m", n.Name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func liveISOReachable(ctx context.Context, address string) (bool, error) {
	addr := normalizeInstallerAddr(address)
	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(dialTarget(addr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	_, err = nodepb.NewNodeAdminClient(conn).ListDisks(dialCtx, &nodepb.ListDisksRequest{}, grpc.WaitForReady(true))
	if err != nil {
		return false, nil
	}
	return true, nil
}

func installClusterNode(ctx context.Context, dir string, n clusterNode, joinControllers []string, bootstrap bool) error {
	addr := normalizeInstallerAddr(n.Address)
	host := hostFromAddress(addr)
	caPEM, err := readPEM(dir, "ca.crt")
	if err != nil {
		return err
	}
	var nodeCert, nodeKey, ctrlCert, ctrlKey, subCert, subKey string
	if n.Controller {
		ctrlCert, ctrlKey, err = signHostLeaf(dir, "kcore-controller-"+host, host, true, true)
		if err != nil {
			return fmt.Errorf("controller certificate: %w", err)
		}
		nodeCert, nodeKey, err = signHostLeaf(dir, "kcore-node-"+host, host, true, true)
		if err != nil {
			return fmt.Errorf("node certificate: %w", err)
		}
		subCert, err = readPEM(dir, "sub-ca.crt")
		if err != nil {
			return err
		}
		subKey, err = readPEM(dir, "sub-ca.key")
		if err != nil {
			return err
		}
	} else {
		join := ""
		if len(joinControllers) > 0 {
			join = joinControllers[0]
		}
		ctrl, err := dialControllerFromCerts(ctx, dir, join)
		if err != nil {
			return fmt.Errorf("dial controller to issue node cert: %w", err)
		}
		defer func() { _ = ctrl.conn.Close() }()
		issued, err := ctrl.client.IssueNodeBootstrapCert(ctx, &pb.IssueNodeBootstrapCertRequest{
			NodeId:   n.Name,
			NodeHost: host,
		})
		if err != nil {
			return fmt.Errorf("issue node bootstrap cert: %w", err)
		}
		if err := fail(issued.Success, issued.Message); err != nil {
			return err
		}
		nodeCert, nodeKey = issued.CertPem, issued.KeyPem
	}

	conn, err := grpc.NewClient(dialTarget(addr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial installer %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	backend := nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_FILESYSTEM
	switch n.StorageBackend {
	case "lvm":
		backend = nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_LVM
	case "zfs":
		backend = nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_ZFS
	case "ceph":
		backend = nodepb.StorageBackendType_STORAGE_BACKEND_TYPE_CEPH
	}

	var joins []string
	var join string
	if !bootstrap {
		joins = joinControllers
		if len(joins) > 0 {
			join = joins[0]
		}
	}
	resp, err := nodepb.NewNodeAdminClient(conn).InstallToDisk(ctx, &nodepb.InstallToDiskRequest{
		OsDisk:            n.OsDisk,
		DataDisks:         n.DataDisks,
		Controller:        join,
		Controllers:       joins,
		RunController:     n.Controller,
		CaCertPem:         caPEM,
		NodeCertPem:       nodeCert,
		NodeKeyPem:        nodeKey,
		ControllerCertPem: ctrlCert,
		ControllerKeyPem:  ctrlKey,
		SubCaCertPem:      subCert,
		SubCaKeyPem:       subKey,
		DataDiskMode:      n.StorageBackend,
		StorageBackend:    backend,
		DisableVxlan:      n.DisableVxlan,
		DcId:              n.DcID,
		Hostname:          n.Hostname,
		NodeId:            n.Name,
	})
	if err != nil {
		return fmt.Errorf("install to disk: %w", err)
	}
	if !resp.Accepted {
		return fmt.Errorf("install refused: %s", resp.Message)
	}
	return nil
}
