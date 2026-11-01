package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

type listField struct {
	key  string
	kind schema.ValueType
}

func listDataSource(description, attribute string, fields []listField, extra map[string]*schema.Schema, read schema.ReadContextFunc) *schema.Resource {
	row := map[string]*schema.Schema{}
	for _, f := range fields {
		row[f.key] = &schema.Schema{Type: f.kind, Computed: true}
	}
	s := map[string]*schema.Schema{
		attribute: {Type: schema.TypeList, Computed: true, Elem: &schema.Resource{Schema: row}},
	}
	for k, v := range extra {
		s[k] = v
	}
	return &schema.Resource{Description: description, ReadContext: read, Schema: s}
}

func setListRows(d *schema.ResourceData, id, attribute string, rows []map[string]interface{}) diag.Diagnostics {
	if err := d.Set(attribute, rows); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(id)
	return nil
}

func dataSourceVolumes() *schema.Resource {
	return listDataSource(
		"List volumes. Set `vm` to list only the volumes attached to one VM.",
		"volumes",
		[]listField{
			{"id", schema.TypeString}, {"name", schema.TypeString}, {"vm_name", schema.TypeString},
			{"node_id", schema.TypeString}, {"storage_backend", schema.TypeString},
			{"size_bytes", schema.TypeInt}, {"pool", schema.TypeString}, {"role", schema.TypeString},
			{"encrypted", schema.TypeBool},
		},
		map[string]*schema.Schema{"vm": {Type: schema.TypeString, Optional: true}},
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListVolumes(ctx, &pb.ListVolumesRequest{Vm: d.Get("vm").(string)})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetVolumes()))
			for _, v := range resp.GetVolumes() {
				rows = append(rows, map[string]interface{}{
					"id": v.GetId(), "name": v.GetName(), "vm_name": v.GetVmName(), "node_id": v.GetNodeId(),
					"storage_backend": v.GetStorageBackend(), "size_bytes": int(v.GetStorageSizeBytes()),
					"pool": v.GetPool(), "role": v.GetRole(), "encrypted": v.GetEncrypted(),
				})
			}
			return setListRows(d, "volumes", "volumes", rows)
		},
	)
}

func dataSourceSecurityGroups() *schema.Resource {
	return listDataSource(
		"List security groups.",
		"security_groups",
		[]listField{{"name", schema.TypeString}, {"description", schema.TypeString}, {"rule_count", schema.TypeInt}},
		nil,
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListSecurityGroups(ctx, &pb.ListSecurityGroupsRequest{})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetSecurityGroups()))
			for _, g := range resp.GetSecurityGroups() {
				rows = append(rows, map[string]interface{}{
					"name": g.GetName(), "description": g.GetDescription(), "rule_count": len(g.GetRules()),
				})
			}
			return setListRows(d, "security-groups", "security_groups", rows)
		},
	)
}

func dataSourceSshKeys() *schema.Resource {
	return listDataSource(
		"List SSH public keys.",
		"keys",
		[]listField{{"name", schema.TypeString}, {"public_key", schema.TypeString}},
		nil,
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListSshKeys(ctx, &pb.ListSshKeysRequest{})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetKeys()))
			for _, k := range resp.GetKeys() {
				rows = append(rows, map[string]interface{}{"name": k.GetName(), "public_key": k.GetPublicKey()})
			}
			return setListRows(d, "ssh-keys", "keys", rows)
		},
	)
}

func dataSourceOperators() *schema.Resource {
	res := listDataSource(
		"List operators and their roles.",
		"operators",
		[]listField{{"name", schema.TypeString}, {"cert_serial", schema.TypeString}},
		nil,
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListOperators(ctx, &pb.ListOperatorsRequest{})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetOperators()))
			for _, o := range resp.GetOperators() {
				roles := make([]interface{}, 0, len(o.GetRoles()))
				for _, r := range o.GetRoles() {
					roles = append(roles, listedRoleName(r))
				}
				rows = append(rows, map[string]interface{}{
					"name": o.GetName(), "cert_serial": o.GetCertSerial(), "roles": roles,
				})
			}
			return setListRows(d, "operators", "operators", rows)
		},
	)
	res.Schema["operators"].Elem.(*schema.Resource).Schema["roles"] = &schema.Schema{
		Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString},
	}
	return res
}

