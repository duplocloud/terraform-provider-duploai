package duplocloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

const (
	defaultPollInterval  = 10 * time.Second
	defaultCreateTimeout = 30 * time.Minute
	defaultUpdateTimeout = 30 * time.Minute
	defaultDeleteTimeout = 15 * time.Minute
)

var (
	_ resource.Resource                     = &dynamicResource{}
	_ resource.ResourceWithConfigure        = &dynamicResource{}
	_ resource.ResourceWithImportState      = &dynamicResource{}
	_ resource.ResourceWithConfigValidators = &dynamicResource{}
	_ resource.ResourceWithModifyPlan       = &dynamicResource{}
)

// dynamicResource is the single engine that turns a ResourceSpec plus its
// registered API Endpoint into a fully functional Terraform resource at
// runtime. The spec (JSON) supplies the schema; the endpoint (a duplosdk file)
// supplies the URIs. Every resource the provider serves is an instance of this
// type — there is no per-resource Go code in the engine, the boto3 model.
type dynamicResource struct {
	baseResource
	spec     ResourceSpec
	endpoint duplosdk.Endpoint
}

// newDynamicResourceFactory returns a constructor the provider can register,
// binding a spec to its API endpoint.
func newDynamicResourceFactory(spec ResourceSpec, endpoint duplosdk.Endpoint) func() resource.Resource {
	return func() resource.Resource { return &dynamicResource{spec: spec, endpoint: endpoint} }
}

func (r *dynamicResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.spec.Name
}

// ── Schema ──────────────────────────────────────────────────────────────────

func (r *dynamicResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:      true,
			Description:   "Composite resource identifier (workspace_id/id).",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
	}
	for _, a := range r.spec.Attributes {
		attrs[a.Name] = attrSchema(a)
	}
	if r.spec.Waiter != nil {
		// Optional per-instance override of waiter.failureRetries from the spec:
		// how many extra polls to tolerate a transient failure status before
		// treating it as terminal. Unset → the spec default is used.
		attrs["failure_retries"] = schema.Int64Attribute{
			Optional:    true,
			Description: "Number of extra polls to tolerate a transient failure status during provisioning before treating it as terminal. Overrides the resource's default; leave unset to use it.",
		}
	}

	out := schema.Schema{
		Description: r.spec.Description,
		Attributes:  attrs,
	}
	if r.spec.Waiter != nil {
		out.Blocks = map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: r.endpoint.HasUpdate(), Delete: true}),
		}
	}
	resp.Schema = out
}

// ── SDK binding ───────────────────────────────────────────────────────────────

// api binds an SDK client to this resource's endpoint and resolved scope.
func (r *dynamicResource) api(scope map[string]string, failureRetries int) *duplosdk.RESTResource[map[string]any] {
	return duplosdk.NewRESTResource[map[string]any](r.Client, r.endpoint, scope, r.waiter(failureRetries))
}

// scopeFromReader reads every path-parameter attribute (as declared by the
// endpoint), returning both a name→value map (for URI substitution) and the
// ordered values (for composing the resource id).
func (r *dynamicResource) scopeFromReader(ctx context.Context, reader attrReader, diags *diag.Diagnostics) (map[string]string, []string) {
	params := r.endpoint.PathParams()
	scope := make(map[string]string, len(params))
	ordered := make([]string, 0, len(params))
	for _, p := range params {
		var v types.String
		diags.Append(reader.GetAttribute(ctx, path.Root(p), &v)...)
		scope[p] = v.ValueString()
		ordered = append(ordered, v.ValueString())
	}
	return scope, ordered
}

// composeID joins the ordered scope values and the backend object id into the
// composite resource id, e.g. "tenant/cluster/obj-123".
//
// Constraint: path-parameter values and the object id must not contain "/", as
// parseID splits the id on "/" by position. DuploCloud ids are opaque tokens,
// so this holds in practice; encode the segments if a future resource can carry
// slashes.
func composeID(scopeValues []string, objID string) string {
	return strings.Join(append(append([]string{}, scopeValues...), objID), "/")
}

// specFailureRetries is the waiter.failureRetries declared in the spec (0 when
// no waiter). It is the default when the config doesn't override it.
func (r *dynamicResource) specFailureRetries() int {
	if r.spec.Waiter == nil {
		return 0
	}
	return r.spec.Waiter.FailureRetries
}

