package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

const (
	ingressKindGateway = "IngressGateway"
	ingressKindRoute   = "IngressRoute"
	ingressKindPolicy  = "WafPolicy"
)

// The ingress specs are camelCase documents the controller parses and
// canonicalises, filling defaults. The provider therefore sends the spec as
// the operator wrote it and does not read it back, so a default the controller
// adds never shows up as drift.
func ingressSpecSchema(description string) *schema.Schema {
	return &schema.Schema{
		Type:             schema.TypeString,
		Required:         true,
		ValidateFunc:     validation.StringIsJSON,
		DiffSuppressFunc: suppressEquivalentJSON,
		StateFunc:        normalizeJSON,
		Description:      description,
	}
}

func normalizeJSON(raw interface{}) string {
	text, _ := raw.(string)
	var value interface{}
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return text
	}
	out, err := json.Marshal(value)
	if err != nil {
		return text
	}
	return string(out)
}

func suppressEquivalentJSON(_, oldValue, newValue string, _ *schema.ResourceData) bool {
	var a, b interface{}
	if json.Unmarshal([]byte(oldValue), &a) != nil || json.Unmarshal([]byte(newValue), &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func ingressSpecObject(raw string) (map[string]interface{}, error) {
	var spec map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return nil, fmt.Errorf("spec is not valid JSON: %w", err)
	}
	if spec == nil {
		return nil, fmt.Errorf("spec must be a JSON object")
	}
	return spec, nil
}

// routeSpecJSON puts the route's gateway into the spec. The controller reads
// the gateway from the spec, and a separate attribute gives Terraform the
// dependency edge from the route to its gateway.
func routeSpecJSON(gateway, raw string) (string, error) {
	spec, err := ingressSpecObject(raw)
	if err != nil {
		return "", err
	}
	if existing, ok := spec["gateway"].(string); ok && existing != "" && existing != gateway {
		return "", fmt.Errorf("spec.gateway is %q but gateway is %q; set the gateway attribute and leave it out of spec", existing, gateway)
	}
	spec["gateway"] = gateway
	out, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func planIngressDocument(ctx context.Context, client pb.ControllerClient, gateway, kind, name, specJSON string) diag.Diagnostics {
	plan, err := client.PlanIngress(ctx, &pb.PlanIngressRequest{
		Gateway:   gateway,
		Documents: []*pb.IngressDocument{{Kind: kind, Name: name, SpecJson: specJSON}},
	})
	if err != nil {
		return diag.FromErr(fmt.Errorf("planning %s %s: %w", kind, name, err))
	}
	var diags diag.Diagnostics
	for _, warning := range plan.GetWarnings() {
		diags = append(diags, diag.Diagnostic{Severity: diag.Warning, Summary: "Ingress plan warning", Detail: warning})
	}
	if errs := plan.GetErrors(); len(errs) > 0 {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("%s %s failed validation", kind, name),
			Detail:   strings.Join(errs, "; "),
		})
	}
	return diags
}

func resourceIngressGateway() *schema.Resource {
	return &schema.Resource{
		Description: "Ingress gateway (`kind: IngressGateway`): Caddy with the Coraza WAF on one or two nodes, with ACME certificates and an optional keepalived VIP. " +
			"Apply pushes the node configuration, so expect a NixOS rebuild. Destroy fails while routes still use the gateway; Terraform orders that for you when the routes reference `kcore_ingress_gateway.<name>.name`.",
		CreateContext: resourceIngressGatewayUpsert,
		ReadContext:   resourceIngressGatewayRead,
		UpdateContext: resourceIngressGatewayUpsert,
		DeleteContext: resourceIngressGatewayDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
			Update: schema.DefaultTimeout(30 * time.Minute),
		},
		Schema: map[string]*schema.Schema{
			"name": {Type: schema.TypeString, Required: true, ForceNew: true, Description: "Gateway name (`metadata.name`)."},
			"spec": ingressSpecSchema("The `spec` of an IngressGateway as JSON, for example `jsonencode({ nodes = [\"node-105\"], listeners = [...], tls = { acme = {...} } })`. Field names are the camelCase names used in `docs/user/ingress.md`."),
			"wait_for_ready": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Wait until every gateway node has applied the configuration before apply returns.",
			},
			"generation":     {Type: schema.TypeInt, Computed: true},
			"phase":          {Type: schema.TypeString, Computed: true, Description: "pending, applying, ready, degraded, or failed."},
			"status_message": {Type: schema.TypeString, Computed: true},
			"node_status": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"node_id":      {Type: schema.TypeString, Computed: true},
					"healthy":      {Type: schema.TypeBool, Computed: true},
					"applied_hash": {Type: schema.TypeString, Computed: true},
					"vip_holder":   {Type: schema.TypeString, Computed: true},
					"message":      {Type: schema.TypeString, Computed: true},
				}},
			},
		},
	}
}

func resourceIngressGatewayUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := api(meta).controller
	name := strings.TrimSpace(s(d, "name"))
	specJSON := s(d, "spec")
	if _, err := ingressSpecObject(specJSON); err != nil {
		return diag.FromErr(err)
	}
	diags := planIngressDocument(ctx, client, name, ingressKindGateway, name, specJSON)
	if diags.HasError() {
		return diags
	}
	resp, err := client.CreateIngressGateway(ctx, &pb.CreateIngressGatewayRequest{Name: name, SpecJson: specJSON})
	if err != nil {
		return append(diags, diag.FromErr(fmt.Errorf("applying ingress gateway %s: %w", name, err))...)
	}
	if !resp.GetSuccess() {
		return append(diags, diag.Errorf("applying ingress gateway %s: the controller did not accept it", name)...)
	}
	d.SetId(name)
	if b(d, "wait_for_ready") && resp.GetAction() != pb.ApplyAction_APPLY_ACTION_UNCHANGED {
		timeout := d.Timeout(schema.TimeoutUpdate)
		if d.IsNewResource() {
			timeout = d.Timeout(schema.TimeoutCreate)
		}
		if err := waitForIngressGateway(ctx, client, name, resp.GetGateway().GetGeneration(), timeout); err != nil {
			return append(diags, diag.FromErr(err)...)
		}
	}
	return append(diags, resourceIngressGatewayRead(ctx, d, meta)...)
}

