package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func resourceNetwork() *schema.Resource {
	return &schema.Resource{
		Description:   "Cluster network (`kctl create network`). Fields are immutable; a change replaces the network.",
		CreateContext: resourceNetworkCreate,
		ReadContext:   resourceNetworkRead,
		DeleteContext: resourceNetworkDelete,
		Schema: map[string]*schema.Schema{
			"name":             {Type: schema.TypeString, Required: true, ForceNew: true},
			"external_ip":      {Type: schema.TypeString, Optional: true, ForceNew: true},
			"gateway_ip":       {Type: schema.TypeString, Optional: true, ForceNew: true},
			"internal_netmask": {Type: schema.TypeString, Optional: true, ForceNew: true},
			"target_node":      {Type: schema.TypeString, Optional: true, ForceNew: true},
			"allowed_tcp_ports": {
				Type: schema.TypeList, Optional: true, ForceNew: true,
				Elem: &schema.Schema{Type: schema.TypeInt},
			},
			"allowed_udp_ports": {
				Type: schema.TypeList, Optional: true, ForceNew: true,
				Elem: &schema.Schema{Type: schema.TypeInt},
			},
			"vlan_id":             {Type: schema.TypeInt, Optional: true, ForceNew: true},
			"network_type":        {Type: schema.TypeString, Optional: true, ForceNew: true, Default: "nat", ValidateFunc: validation.StringInSlice([]string{"nat", "bridge", "vxlan"}, false)},
			"enable_outbound_nat": {Type: schema.TypeBool, Optional: true, ForceNew: true, Default: true},
			"ipv6_prefix":         {Type: schema.TypeString, Optional: true, ForceNew: true},
			"ipv6_gateway":        {Type: schema.TypeString, Optional: true, ForceNew: true},
			"east_west_firewall":  {Type: schema.TypeBool, Optional: true, ForceNew: true},
			"node_id":             {Type: schema.TypeString, Computed: true},
		},
	}
}

func resourceNetworkCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateNetwork(ctx, &pb.CreateNetworkRequest{
		Name:              s(d, "name"),
		ExternalIp:        s(d, "external_ip"),
		GatewayIp:         s(d, "gateway_ip"),
		InternalNetmask:   s(d, "internal_netmask"),
		TargetNode:        s(d, "target_node"),
		AllowedTcpPorts:   int32List(d, "allowed_tcp_ports"),
		AllowedUdpPorts:   int32List(d, "allowed_udp_ports"),
		VlanId:            i32(d, "vlan_id"),
		NetworkType:       s(d, "network_type"),
		EnableOutboundNat: b(d, "enable_outbound_nat"),
		Ipv6Prefix:        s(d, "ipv6_prefix"),
		Ipv6Gateway:       s(d, "ipv6_gateway"),
		EastWestFirewall:  b(d, "east_west_firewall"),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, resp.Message); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	d.Set("node_id", resp.NodeId)
	return resourceNetworkRead(ctx, d, meta)
}

func resourceNetworkRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.ListNetworks(ctx, &pb.ListNetworksRequest{TargetNode: s(d, "target_node")})
	if err != nil {
		return diag.FromErr(err)
	}
	for _, n := range resp.Networks {
		if n.Name == d.Id() {
			d.Set("external_ip", n.ExternalIp)
			d.Set("gateway_ip", n.GatewayIp)
			d.Set("internal_netmask", n.InternalNetmask)
			d.Set("node_id", n.NodeId)
			d.Set("vlan_id", n.VlanId)
			d.Set("network_type", n.NetworkType)
			d.Set("enable_outbound_nat", n.EnableOutboundNat)
			d.Set("ipv6_prefix", n.Ipv6Prefix)
			d.Set("ipv6_gateway", n.Ipv6Gateway)
			d.Set("east_west_firewall", n.EastWestFirewall)
			return nil
		}
	}
	d.SetId("")
	return nil
}

func resourceNetworkDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteNetwork(ctx, &pb.DeleteNetworkRequest{
		Name:       d.Id(),
		TargetNode: s(d, "target_node"),
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

func resourceSecurityGroup() *schema.Resource {
	return &schema.Resource{
		Description:   "Security group (`kctl security-group`).",
		CreateContext: resourceSecurityGroupCreate,
		ReadContext:   resourceSecurityGroupRead,
		UpdateContext: resourceSecurityGroupCreate,
		DeleteContext: resourceSecurityGroupDelete,
		Schema: map[string]*schema.Schema{
			"name":        {Type: schema.TypeString, Required: true, ForceNew: true},
			"description": {Type: schema.TypeString, Optional: true},
			"rule": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"protocol":    {Type: schema.TypeString, Required: true, ValidateFunc: validation.StringInSlice([]string{"tcp", "udp"}, false)},
					"host_port":   {Type: schema.TypeInt, Required: true},
					"target_port": {Type: schema.TypeInt, Optional: true},
					"source_cidr": {Type: schema.TypeString, Optional: true},
					"target_vm":   {Type: schema.TypeString, Optional: true},
					"enable_dnat": {Type: schema.TypeBool, Optional: true},
				}},
			},
		},
	}
}