// failureRetries resolves the effective retry count for an operation: the
// optional `failure_retries` value from the resource config if set, otherwise
// the spec default. cfg is the plan (create/update) or state (delete).
func (r *dynamicResource) failureRetries(ctx context.Context, cfg attrReader, diags *diag.Diagnostics) int {
	if r.spec.Waiter == nil {
		return 0
	}
	var v types.Int64
	diags.Append(cfg.GetAttribute(ctx, path.Root("failure_retries"), &v)...)
	if diags.HasError() || v.IsNull() || v.IsUnknown() {
		return r.spec.Waiter.FailureRetries
	}
	return int(v.ValueInt64())
}

func (r *dynamicResource) waiter(failureRetries int) *duplosdk.Waiter[map[string]any] {
	w := r.spec.Waiter
	if w == nil {
		return nil
	}
	interval := defaultPollInterval
	if w.PollIntervalSeconds > 0 {
		interval = time.Duration(w.PollIntervalSeconds) * time.Second
	}
	statusSegs := strings.Split(w.StatusPath, ".")
	return &duplosdk.Waiter[map[string]any]{
		PollInterval:   interval,
		SuccessState:   w.SuccessState,
		FailureStates:  w.FailureStates,
		FailureRetries: failureRetries,
		StatusFn: func(m *map[string]any) string {
			return toStringValue(extractPath(*m, statusSegs))
		},
		FailureDetailFn: func(m *map[string]any) string {
			if w.FailureDetailPath == "" {
				return ""
			}
			return toStringValue(extractPath(*m, strings.Split(w.FailureDetailPath, ".")))
		},
	}
}

// apiBodyEqual reports whether two request bodies are identical (same API-mapped
// inputs). Map key order is normalized by json.Marshal, so the comparison is
// stable.
func apiBodyEqual(a, b map[string]any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ja) == string(jb)
}

