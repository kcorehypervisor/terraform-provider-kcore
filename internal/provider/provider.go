package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

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
	conn       *grpc.ClientConn
	controller pb.ControllerClient
	vault      vaultpb.VaultClient
}

func (c *apiClient) requireVault() (vaultpb.VaultClient, error) {
	if c.vault == nil {
		return nil, fmt.Errorf("vault_address is not set")
	}
	return c.vault, nil
}

func providerConfigure(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	var diags diag.Diagnostics

	controllerAddr := d.Get("controller_address").(string)
	tlsCertPath := d.Get("tls_cert_path").(string)
	tlsKeyPath := d.Get("tls_key_path").(string)
	tlsCAPath := d.Get("tls_ca_path").(string)
	insecure := d.Get("insecure").(bool)

	var opts []grpc.DialOption

	if insecure {
		opts = append(opts, grpc.WithInsecure())
	} else if tlsCertPath != "" && tlsKeyPath != "" {
		// Load client cert and key
		cert, err := tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
		if err != nil {
			return nil, diag.FromErr(fmt.Errorf("failed to load client cert: %w", err))
		}

		// Create TLS config
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		// Load CA cert if provided
		if tlsCAPath != "" {
			caCert, err := os.ReadFile(tlsCAPath)
			if err != nil {
				return nil, diag.FromErr(fmt.Errorf("failed to read CA cert: %w", err))
			}

			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, diag.FromErr(fmt.Errorf("failed to parse CA cert"))
			}

			tlsConfig.RootCAs = caCertPool
		}

		creds := credentials.NewTLS(tlsConfig)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	// Connect to the controller
	conn, err := grpc.Dial(controllerAddr, opts...)
	if err != nil {
		return nil, diag.FromErr(fmt.Errorf("failed to connect to controller: %w", err))
	}

	client := &apiClient{
		conn:       conn,
		controller: pb.NewControllerClient(conn),
	}
	if vaultAddr := d.Get("vault_address").(string); vaultAddr != "" {
		vaultConn, err := grpc.Dial(vaultAddr, opts...)
		if err != nil {
			return nil, diag.FromErr(fmt.Errorf("failed to connect to vault: %w", err))
		}
		client.vault = vaultpb.NewVaultClient(vaultConn)
	}

	return client, diags
}