func waitForIngressGateway(ctx context.Context, client pb.ControllerClient, name string, generation int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for {
		resp, err := client.GetIngressGateway(ctx, &pb.GetIngressGatewayRequest{Name: name})
		if err != nil {
			last = err.Error()
		} else {
			status := resp.GetGateway().GetStatus()
			last = fmt.Sprintf("phase %s, observed generation %d of %d: %s", status.GetPhase(), status.GetObservedGeneration(), generation, status.GetMessage())
			if status.GetPhase() == "failed" {
				return fmt.Errorf("ingress gateway %s failed: %s", name, status.GetMessage())
			}
			if status.GetPhase() == "ready" && status.GetObservedGeneration() >= generation {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ingress gateway %s to be ready (%s)", name, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

func resourceIngressGatewayRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetIngressGateway(ctx, &pb.GetIngressGatewayRequest{Name: d.Id()})
	if err != nil {
		if grpcNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}
	gateway := resp.GetGateway()
	if gateway == nil || gateway.GetName() == "" {
		d.SetId("")
		return nil
	}
	d.Set("name", gateway.GetName())
	d.Set("generation", gateway.GetGeneration())
	d.Set("phase", gateway.GetStatus().GetPhase())
	d.Set("status_message", gateway.GetStatus().GetMessage())
	nodes := make([]map[string]interface{}, 0, len(gateway.GetStatus().GetNodes()))
	for _, node := range gateway.GetStatus().GetNodes() {
		nodes = append(nodes, map[string]interface{}{
			"node_id":      node.GetNodeId(),
			"healthy":      node.GetHealthy(),
			"applied_hash": node.GetAppliedHash(),
			"vip_holder":   node.GetVipHolder(),
			"message":      node.GetMessage(),
		})
	}
	return diag.FromErr(d.Set("node_status", nodes))
}

func resourceIngressGatewayDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_, err := api(meta).controller.DeleteIngressGateway(ctx, &pb.DeleteIngressGatewayRequest{Name: d.Id()})
	if err != nil && !grpcNotFound(err) {
		return diag.FromErr(fmt.Errorf("deleting ingress gateway %s: %w", d.Id(), err))
	}
	d.SetId("")
	return nil
}

func resourceIngressRoute() *schema.Resource {
	return &schema.Resource{
		Description: "Ingress route (`kind: IngressRoute`): hostnames and paths mapped to VM, container, or external backends on a gateway, with load balancing, health checks and per-route WAF settings. " +
			"Switching a route to WAF block mode after reading the events stays a `kctl ingress waf set-mode` operation, or edit `spec`.",
		CreateContext: resourceIngressRouteUpsert,
		ReadContext:   resourceIngressRouteRead,
		UpdateContext: resourceIngressRouteUpsert,
		DeleteContext: resourceIngressRouteDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(15 * time.Minute),
			Update: schema.DefaultTimeout(15 * time.Minute),
		},
		Schema: map[string]*schema.Schema{
			"name":    {Type: schema.TypeString, Required: true, ForceNew: true, Description: "Route name (`metadata.name`)."},
			"gateway": {Type: schema.TypeString, Required: true, Description: "Name of the gateway that serves the route. Reference `kcore_ingress_gateway.<name>.name` so Terraform creates the gateway first and deletes it last."},
			"spec":    ingressSpecSchema("The `spec` of an IngressRoute as JSON, without `gateway`: `hosts`, `paths`, `waf`, `headers`, `redirectHttp`, and for TCP routes `kind` and `port`."),
			"wait_for_ready": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Wait until the route reports ready before apply returns. Certificates are issued by the gateway, so a route with a new hostname can take a few minutes.",
			},
			"generation":     {Type: schema.TypeInt, Computed: true},
			"phase":          {Type: schema.TypeString, Computed: true, Description: "pending, ready, or degraded."},
			"status_message": {Type: schema.TypeString, Computed: true},
			"upstreams": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"address": {Type: schema.TypeString, Computed: true},
					"healthy": {Type: schema.TypeBool, Computed: true},
				}},
			},
			"certificates": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"host":      {Type: schema.TypeString, Computed: true},
					"issuer":    {Type: schema.TypeString, Computed: true},
					"not_after": {Type: schema.TypeString, Computed: true},
				}},
			},
		},
	}
}

func resourceIngressRouteUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := api(meta).controller
	name := strings.TrimSpace(s(d, "name"))
	gateway := strings.TrimSpace(s(d, "gateway"))
	specJSON, err := routeSpecJSON(gateway, s(d, "spec"))
	if err != nil {
		return diag.FromErr(err)
	}
	diags := planIngressDocument(ctx, client, gateway, ingressKindRoute, name, specJSON)
	if diags.HasError() {
		return diags
	}
	resp, err := client.CreateIngressRoute(ctx, &pb.CreateIngressRouteRequest{Name: name, SpecJson: specJSON})
	if err != nil {
		return append(diags, diag.FromErr(fmt.Errorf("applying ingress route %s: %w", name, err))...)
	}
	if !resp.GetSuccess() {
		return append(diags, diag.Errorf("applying ingress route %s: the controller did not accept it", name)...)
	}
	d.SetId(name)
	if b(d, "wait_for_ready") {
		timeout := d.Timeout(schema.TimeoutUpdate)
		if d.IsNewResource() {
			timeout = d.Timeout(schema.TimeoutCreate)
		}
		if err := waitForIngressRoute(ctx, client, name, timeout); err != nil {
			return append(diags, diag.FromErr(err)...)
		}
	}
	return append(diags, resourceIngressRouteRead(ctx, d, meta)...)
}

