package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceCluster() *schema.Resource {
	return &schema.Resource{
		Description:   "Create a kcore cluster trust root: CA, sub-CA, controller certificate, and kctl client certificate. Same files as `kctl create cluster`.",
		CreateContext: resourceClusterCreate,
		ReadContext:   resourceClusterRead,
		DeleteContext: resourceClusterDelete,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Cluster context name.",
			},
			"controller": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Controller address (host or host:9090). The controller certificate CN and SAN use the host.",
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
		},
	}
}

func resourceClusterCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	dir := s(d, "certs_dir")
	if clusterCertsPresent(dir) && !b(d, "force") {
		return diag.FromErr(fmt.Errorf("certificates already exist in %s (set force = true to overwrite)", dir))
	}
	if err := generateClusterPKI(dir, s(d, "controller")); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceClusterRead(ctx, d, meta)
}

func resourceClusterRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_ = ctx
	_ = meta
	dir := s(d, "certs_dir")
	if !clusterCertsPresent(dir) {
		d.SetId("")
		return nil
	}
	d.Set("ca_cert_path", filepath.Join(dir, "ca.crt"))
	d.Set("client_cert_path", filepath.Join(dir, "kctl.crt"))
	d.Set("client_key_path", filepath.Join(dir, "kctl.key"))
	d.Set("controller_cert_path", filepath.Join(dir, "controller.crt"))
	d.Set("controller_key_path", filepath.Join(dir, "controller.key"))
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