func listedRoleName(r pb.OperatorRoleKind) string {
	if r == pb.OperatorRoleKind_OPERATOR_ROLE_KIND_BACKUP_OPERATOR {
		return "backup-operator"
	}
	return roleName(r)
}

func dataSourceIngressGateways() *schema.Resource {
	return listDataSource(
		"List ingress gateways.",
		"gateways",
		[]listField{
			{"name", schema.TypeString}, {"generation", schema.TypeInt},
			{"phase", schema.TypeString}, {"message", schema.TypeString},
		},
		nil,
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListIngressGateways(ctx, &pb.ListIngressGatewaysRequest{})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetGateways()))
			for _, g := range resp.GetGateways() {
				rows = append(rows, map[string]interface{}{
					"name": g.GetName(), "generation": int(g.GetGeneration()),
					"phase": g.GetStatus().GetPhase(), "message": g.GetStatus().GetMessage(),
				})
			}
			return setListRows(d, "ingress-gateways", "gateways", rows)
		},
	)
}

func dataSourceIngressRoutes() *schema.Resource {
	return listDataSource(
		"List ingress routes.",
		"routes",
		[]listField{
			{"name", schema.TypeString}, {"gateway", schema.TypeString}, {"owner", schema.TypeString},
			{"generation", schema.TypeInt}, {"phase", schema.TypeString}, {"message", schema.TypeString},
		},
		nil,
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListIngressRoutes(ctx, &pb.ListIngressRoutesRequest{})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetRoutes()))
			for _, r := range resp.GetRoutes() {
				rows = append(rows, map[string]interface{}{
					"name": r.GetName(), "gateway": r.GetGateway(), "owner": r.GetOwner(),
					"generation": int(r.GetGeneration()),
					"phase":      r.GetStatus().GetPhase(), "message": r.GetStatus().GetMessage(),
				})
			}
			return setListRows(d, "ingress-routes", "routes", rows)
		},
	)
}

func dataSourceWafPolicies() *schema.Resource {
	return listDataSource(
		"List WAF policies.",
		"policies",
		[]listField{{"name", schema.TypeString}, {"generation", schema.TypeInt}},
		nil,
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListWafPolicies(ctx, &pb.ListWafPoliciesRequest{})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetPolicies()))
			for _, p := range resp.GetPolicies() {
				rows = append(rows, map[string]interface{}{"name": p.GetName(), "generation": int(p.GetGeneration())})
			}
			return setListRows(d, "waf-policies", "policies", rows)
		},
	)
}

func dataSourceClusterUpdates() *schema.Resource {
	return listDataSource(
		"List cluster updates with their phase and approval status.",
		"updates",
		[]listField{
			{"name", schema.TypeString}, {"generation", schema.TypeInt},
			{"phase", schema.TypeString}, {"approval_status", schema.TypeString},
			{"update_kind", schema.TypeString},
		},
		nil,
		func(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
			resp, err := api(meta).controller.ListClusterUpdates(ctx, &pb.ListClusterUpdatesRequest{})
			if err != nil {
				return diag.FromErr(err)
			}
			rows := make([]map[string]interface{}, 0, len(resp.GetClusterUpdates()))
			for _, u := range resp.GetClusterUpdates() {
				rows = append(rows, map[string]interface{}{
					"name": u.GetSpec().GetName(), "generation": int(u.GetGeneration()),
					"phase": u.GetPhase().String(), "approval_status": u.GetApprovalStatus().String(),
					"update_kind": u.GetSpec().GetUpdateKind(),
				})
			}
			return setListRows(d, "cluster-updates", "updates", rows)
		},
	)
}