// ModifyPlan suppresses the no-op churn that otherwise appears when a resource
// is updated without changing anything the API cares about — most commonly the
// first plan after `terraform import`, where the config carries a `timeouts`
// block but imported state has none. Terraform then plans an in-place update for
// the timeouts block alone and marks every plain `computed` output (which has no
// UseStateForUnknown) as "(known after apply)". When no API-mapped attribute has
// actually changed, we copy the prior-state value for each computed attribute
// into the plan, so the plan stays quiet (only the timeouts block differs) and
// the apply is a true no-op (see Update). This is generic — it applies to every
// resource the engine serves, current and future.
func (r *dynamicResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Only relevant on update: create has no prior state, destroy has no plan.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	planBody := r.bodyFromRaw(req.Plan.Raw, &resp.Diagnostics)
	stateBody := r.bodyFromRaw(req.State.Raw, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !apiBodyEqual(planBody, stateBody) {
		return // a real update — let computed outputs recompute normally
	}

	var planTop, stateTop map[string]tftypes.Value
	if err := req.Plan.Raw.As(&planTop); err != nil {
		return
	}
	if err := req.State.Raw.As(&stateTop); err != nil {
		return
	}
	for _, a := range r.spec.Attributes {
		if !a.Computed {
			continue
		}
		if sv, ok := stateTop[a.Name]; ok {
			planTop[a.Name] = sv // hold the prior value instead of (known after apply)
		}
	}
	resp.Plan.Raw = tftypes.NewValue(req.Plan.Raw.Type(), planTop)
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

func (r *dynamicResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	scope, scopeVals := r.scopeFromReader(ctx, req.Plan, &resp.Diagnostics)
	body := r.bodyFromRaw(req.Plan.Raw, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	log.Printf("[TRACE] dynamic %s Create(%s): start", r.spec.Name, strings.Join(scopeVals, "/"))

	retries := r.failureRetries(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	api := r.api(scope, retries)
	created, err := api.Create(&body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating "+r.spec.Name, err.Error())
		return
	}
	objID := toStringValue(extractPath(*created, strings.Split(r.spec.IDPath, ".")))

	final := created
	if r.spec.Waiter != nil {
		timeout := r.timeout(ctx, req.Plan, "create", &resp.Diagnostics)
		final, err = api.WaitUntilReady(ctx, objID, timeout)
		if err != nil {
			resp.Diagnostics.AddError("Error waiting for "+r.spec.Name, err.Error())
			return
		}
	}

	id := composeID(scopeVals, objID)
	state := r.stateFromResponse(ctx, req.Plan.Raw, *final, scope, id, false, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.Raw = state
	log.Printf("[TRACE] dynamic %s Create(%s): end", r.spec.Name, id)
}

func (r *dynamicResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	scope, objID, id, err := r.parseID(ctx, req.State, &resp.Diagnostics)
	if err != nil || resp.Diagnostics.HasError() {
		return
	}
	log.Printf("[TRACE] dynamic %s Read(%s): start", r.spec.Name, id)

	obj, clientErr := r.api(scope, r.specFailureRetries()).Get(objID)
	if clientErr != nil {
		if clientErr.IsNotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading "+r.spec.Name, clientErr.Error())
		return
	}

	state := r.stateFromResponse(ctx, req.State.Raw, *obj, scope, id, true, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.Raw = state
	log.Printf("[TRACE] dynamic %s Read(%s): end", r.spec.Name, id)
}

func (r *dynamicResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	scope, objID, id, err := r.parseID(ctx, req.State, &resp.Diagnostics)
	if err != nil || resp.Diagnostics.HasError() {
		return
	}

	// Immutable resource (no Update operation registered): every attribute is
	// forceNew, so attribute changes force replacement and never reach here. The
	// only diffs that do are config-only (e.g. the timeouts block) — refresh
	// computed values from the API and persist, without issuing an update call.
	if !r.endpoint.HasUpdate() {
		obj, clientErr := r.api(scope).Get(objID)
		if clientErr != nil {
			resp.Diagnostics.AddError("Error reading "+r.spec.Name, clientErr.Error())
			return
		}
		state := r.stateFromResponse(ctx, req.Plan.Raw, *obj, scope, id, false, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.State.Raw = state
		log.Printf("[TRACE] dynamic %s Update(%s): immutable resource, refreshed without update call", r.spec.Name, id)
		return
	}

	body := r.bodyFromRaw(req.Plan.Raw, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	log.Printf("[TRACE] dynamic %s Update(%s): start", r.spec.Name, id)

	// If nothing the API cares about changed (e.g. only the timeouts block
	// differs, as on the first apply after import), skip the API call and the
	// waiter entirely — issuing a PUT would re-trigger provisioning for a
	// metadata-only change. ModifyPlan has already carried prior computed values
	// into the plan, so it is fully known and safe to persist directly.
	stateBody := r.bodyFromRaw(req.State.Raw, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if apiBodyEqual(body, stateBody) {
		resp.State.Raw = req.Plan.Raw
		log.Printf("[TRACE] dynamic %s Update(%s): no API-relevant change, skipped", r.spec.Name, id)
		return
	}

	retries := r.failureRetries(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	api := r.api(scope, retries)
	updated, clientErr := api.Update(objID, &body)
	if clientErr != nil {
		resp.Diagnostics.AddError("Error updating "+r.spec.Name, clientErr.Error())
		return
	}

	final := updated
	if r.spec.Waiter != nil {
		timeout := r.timeout(ctx, req.Plan, "update", &resp.Diagnostics)
		final, clientErr = api.WaitUntilReady(ctx, objID, timeout)
		if clientErr != nil {
			resp.Diagnostics.AddError("Error waiting for "+r.spec.Name+" update", clientErr.Error())
			return
		}
	}

	state := r.stateFromResponse(ctx, req.Plan.Raw, *final, scope, id, false, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.Raw = state
	log.Printf("[TRACE] dynamic %s Update(%s): end", r.spec.Name, id)
}

func (r *dynamicResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	scope, objID, id, err := r.parseID(ctx, req.State, &resp.Diagnostics)
	if err != nil || resp.Diagnostics.HasError() {
		return
	}
	log.Printf("[TRACE] dynamic %s Delete(%s): start", r.spec.Name, id)
	api := r.api(scope, r.failureRetries(ctx, req.State, &resp.Diagnostics))
	if resp.Diagnostics.HasError() {
		return
	}

	var timeout time.Duration
	if r.spec.Waiter != nil {
		timeout = r.timeout(ctx, req.State, "delete", &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Resources the API refuses to delete while live declare a Deprovision
	// operation: tear down the underlying cloud resources and wait for the
	// deprovisioned state before issuing the delete call.
	if r.endpoint.HasDeprovision() {
		if clientErr := api.Deprovision(objID); clientErr != nil {
			resp.Diagnostics.AddError("Error deprovisioning "+r.spec.Name, clientErr.Error())
			return
		}
		if r.spec.Waiter != nil && r.spec.Waiter.DeprovisionedState != "" {
			if clientErr := api.WaitUntilDeprovisioned(ctx, objID, r.spec.Waiter.DeprovisionedState, timeout); clientErr != nil {
				resp.Diagnostics.AddError("Error waiting for "+r.spec.Name+" deprovisioning", clientErr.Error())
				return
			}
		}
	}

	if clientErr := api.Delete(objID); clientErr != nil {
		resp.Diagnostics.AddError("Error deleting "+r.spec.Name, clientErr.Error())
		return
	}

	// For asynchronously-provisioned resources, the delete call only starts
	// deprovisioning — poll until the object is actually gone.
	if r.spec.Waiter != nil {
		if clientErr := api.WaitUntilGone(ctx, objID, timeout); clientErr != nil {
			resp.Diagnostics.AddError("Error waiting for "+r.spec.Name+" deletion", clientErr.Error())
			return
		}
	}
	log.Printf("[TRACE] dynamic %s Delete(%s): end", r.spec.Name, id)
}

func (r *dynamicResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── Conditional-required validation ───────────────────────────────────────────

func (r *dynamicResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	if len(r.spec.RequiredIf) == 0 {
		return nil
	}
	return []resource.ConfigValidator{requiredIfValidator{spec: r.spec}}
}

type requiredIfValidator struct{ spec ResourceSpec }

func (v requiredIfValidator) Description(_ context.Context) string {
	return "Enforces conditional-required rules declared in the resource spec."
}
func (v requiredIfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v requiredIfValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	for _, rule := range v.spec.RequiredIf {
		when := v.spec.attr(rule.WhenAttribute)
		target := v.spec.attr(rule.Attribute)
		if when == nil || target == nil {
			continue
		}
		if readConfigString(ctx, req.Config, *when) != rule.WhenEquals {
			continue
		}
		if configAttrNull(ctx, req.Config, *target) {
			resp.Diagnostics.AddAttributeError(
				path.Root(rule.Attribute),
				"Missing required attribute",
				fmt.Sprintf("%q must be set when %q is %q.", rule.Attribute, rule.WhenAttribute, rule.WhenEquals),
			)
		}
	}
}

// ── Plan/state plumbing ───────────────────────────────────────────────────────

// attrReader is satisfied by tfsdk.Plan, tfsdk.State, and tfsdk.Config — all of
// which expose GetAttribute. Using the interface lets one set of helpers serve
// every CRUD phase.
type attrReader interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

// bodyFromRaw builds the request body from the plan's raw value. Working from
// the raw tftypes lets one code path serialize attributes of any type — scalars,
// collections, and arbitrarily nested objects — without per-type plan reads.
func (r *dynamicResource) bodyFromRaw(raw tftypes.Value, diags *diag.Diagnostics) map[string]any {
	var top map[string]tftypes.Value
	if err := raw.As(&top); err != nil {
		diags.AddError("Internal error", fmt.Sprintf("decoding plan: %s", err))
		return nil
	}
	body := map[string]any{}
	for _, a := range r.spec.Attributes {
		reqPath := a.requestPath()
		if reqPath == "" || a.NoSend || (!a.Required && !a.Optional) {
			continue
		}
		child, ok := top[a.Name]
		if !ok {
			continue
		}
		val, ok := attrToRequest(a, child)
		if !ok {
			continue
		}
		setPath(body, strings.Split(reqPath, "."), val)
	}
	applyConstants(body, r.spec.RequestConstants, diags)
	return body
}

// applyConstants injects the spec's fixed request fields into the body.
func applyConstants(body map[string]any, constants []ConstantField, diags *diag.Diagnostics) {
	for _, c := range constants {
		if c.Path == "" {
			continue
		}
		var v any
		if err := json.Unmarshal(c.Value, &v); err != nil {
			diags.AddError("Invalid request constant", fmt.Sprintf("constant %q: %s", c.Path, err))
			continue
		}
		setPath(body, strings.Split(c.Path, "."), v)
	}
}

func (r *dynamicResource) timeout(ctx context.Context, plan attrReader, op string, diags *diag.Diagnostics) time.Duration {
	var to timeouts.Value
	diags.Append(plan.GetAttribute(ctx, path.Root("timeouts"), &to)...)
	w := r.spec.Waiter
	switch op {
	case "create":
		d, td := to.Create(ctx, minutesOr(w.CreateTimeoutMinutes, defaultCreateTimeout))
		diags.Append(td...)
		return d
	case "update":
		d, td := to.Update(ctx, minutesOr(w.UpdateTimeoutMinutes, defaultUpdateTimeout))
		diags.Append(td...)
		return d
	default:
		d, td := to.Delete(ctx, minutesOr(w.DeleteTimeoutMinutes, defaultDeleteTimeout))
		diags.Append(td...)
		return d
	}
}

// parseID decodes the composite resource id back into its path-parameter scope
// (name→value), the backend object id, and the original id string. The SDK
// rebuilds the URI from the endpoint + scope, so Read/Update/Delete work from
// state alone — no matter how many path parameters the resource has.
func (r *dynamicResource) parseID(ctx context.Context, state attrReader, diags *diag.Diagnostics) (scope map[string]string, objID, id string, err error) {
	var idVal types.String
	diags.Append(state.GetAttribute(ctx, path.Root("id"), &idVal)...)
	id = idVal.ValueString()

	params := r.endpoint.PathParams()
	parts, splitErr := splitID(id, len(params)+1)
	if splitErr != nil {
		diags.AddError("Invalid resource ID", splitErr.Error())
		return nil, "", id, splitErr
	}
	scope = make(map[string]string, len(params))
	for i, p := range params {
		scope[p] = parts[i]
	}
	objID = parts[len(params)]
	return scope, objID, id, nil
}

// readPlanGoValue reads one attribute from the plan and converts it to a
// JSON-ready Go value. Null/unknown values return ok=false so they are omitted
// from the request body.
func readPlanGoValue(ctx context.Context, plan attrReader, a AttributeSpec, diags *diag.Diagnostics) (any, bool) {
	switch a.Type {
	case "string":
		var v types.String
		diags.Append(plan.GetAttribute(ctx, path.Root(a.Name), &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		return v.ValueString(), true
	case "bool":
		var v types.Bool
		diags.Append(plan.GetAttribute(ctx, path.Root(a.Name), &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		return v.ValueBool(), true
	case "int":
		var v types.Int64
		diags.Append(plan.GetAttribute(ctx, path.Root(a.Name), &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		return v.ValueInt64(), true
	case "list(string)":
		var v types.List
		diags.Append(plan.GetAttribute(ctx, path.Root(a.Name), &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		out := make([]string, 0, len(v.Elements()))
		for _, e := range v.Elements() {
			if s, ok := e.(types.String); ok {
				out = append(out, s.ValueString())
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// readConfigString reads an attribute from config and renders it as a string
// for comparison in requiredIf rules.
func readConfigString(ctx context.Context, cfg attrReader, a AttributeSpec) string {
	v, ok := readPlanGoValue(ctx, cfg, a, &diag.Diagnostics{})
	if !ok {
		return ""
	}
	return toStringValue(v)
}

// configAttrNull reports whether an attribute is unset in config.
func configAttrNull(ctx context.Context, cfg attrReader, a AttributeSpec) bool {
	_, ok := readPlanGoValue(ctx, cfg, a, &diag.Diagnostics{})
	return !ok
}

// stateFromResponse builds the new state object value. It starts from base
// (the plan's raw value on create/update, or the prior state's raw value on
// read) and overlays the id plus values derived from the API response.
//
// On read (refreshInputs=true) every API-mapped attribute is refreshed from the
// response so Terraform detects drift. On create/update the configured input
// values are kept from the plan to avoid "provider produced inconsistent
// result" errors; only computed-only attributes are taken from the response.
func (r *dynamicResource) stateFromResponse(_ context.Context, baseRaw tftypes.Value, resp map[string]any, scope map[string]string, id string, refreshInputs bool, diags *diag.Diagnostics) tftypes.Value {
	objType, ok := baseRaw.Type().(tftypes.Object)
	if !ok {
		diags.AddError("Internal error", "resource state is not an object")
		return tftypes.Value{}
	}
	current := map[string]tftypes.Value{}
	if err := baseRaw.As(&current); err != nil {
		diags.AddError("Internal error", fmt.Sprintf("decoding state: %s", err))
		return tftypes.Value{}
	}
	next := make(map[string]tftypes.Value, len(current))
	for k, v := range current {
		next[k] = v
	}

	next["id"] = tftypes.NewValue(tftypes.String, id)
	// Re-assert each path-parameter attribute from the parsed scope so they
	// survive import (where only the id is supplied).
	for name, val := range scope {
		if _, present := next[name]; present {
			next[name] = tftypes.NewValue(tftypes.String, val)
		}
	}

	for _, a := range r.spec.Attributes {
		respPath := a.responsePath()
		if respPath == "" {
			continue
		}
		computedOnly := a.Computed && !a.Required && !a.Optional
		// On create/update we normally keep the configured plan value to avoid
		// "provider produced inconsistent result" errors. But we must take the
		// value from the response when it is computed-only, OR when the plan
		// value is not fully known — an Optional+Computed field the user left
		// unset (no static default), or a configured object whose own computed
		// child is still unknown. Leaving any unknown would error after apply.
		planFullyKnown := next[a.Name].IsFullyKnown()
		if !computedOnly && !refreshInputs && planFullyKnown {
			continue // keep configured value from the plan
		}
		attrType, hasType := objType.AttributeTypes[a.Name]
		if !hasType {
			continue
		}
		goVal := extractPath(resp, strings.Split(respPath, "."))
		if !computedOnly && !refreshInputs {
			// Preserve the user's configured (known) leaves and fill only the
			// still-unknown ones from the response, so we never replace a
			// configured value with a server-normalized one.
			next[a.Name] = mergeUnknownFromResponse(a, attrType, next[a.Name], goVal)
		} else {
			next[a.Name] = attrFromResponse(a, attrType, goVal)
		}
	}

	return tftypes.NewValue(objType, next)
}

// ── value conversion helpers ──────────────────────────────────────────────────

func toBigFloat(v any) *big.Float {
	switch n := v.(type) {
	case float64:
		return big.NewFloat(n)
	case json.Number:
		// Parse the literal at high precision so large integers survive
		// (n.Float64() would round beyond 2^53).
		if bf, _, err := big.ParseFloat(n.String(), 10, 200, big.ToNearestEven); err == nil {
			return bf
		}
		return big.NewFloat(0)
	case int:
		return new(big.Float).SetInt64(int64(n))
	case int64:
		return new(big.Float).SetInt64(n)
	default:
		return big.NewFloat(0)
	}
}

func toStringValue(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case bool:
		return strconv.FormatBool(s)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case json.Number:
		return s.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ── path helpers ──────────────────────────────────────────────────────────────

// extractPath walks a decoded JSON value following dot-separated segments. A
// segment suffixed with "[]" treats the current value as an array and maps the
// remaining path over each element, yielding []any.
func extractPath(cur any, segs []string) any {
	if len(segs) == 0 {
		return cur
	}
	seg := segs[0]
	if strings.HasSuffix(seg, "[]") {
		key := strings.TrimSuffix(seg, "[]")
		arr, ok := mapIndex(cur, key).([]any)
		if !ok {
			return nil
		}
		out := make([]any, 0, len(arr))
		for _, el := range arr {
			if v := extractPath(el, segs[1:]); v != nil {
				out = append(out, v)
			}
		}
		return out
	}
	return extractPath(mapIndex(cur, seg), segs[1:])
}

func mapIndex(cur any, key string) any {
	if m, ok := cur.(map[string]any); ok {
		return m[key]
	}
	return nil
}

// setPath writes val into a nested map at the dot-path, creating intermediate
// maps as needed.
func setPath(root map[string]any, segs []string, val any) {
	m := root
	for _, s := range segs[:len(segs)-1] {
		next, ok := m[s].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[s] = next
		}
		m = next
	}
	m[segs[len(segs)-1]] = val
}

func minutesOr(min int, fallback time.Duration) time.Duration {
	if min > 0 {
		return time.Duration(min) * time.Minute
	}
	return fallback
}

func (s *ResourceSpec) attr(name string) *AttributeSpec {
	for i := range s.Attributes {
		if s.Attributes[i].Name == name {
			return &s.Attributes[i]
		}
	}
	return nil
}