func securityGroupFromState(d *schema.ResourceData) *pb.SecurityGroup {
	sg := &pb.SecurityGroup{Name: s(d, "name"), Description: s(d, "description")}
	raw, ok := d.GetOk("rule")
	if !ok {
		return sg
	}
	for _, item := range raw.([]interface{}) {
		m := item.(map[string]interface{})
		sg.Rules = append(sg.Rules, &pb.SecurityGroupRule{
			Protocol:   m["protocol"].(string),
			HostPort:   int32(m["host_port"].(int)),
			TargetPort: int32(m["target_port"].(int)),
			SourceCidr: m["source_cidr"].(string),
			TargetVm:   m["target_vm"].(string),
			EnableDnat: m["enable_dnat"].(bool),
		})
	}
	return sg
}

func resourceSecurityGroupCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.CreateSecurityGroup(ctx, &pb.CreateSecurityGroupRequest{
		SecurityGroup: securityGroupFromState(d),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(s(d, "name"))
	return resourceSecurityGroupRead(ctx, d, meta)
}

func resourceSecurityGroupRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetSecurityGroup(ctx, &pb.GetSecurityGroupRequest{Name: d.Id()})
	if err != nil || resp.GetSecurityGroup() == nil || resp.SecurityGroup.Name == "" {
		d.SetId("")
		return nil
	}
	sg := resp.SecurityGroup
	d.Set("name", sg.Name)
	d.Set("description", sg.Description)
	rules := make([]map[string]interface{}, 0, len(sg.Rules))
	for _, r := range sg.Rules {
		rules = append(rules, map[string]interface{}{
			"protocol":    r.Protocol,
			"host_port":   r.HostPort,
			"target_port": r.TargetPort,
			"source_cidr": r.SourceCidr,
			"target_vm":   r.TargetVm,
			"enable_dnat": r.EnableDnat,
		})
	}
	d.Set("rule", rules)
	return nil
}

func resourceSecurityGroupDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DeleteSecurityGroup(ctx, &pb.DeleteSecurityGroupRequest{Name: d.Id()})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(fmt.Errorf("delete security group: %w", err))
	}
	d.SetId("")
	return nil
}

func resourceSecurityGroupAttachment() *schema.Resource {
	return &schema.Resource{
		Description:   "Attach a security group to a VM or a network.",
		CreateContext: resourceSGAttachCreate,
		ReadContext:   resourceSGAttachRead,
		DeleteContext: resourceSGAttachDelete,
		Schema: map[string]*schema.Schema{
			"security_group": {Type: schema.TypeString, Required: true, ForceNew: true},
			"target_kind":    {Type: schema.TypeString, Required: true, ForceNew: true, ValidateFunc: validation.StringInSlice([]string{"vm", "network"}, false)},
			"target_id":      {Type: schema.TypeString, Required: true, ForceNew: true},
			"target_node":    {Type: schema.TypeString, Optional: true, ForceNew: true},
		},
	}
}

func sgKind(s string) pb.SecurityGroupTargetKind {
	if s == "network" {
		return pb.SecurityGroupTargetKind_SECURITY_GROUP_TARGET_KIND_NETWORK
	}
	return pb.SecurityGroupTargetKind_SECURITY_GROUP_TARGET_KIND_VM
}

func resourceSGAttachCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.AttachSecurityGroup(ctx, &pb.AttachSecurityGroupRequest{
		SecurityGroup: s(d, "security_group"),
		TargetKind:    sgKind(s(d, "target_kind")),
		TargetId:      s(d, "target_id"),
		TargetNode:    s(d, "target_node"),
	})
	if err != nil {
		return diag.FromErr(err)
	}
	if err := fail(resp.Success, ""); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(fmt.Sprintf("%s/%s/%s", s(d, "security_group"), s(d, "target_kind"), s(d, "target_id")))
	return nil
}

func resourceSGAttachRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetSecurityGroup(ctx, &pb.GetSecurityGroupRequest{Name: s(d, "security_group")})
	if err != nil || resp.GetSecurityGroup() == nil {
		d.SetId("")
		return nil
	}
	kind := sgKind(s(d, "target_kind"))
	for _, a := range resp.Attachments {
		if a.TargetKind == kind && a.TargetId == s(d, "target_id") {
			return nil
		}
	}
	d.SetId("")
	return nil
}

func resourceSGAttachDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.DetachSecurityGroup(ctx, &pb.DetachSecurityGroupRequest{
		SecurityGroup: s(d, "security_group"),
		TargetKind:    sgKind(s(d, "target_kind")),
		TargetId:      s(d, "target_id"),
		TargetNode:    s(d, "target_node"),
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
