package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceSSHKey() *schema.Resource {
	return &schema.Resource{
		Description:   "SSH public key registered with the controller (`kctl ssh-key add`).",
		CreateContext: resourceSSHKeyCreate,
		ReadContext:   resourceSSHKeyRead,
		DeleteContext: resourceSSHKeyDelete,
		Schema: map[string]*schema.Schema{
			"name":       {Type: schema.TypeString, Required: true, ForceNew: true},
			"public_key": {Type: schema.TypeString, Required: true, ForceNew: true},
		},
	}
}

func resourceSSHKeyCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateSshKey(ctx, &pb.CreateSshKeyRequest{
		Name:      s(d, "name"),
		PublicKey: s(d, "public_key"),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceSSHKeyRead(ctx, d, meta)
}

func resourceSSHKeyRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetSshKey(ctx, &pb.GetSshKeyRequest{Name: d.Id()})
	if err != nil || resp.GetKey() == nil || resp.Key.Name == "" {
		d.SetId("")
		return nil
	}
	d.Set("name", resp.Key.Name)
	d.Set("public_key", resp.Key.PublicKey)
	return nil
}

func resourceSSHKeyDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteSshKey(ctx, &pb.DeleteSshKeyRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceOperator() *schema.Resource {
	return &schema.Resource{
		Description:   "RBAC operator and optional client certificate (`kctl operator add`).",
		CreateContext: resourceOperatorCreate,
		ReadContext:   resourceOperatorRead,
		UpdateContext: resourceOperatorUpdate,
		DeleteContext: resourceOperatorDelete,
		Schema: map[string]*schema.Schema{
			"name": {Type: schema.TypeString, Required: true, ForceNew: true},
			"roles": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					ValidateFunc: validation.StringInSlice([]string{"read-only", "vm-admin", "cluster-admin"}, false),
				},
			},
			"issue_cert": {Type: schema.TypeBool, Optional: true, ForceNew: true},
			"cert_pem":   {Type: schema.TypeString, Computed: true},
			"key_pem":    {Type: schema.TypeString, Computed: true, Sensitive: true},
		},
	}
}

func operatorRole(name string) (pb.OperatorRoleKind, error) {
	switch name {
	case "read-only":
		return pb.OperatorRoleKind_OPERATOR_ROLE_KIND_READ_ONLY, nil
	case "vm-admin":
		return pb.OperatorRoleKind_OPERATOR_ROLE_KIND_VM_ADMIN, nil
	case "cluster-admin":
		return pb.OperatorRoleKind_OPERATOR_ROLE_KIND_CLUSTER_ADMIN, nil
	default:
		return 0, fmt.Errorf("unknown operator role %q", name)
	}
}

func roleName(k pb.OperatorRoleKind) string {
	switch k {
	case pb.OperatorRoleKind_OPERATOR_ROLE_KIND_READ_ONLY:
		return "read-only"
	case pb.OperatorRoleKind_OPERATOR_ROLE_KIND_VM_ADMIN:
		return "vm-admin"
	case pb.OperatorRoleKind_OPERATOR_ROLE_KIND_CLUSTER_ADMIN:
		return "cluster-admin"
	default:
		return ""
	}
}

func grantRoles(ctx context.Context, c pb.ControllerClient, name string, roles []string) error {
	for _, role := range roles {
		kind, err := operatorRole(role)
		if err != nil {
			return err
		}
		if _, err := c.GrantOperatorRole(ctx, &pb.GrantOperatorRoleRequest{OperatorName: name, Role: kind}); err != nil {
			return err
		}
	}
	return nil
}

func resourceOperatorCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	c := api(meta).controller
	name := s(d, "name")
	if _, err := c.CreateOperator(ctx, &pb.CreateOperatorRequest{Name: name}); err != nil {
		return diag.FromErr(err)
	}
	if err := grantRoles(ctx, c, name, setList(d, "roles")); err != nil {
		return diag.FromErr(err)
	}
	if b(d, "issue_cert") {
		issued, err := c.IssueOperatorCert(ctx, &pb.IssueOperatorCertRequest{OperatorName: name})
		if err != nil {
			return diag.FromErr(err)
		}
		if err := fail(issued.Success, issued.Message); err != nil {
			return diag.FromErr(err)
		}
		d.Set("cert_pem", issued.CertPem)
		d.Set("key_pem", issued.KeyPem)
	}
	d.SetId(name)
	return resourceOperatorRead(ctx, d, meta)
}

func resourceOperatorRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetOperator(ctx, &pb.GetOperatorRequest{Name: d.Id()})
	if err != nil || resp.GetOperator() == nil || resp.Operator.Name == "" {
		d.SetId("")
		return nil
	}
	var roles []string
	for _, r := range resp.Operator.Roles {
		if name := roleName(r); name != "" {
			roles = append(roles, name)
		}
	}
	d.Set("roles", roles)
	return nil
}

func resourceOperatorUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	if d.HasChange("roles") {
		c := api(meta).controller
		old, newV := d.GetChange("roles")
		prev := old.(*schema.Set)
		next := newV.(*schema.Set)
		for _, r := range prev.Difference(next).List() {
			kind, err := operatorRole(r.(string))
			if err != nil {
				return diag.FromErr(err)
			}
			if _, err := c.RevokeOperatorRole(ctx, &pb.RevokeOperatorRoleRequest{OperatorName: d.Id(), Role: kind}); err != nil {
				return diag.FromErr(err)
			}
		}
		var add []string
		for _, r := range next.Difference(prev).List() {
			add = append(add, r.(string))
		}
		if err := grantRoles(ctx, c, d.Id(), add); err != nil {
			return diag.FromErr(err)
		}
	}
	return resourceOperatorRead(ctx, d, meta)
}

func resourceOperatorDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteOperator(ctx, &pb.DeleteOperatorRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