func waitForIngressRoute(ctx context.Context, client pb.ControllerClient, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for {
		resp, err := client.GetIngressRoute(ctx, &pb.GetIngressRouteRequest{Name: name})
		if err != nil {
			last = err.Error()
		} else {
			status := resp.GetRoute().GetStatus()
			last = fmt.Sprintf("phase %s: %s", status.GetPhase(), status.GetMessage())
			if status.GetPhase() == "ready" {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ingress route %s to be ready (%s)", name, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

func resourceIngressRouteRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetIngressRoute(ctx, &pb.GetIngressRouteRequest{Name: d.Id()})
	if err != nil {
		if grpcNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}
	route := resp.GetRoute()
	if route == nil || route.GetName() == "" {
		d.SetId("")
		return nil
	}
	d.Set("name", route.GetName())
	if route.GetGateway() != "" {
		d.Set("gateway", route.GetGateway())
	}
	d.Set("generation", route.GetGeneration())
	d.Set("phase", route.GetStatus().GetPhase())
	d.Set("status_message", route.GetStatus().GetMessage())
	upstreams := make([]map[string]interface{}, 0, len(route.GetStatus().GetUpstreams()))
	for _, upstream := range route.GetStatus().GetUpstreams() {
		upstreams = append(upstreams, map[string]interface{}{
			"address": upstream.GetAddress(),
			"healthy": upstream.GetHealthy(),
		})
	}
	certs := make([]map[string]interface{}, 0, len(route.GetStatus().GetCerts()))
	for _, cert := range route.GetStatus().GetCerts() {
		certs = append(certs, map[string]interface{}{
			"host":      cert.GetHost(),
			"issuer":    cert.GetIssuer(),
			"not_after": cert.GetNotAfter(),
		})
	}
	if err := d.Set("upstreams", upstreams); err != nil {
		return diag.FromErr(err)
	}
	return diag.FromErr(d.Set("certificates", certs))
}

func resourceIngressRouteDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_, err := api(meta).controller.DeleteIngressRoute(ctx, &pb.DeleteIngressRouteRequest{Name: d.Id()})
	if err != nil && !grpcNotFound(err) {
		return diag.FromErr(fmt.Errorf("deleting ingress route %s: %w", d.Id(), err))
	}
	d.SetId("")
	return nil
}

func resourceWafPolicy() *schema.Resource {
	return &schema.Resource{
		Description: "WAF policy (`kind: WafPolicy`): OWASP Core Rule Set through Coraza. `engine` starts as `detect`, which only logs. " +
			"Destroy fails while a gateway or route still uses the policy, so reference `kcore_waf_policy.<name>.name` from their specs.",
		CreateContext: resourceWafPolicyUpsert,
		ReadContext:   resourceWafPolicyRead,
		UpdateContext: resourceWafPolicyUpsert,
		DeleteContext: resourceWafPolicyDelete,
		Schema: map[string]*schema.Schema{
			"name":       {Type: schema.TypeString, Required: true, ForceNew: true, Description: "Policy name (`metadata.name`)."},
			"spec":       ingressSpecSchema("The `spec` of a WafPolicy as JSON: `engine`, `crs`, `requestBodyLimitBytes`, and so on. `{}` is a valid policy with every default."),
			"generation": {Type: schema.TypeInt, Computed: true},
		},
	}
}

func resourceWafPolicyUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	name := strings.TrimSpace(s(d, "name"))
	specJSON := s(d, "spec")
	if _, err := ingressSpecObject(specJSON); err != nil {
		return diag.FromErr(err)
	}
	resp, err := api(meta).controller.CreateWafPolicy(ctx, &pb.CreateWafPolicyRequest{Name: name, SpecJson: specJSON})
	if err != nil {
		return diag.FromErr(fmt.Errorf("applying WAF policy %s: %w", name, err))
	}
	if !resp.GetSuccess() {
		return diag.Errorf("applying WAF policy %s: the controller did not accept it", name)
	}
	d.SetId(name)
	return resourceWafPolicyRead(ctx, d, meta)
}

func resourceWafPolicyRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	resp, err := api(meta).controller.GetWafPolicy(ctx, &pb.GetWafPolicyRequest{Name: d.Id()})
	if err != nil {
		if grpcNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}
	policy := resp.GetPolicy()
	if policy == nil || policy.GetName() == "" {
		d.SetId("")
		return nil
	}
	d.Set("name", policy.GetName())
	d.Set("generation", policy.GetGeneration())
	return nil
}

func resourceWafPolicyDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	_, err := api(meta).controller.DeleteWafPolicy(ctx, &pb.DeleteWafPolicyRequest{Name: d.Id()})
	if err != nil && !grpcNotFound(err) {
		return diag.FromErr(fmt.Errorf("deleting WAF policy %s: %w", d.Id(), err))
	}
	d.SetId("")
	return nil
}
