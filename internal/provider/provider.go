package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpcinsecure "google.golang.org/grpc/credentials/insecure"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
	vaultpb "github.com/kcorehypervisor/terraform-provider-kcore/api/vault"
)

// New returns a new provider instance
func New() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"controller_address": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("KCORE_CONTROLLER_ADDRESS", "localhost:9090"),
				Description: "Address of the kcore controller gRPC endpoint",
			},
			"tls_cert_path": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("KCORE_TLS_CERT", ""),
				Description: "Path to the TLS certificate file",
			},
			"tls_key_path": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("KCORE_TLS_KEY", ""),
				Description: "Path to the TLS key file",
			},
			"tls_ca_path": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("KCORE_TLS_CA", ""),
				Description: "Path to the TLS CA certificate file",
			},
			"insecure": {
				Type:        schema.TypeBool,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("KCORE_INSECURE", false),
				Description: "Disable TLS verification (not recommended for production)",
			},
			"vault_address": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("KCORE_VAULT_ENDPOINT", ""),
				Description: "Address of the kcore Vault coordinator. Required by kcore_vault_policy, kcore_vault_target, and kcore_vault_replication.",
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"kcore_cluster":                   resourceCluster(),
			"kcore_node":                      resourceNode(),
			"kcore_vm":                        resourceVM(),
			"kcore_network":                   resourceNetwork(),
			"kcore_security_group":            resourceSecurityGroup(),
			"kcore_security_group_attachment": resourceSecurityGroupAttachment(),
			"kcore_volume":                    resourceVolume(),
			"kcore_volume_snapshot":           resourceVolumeSnapshot(),
			"kcore_snapshot_policy":           resourceSnapshotPolicy(),
			"kcore_disk_layout":               resourceDiskLayout(),
			"kcore_ceph_cluster":              resourceCephCluster(),
			"kcore_shared_filesystem":         resourceSharedFilesystem(),
			"kcore_object_store":              resourceObjectStore(),
			"kcore_object_user":               resourceObjectUser(),
			"kcore_ssh_key":                   resourceSSHKey(),
			"kcore_operator":                  resourceOperator(),
			"kcore_cluster_update":            resourceClusterUpdate(),
			"kcore_cluster_version":           resourceClusterVersion(),
			"kcore_workload":                  resourceWorkload(),
			"kcore_kubernetes_cluster":        resourceKubernetesCluster(),
			"kcore_vault_policy":              resourceVaultPolicy(),
			"kcore_vault_target":              resourceVaultTarget(),
			"kcore_vault_replication":         resourceVaultReplication(),
			"kcore_vgpu_host":                 resourceVgpuHost(),
			"kcore_slurm_cluster":             resourceSlurmCluster(),
			"kcore_ingress_gateway":           resourceIngressGateway(),
			"kcore_ingress_route":             resourceIngressRoute(),
			"kcore_waf_policy":                resourceWafPolicy(),
			"kcore_postgresql":                resourcePostgresql(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"kcore_vm":                  dataSourceVM(),
			"kcore_gpus":                dataSourceGpus(),
			"kcore_node":                dataSourceNode(),
			"kcore_nodes":               dataSourceNodes(),
			"kcore_kubernetes_cluster":  dataSourceKubernetesCluster(),
			"kcore_kubernetes_clusters": dataSourceKubernetesClusters(),
			"kcore_slurm_cluster":       dataSourceSlurmCluster(),
			"kcore_postgresql":          dataSourcePostgresql(),
			"kcore_volumes":             dataSourceVolumes(),
			"kcore_security_groups":     dataSourceSecurityGroups(),
			"kcore_ssh_keys":            dataSourceSshKeys(),
			"kcore_operators":           dataSourceOperators(),
			"kcore_ingress_gateways":    dataSourceIngressGateways(),
			"kcore_ingress_routes":      dataSourceIngressRoutes(),
			"kcore_waf_policies":        dataSourceWafPolicies(),
			"kcore_cluster_updates":     dataSourceClusterUpdates(),
		},
		ConfigureContextFunc: providerConfigure,
	}
}

type apiClient struct {
	mu         sync.Mutex
	conn       *grpc.ClientConn
	controller pb.ControllerClient
	vault      vaultpb.VaultClient

	controllerAddr string
	tlsCertPath    string
	tlsKeyPath     string
	tlsCAPath      string
	insecure       bool
	vaultAddr      string
}

func (c *apiClient) requireVault() (vaultpb.VaultClient, error) {
	if err := c.ensure(); err != nil {
		return nil, err
	}
	if c.vault == nil {
		return nil, fmt.Errorf("vault_address is not set")
	}
	return c.vault, nil
}

// dialTarget keeps the resolver that grpc.Dial used. grpc.NewClient defaults to
// DNS, which re-resolves and can pick another address, so a plain host:port
// is passed through untouched. An address that names its own scheme is kept.
func dialTarget(addr string) string {
	if strings.Contains(addr, "://") || strings.HasPrefix(addr, "unix:") {
		return addr
	}
	return "passthrough:///" + addr
}

func providerConfigure(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	_ = ctx
	client := &apiClient{
		controllerAddr: d.Get("controller_address").(string),
		tlsCertPath:    d.Get("tls_cert_path").(string),
		tlsKeyPath:     d.Get("tls_key_path").(string),
		tlsCAPath:      d.Get("tls_ca_path").(string),
		insecure:       d.Get("insecure").(bool),
		vaultAddr:      d.Get("vault_address").(string),
	}
	// Client certificates load at handshake time (GetClientCertificate), so a
	// first apply can create them with kcore_cluster before any other resource
	// dials. grpc.NewClient itself does not open the connection yet.
	if err := client.ensure(); err != nil {
		return nil, diag.FromErr(err)
	}
	return client, nil
}

func (c *apiClient) ensure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.controller != nil {
		return nil
	}
	opts, err := c.dialOptions()
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(dialTarget(c.controllerAddr), opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	c.conn = conn
	c.controller = pb.NewControllerClient(conn)
	if c.vaultAddr != "" {
		vaultConn, err := grpc.NewClient(dialTarget(c.vaultAddr), opts...)
		if err != nil {
			return fmt.Errorf("failed to connect to vault: %w", err)
		}
		c.vault = vaultpb.NewVaultClient(vaultConn)
	}
	return nil
}

func (c *apiClient) dialOptions() ([]grpc.DialOption, error) {
	if c.insecure || (c.tlsCertPath == "" && c.tlsKeyPath == "") {
		return []grpc.DialOption{grpc.WithTransportCredentials(grpcinsecure.NewCredentials())}, nil
	}
	if c.tlsCertPath == "" || c.tlsKeyPath == "" {
		return nil, fmt.Errorf("tls_cert_path and tls_key_path must both be set")
	}
	// Load client certs at handshake time so a first apply can create them via
	// kcore_cluster before any other resource dials the controller.
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(c.tlsCertPath, c.tlsKeyPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load client cert: %w", err)
			}
			return &cert, nil
		},
	}
	if c.tlsCAPath != "" {
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("server presented no certificates")
			}
			caPEM, err := os.ReadFile(c.tlsCAPath) //nolint:gosec // operator provider configuration
			if err != nil {
				return fmt.Errorf("failed to read CA cert: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caPEM) {
				return fmt.Errorf("failed to parse CA cert")
			}
			certs := make([]*x509.Certificate, 0, len(rawCerts))
			for _, raw := range rawCerts {
				cert, err := x509.ParseCertificate(raw)
				if err != nil {
					return fmt.Errorf("parse server cert: %w", err)
				}
				certs = append(certs, cert)
			}
			intermediates := x509.NewCertPool()
			for _, cert := range certs[1:] {
				intermediates.AddCert(cert)
			}
			_, err = certs[0].Verify(x509.VerifyOptions{Roots: pool, Intermediates: intermediates})
			return err
		}
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))}, nil
}
