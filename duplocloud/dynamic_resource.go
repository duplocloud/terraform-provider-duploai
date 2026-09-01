package duplocloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"slices"
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
	idDescription := "Composite resource identifier (workspace_id/id)."
	if r.spec.Association != nil {
		// A link has no object id — it is identified by its path parameters alone.
		idDescription = "Composite resource identifier (" +
			strings.Join(r.endpoint.PathParams(), "/") + ")."
	}
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:      true,
			Description:   idDescription,
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
		// Optional user-gated "wait until a response path is populated" control
		// (e.g. wait_for_load_balancer). When true, create/update keep polling
		// after the normal ready gate until waiter.populatedPath is non-empty.
		if r.spec.Waiter.PopulatedPath != "" && r.spec.Waiter.PopulatedPathAttribute != "" {
			attrs[r.spec.Waiter.PopulatedPathAttribute] = schema.BoolAttribute{
				Optional:    true,
				Description: "When true, wait during create and update until the backend reports a value at " + r.spec.Waiter.PopulatedPath + " (e.g. a provisioned load balancer address), or the operation times out. Defaults to false.",
			}
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

// apiWaitingForPopulated is like api but, when waitForPopulated is true, extends
// the waiter with a secondary gate that holds success until waiter.populatedPath
// is non-empty — e.g. keep polling after an Ingress is Complete until the cloud
// controller assigns a load balancer address. Composes with any existing
// readyPath/readyState gate (both must hold). No-op when the resource declares no
// populatedPath, so callers can use it unconditionally.
func (r *dynamicResource) apiWaitingForPopulated(scope map[string]string, failureRetries int, waitForPopulated bool) *duplosdk.RESTResource[map[string]any] {
	w := r.waiter(failureRetries)
	if w != nil && waitForPopulated && r.spec.Waiter.PopulatedPath != "" {
		applyPopulatedGate(w, r.spec.Waiter.PopulatedPath)
	}
	return duplosdk.NewRESTResource[map[string]any](r.Client, r.endpoint, scope, w)
}

// applyPopulatedGate extends waiter w with a secondary success gate that holds
// until the value at populatedPath is non-empty, composing with any existing
// readyPath/readyState gate (both must hold).
func applyPopulatedGate(w *duplosdk.Waiter[map[string]any], populatedPath string) {
	segs := strings.Split(populatedPath, ".")
	const populated = "\x00populated"
	prevFn, prevState := w.ReadyFn, w.ReadyState
	w.ReadyState = populated
	w.ReadyFn = func(m *map[string]any) string {
		if prevFn != nil && prevFn(m) != prevState {
			return "" // an existing ready gate is not yet met
		}
		if isNonEmptyValue(extractPath(*m, segs)) {
			return populated
		}
		return ""
	}
}

// waitForPopulated reads the optional boolean control attribute
// (waiter.populatedPathAttribute, e.g. wait_for_load_balancer) from the plan.
// Returns false when the resource declares no such control or it is unset.
func (r *dynamicResource) waitForPopulated(ctx context.Context, cfg attrReader, diags *diag.Diagnostics) bool {
	if r.spec.Waiter == nil || r.spec.Waiter.PopulatedPathAttribute == "" {
		return false
	}
	var v types.Bool
	diags.Append(cfg.GetAttribute(ctx, path.Root(r.spec.Waiter.PopulatedPathAttribute), &v)...)
	if diags.HasError() || v.IsNull() || v.IsUnknown() {
		return false
	}
	return v.ValueBool()
}

// isNonEmptyValue reports whether a decoded JSON value carries content: a
// non-empty string/slice/map, a true bool, or any non-nil scalar.
func isNonEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	case bool:
		return t
	default:
		return true
	}
}

// readPathParamScope reads every path-parameter attribute declared by endpoint
// from reader, returning a name→value scope map and the ordered values for ID
// composition. reader is satisfied by tfsdk.Plan, tfsdk.State, and tfsdk.Config —
// so this function works for both resources and data sources.
func readPathParamScope(ctx context.Context, endpoint duplosdk.Endpoint, reader attrReader, diags *diag.Diagnostics) (map[string]string, []string) {
	params := endpoint.PathParams()
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

func (r *dynamicResource) scopeFromReader(ctx context.Context, reader attrReader, diags *diag.Diagnostics) (map[string]string, []string) {
	return readPathParamScope(ctx, r.endpoint, reader, diags)
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

// resourceID composes the state id. An association resource has no object id —
// its identity is exactly its path parameters — so the id is just those values
// joined ("<workspace_id>/<scope_id>"), with no trailing empty segment.
func (r *dynamicResource) resourceID(scopeValues []string, objID string) string {
	if r.spec.Association != nil {
		return strings.Join(scopeValues, "/")
	}
	return composeID(scopeValues, objID)
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
	var failureInterval time.Duration
	if w.FailurePollIntervalSeconds > 0 {
		failureInterval = time.Duration(w.FailurePollIntervalSeconds) * time.Second
	}
	statusSegs := strings.Split(w.StatusPath, ".")
	waiter := &duplosdk.Waiter[map[string]any]{
		PollInterval:        interval,
		FailurePollInterval: failureInterval,
		SuccessState:        w.SuccessState,
		FailureStates:       w.FailureStates,
		FailureRetries:      failureRetries,
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
	if w.ReadyPath != "" && w.ReadyState != "" {
		readySegs := strings.Split(w.ReadyPath, ".")
		waiter.ReadyState = w.ReadyState
		waiter.ReadyFn = func(m *map[string]any) string {
			return toStringValue(extractPath(*m, readySegs))
		}
	}
	if w.ReadyFailurePath != "" && len(w.ReadyFailureStates) > 0 {
		readyFailureSegs := strings.Split(w.ReadyFailurePath, ".")
		waiter.ReadyFailureFn = func(m *map[string]any) string {
			return toStringValue(extractPath(*m, readyFailureSegs))
		}
		waiter.ReadyFailureStates = w.ReadyFailureStates
	}
	return waiter
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

// jsonEqual reports whether two Go values (scalars or slices from
// readPlanGoValue) are equal by JSON encoding — handles strings, numbers, bools,
// and string lists without per-type comparison.
func jsonEqual(a, b any) bool {
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
	planBody := r.bodyFromRaw(req.Plan.Raw, "update", &resp.Diagnostics)
	stateBody := r.bodyFromRaw(req.State.Raw, "update", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !apiBodyEqual(planBody, stateBody) {
		return // a real update — let computed outputs recompute normally
	}

	// Single-intent updates carry their change in createPath fields that are
	// absent from the update body above, so apiBodyEqual misses them. Check the
	// updateIntent attributes directly: if one changed it IS a real update, so
	// don't pin computed outputs (let them recompute / stay known-after-apply).
	if r.spec.SingleIntentUpdate != nil {
		for _, a := range r.spec.Attributes {
			if a.UpdateIntent == nil {
				continue
			}
			pv, pok := readPlanGoValue(ctx, req.Plan, a, &resp.Diagnostics)
			sv, _ := readPlanGoValue(ctx, req.State, a, &resp.Diagnostics)
			if pok && !jsonEqual(pv, sv) {
				return
			}
		}
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
	if resp.Diagnostics.HasError() {
		return
	}

	// An association is created by POSTing the link path itself — both ids are in
	// the URL, there is no body, and the response carries no content.
	if r.spec.Association != nil {
		id := r.resourceID(scopeVals, "")
		log.Printf("[TRACE] dynamic %s Create(%s): association", r.spec.Name, id)
		if clientErr := r.api(scope, r.specFailureRetries()).CreateNoContent(); clientErr != nil {
			resp.Diagnostics.AddError("Error creating "+r.spec.Name, clientErr.Error())
			return
		}
		state := r.stateFromResponse(ctx, req.Plan.Raw, map[string]any{}, scope, id, false, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.State.Raw = state
		return
	}

	body := r.bodyFromRaw(req.Plan.Raw, "create", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	log.Printf("[TRACE] dynamic %s Create(%s): start", r.spec.Name, strings.Join(scopeVals, "/"))

	retries := r.failureRetries(ctx, req.Plan, &resp.Diagnostics)
	waitPopulated := r.waitForPopulated(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// The create call itself gets the create timeout, not the client's 60s
	// default — a backend that provisions synchronously can block for minutes.
	api := r.apiWaitingForPopulated(scope, retries, waitPopulated).
		WithCallTimeout(r.operationTimeout(ctx, req.Plan, "create", &resp.Diagnostics))
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := api.Create(&body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating "+r.spec.Name, err.Error())
		return
	}
	objID := toStringValue(extractPath(*created, strings.Split(r.spec.IDPath, ".")))
	id := r.resourceID(scopeVals, objID)

	// From here on, the object exists in the backend. If a later step (the
	// readiness wait, a post-create refresh, or the updateAfterCreate
	// follow-up) fails, we must still record its id in state — otherwise
	// Terraform believes creation never happened, and the next apply issues a
	// fresh POST that collides with the object still sitting in the backend
	// (e.g. a Helm release whose install failed: the record was created, but
	// Flux's Stalled condition trips WaitUntilReady's failure gate). Saving
	// what we know here lets Terraform track it as tainted and reconcile it
	// on the next apply instead of requiring a manual delete out of band.
	saveKnownState := func(obj map[string]any) {
		state := r.stateFromResponse(ctx, req.Plan.Raw, obj, scope, id, false, &resp.Diagnostics)
		if !resp.Diagnostics.HasError() {
			resp.State.Raw = state
		}
	}

	final := created
	if r.spec.Waiter != nil {
		timeout := r.timeout(ctx, req.Plan, "create", &resp.Diagnostics)
		final, err = api.WaitUntilReady(ctx, objID, timeout)
		if err != nil {
			saveKnownState(*created)
			resp.Diagnostics.AddError("Error waiting for "+r.spec.Name, err.Error())
			return
		}
	} else if r.spec.ReadAfterWrite {
		// The create response may differ from the canonical read (e.g. encrypted
		// fields). Refresh from a GET so state matches what reads return.
		final, err = api.Get(objID)
		if err != nil {
			saveKnownState(*created)
			resp.Diagnostics.AddError("Error reading "+r.spec.Name+" after create", err.Error())
			return
		}
	}

	// Some backends apply a subset of fields only on the update path, never on
	// create (e.g. Azure storage account data protection is applied by
	// UpdateInCloud, not CreateInCloud). When updateAfterCreate is set, issue a
	// follow-up update once the resource is ready so those fields take effect on
	// first apply. The update body is idempotent for fields create already applied.
	if r.spec.UpdateAfterCreate {
		updBody := r.updateBodyFromRaw(req.Plan.Raw, objID, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			saveKnownState(*final)
			return
		}
		if _, err = api.Update(objID, &updBody); err != nil {
			saveKnownState(*final)
			resp.Diagnostics.AddError("Error applying post-create update for "+r.spec.Name, err.Error())
			return
		}
		if r.spec.Waiter != nil {
			timeout := r.timeout(ctx, req.Plan, "update", &resp.Diagnostics)
			ready, waitErr := api.WaitUntilReady(ctx, objID, timeout)
			if waitErr != nil {
				saveKnownState(*final)
				resp.Diagnostics.AddError("Error waiting for "+r.spec.Name+" post-create update", waitErr.Error())
				return
			}
			final = ready
		} else {
			got, getErr := api.Get(objID)
			if getErr != nil {
				saveKnownState(*final)
				resp.Diagnostics.AddError("Error reading "+r.spec.Name+" after post-create update", getErr.Error())
				return
			}
			final = got
		}
	}

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

	if r.spec.Association != nil {
		r.readAssociation(ctx, req, resp, scope, id)
		return
	}

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

// readAssociation refreshes a link-only resource. The API offers no GET for the
// link itself, so the parent is fetched and the link counts as present only
// while the member value still appears in the parent's list. When it does not —
// the link was removed out of band, or the parent is gone — the resource is
// dropped from state so the next plan recreates it, rather than reporting a
// mapping that no longer exists.
func (r *dynamicResource) readAssociation(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse, scope map[string]string, id string) {
	as := r.spec.Association
	parentPath := r.endpoint.ResolvePath(as.ReadPath, scope)

	parent, clientErr := r.api(scope, r.specFailureRetries()).GetPath(parentPath)
	if clientErr != nil {
		if clientErr.IsNotFound() {
			// Parent gone ⇒ the link cannot exist either.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading "+r.spec.Name, clientErr.Error())
		return
	}

	want := scope[as.MemberAttribute]
	members := anyToStringSlice(extractPath(*parent, strings.Split(as.MemberPath, ".")))
	if !slices.Contains(members, want) {
		log.Printf("[TRACE] dynamic %s Read(%s): %s no longer in %s, removing from state",
			r.spec.Name, id, want, as.MemberPath)
		resp.State.RemoveResource(ctx)
		return
	}

	// Still linked. Nothing is read back into attributes — every one of them is a
	// path parameter — but rebuilding state re-asserts them from the parsed id,
	// which is what makes import work.
	resp.State.Raw = r.stateFromResponse(ctx, req.State.Raw, map[string]any{}, scope, id, true, &resp.Diagnostics)
	log.Printf("[TRACE] dynamic %s Read(%s): end", r.spec.Name, id)
}

func (r *dynamicResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	scope, objID, id, err := r.parseID(ctx, req.State, &resp.Diagnostics)
	if err != nil || resp.Diagnostics.HasError() {
		return
	}

	// An association has no readable object and nothing to update — every
	// attribute is a forceNew path parameter, so a real change replaces the link.
	// Any diff reaching here is config-only; persist the plan as-is.
	if r.spec.Association != nil {
		resp.State.Raw = req.Plan.Raw
		log.Printf("[TRACE] dynamic %s Update(%s): association, nothing to update", r.spec.Name, id)
		return
	}

	// Immutable resource (no Update operation registered): every attribute is
	// forceNew, so attribute changes force replacement and never reach here. The
	// only diffs that do are config-only (e.g. the timeouts block) — refresh
	// computed values from the API and persist, without issuing an update call.
	if !r.endpoint.HasUpdate() {
		obj, clientErr := r.api(scope, r.specFailureRetries()).Get(objID)
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

	// Single-intent update: mutate each changed attribute in its own PUT, waiting
	// for the ready state between updates (e.g. AWS MSK broker count / storage /
	// instance type / kafka version).
	if r.spec.SingleIntentUpdate != nil {
		r.singleIntentUpdate(ctx, req, resp, scope, objID, id)
		return
	}

	body := r.updateBodyFromRaw(req.Plan.Raw, objID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	log.Printf("[TRACE] dynamic %s Update(%s): start", r.spec.Name, id)

	// If nothing the API cares about changed (e.g. only the timeouts block
	// differs, as on the first apply after import), skip the API call and the
	// waiter entirely — issuing a PUT would re-trigger provisioning for a
	// metadata-only change. ModifyPlan has already carried prior computed values
	// into the plan, so it is fully known and safe to persist directly.
	// Built the same way as the plan body (id included) so the injected id cannot
	// make the two differ and defeat the skip.
	stateBody := r.updateBodyFromRaw(req.State.Raw, objID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if apiBodyEqual(body, stateBody) {
		resp.State.Raw = req.Plan.Raw
		log.Printf("[TRACE] dynamic %s Update(%s): no API-relevant change, skipped", r.spec.Name, id)
		return
	}

	retries := r.failureRetries(ctx, req.Plan, &resp.Diagnostics)
	waitPopulated := r.waitForPopulated(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	api := r.apiWaitingForPopulated(scope, retries, waitPopulated).
		WithCallTimeout(r.operationTimeout(ctx, req.Plan, "update", &resp.Diagnostics))
	if resp.Diagnostics.HasError() {
		return
	}
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
	} else if r.spec.ReadAfterWrite {
		final, clientErr = api.Get(objID)
		if clientErr != nil {
			resp.Diagnostics.AddError("Error reading "+r.spec.Name+" after update", clientErr.Error())
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

// singleIntentUpdate applies each changed UpdateIntent attribute as its own PUT
// (discriminator + one target value), waiting for the resource to reach the
// configured ready state before and after each update. Used by APIs (e.g. AWS
// MSK) that accept one discrete update at a time.
func (r *dynamicResource) singleIntentUpdate(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse, scope map[string]string, objID, id string) {
	si := r.spec.SingleIntentUpdate

	// Collect the intent attributes whose value changed plan-vs-state.
	type change struct {
		intent *UpdateIntentSpec
		value  any
	}
	var changes []change
	for i := range r.spec.Attributes {
		a := r.spec.Attributes[i]
		if a.UpdateIntent == nil {
			continue
		}
		planVal, planOK := readPlanGoValue(ctx, req.Plan, a, &resp.Diagnostics)
		stateVal, _ := readPlanGoValue(ctx, req.State, a, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if !planOK || jsonEqual(planVal, stateVal) {
			continue
		}
		changes = append(changes, change{intent: a.UpdateIntent, value: planVal})
	}

	// No update-intent change (e.g. only the timeouts block differs) — persist
	// the plan without any API call.
	if len(changes) == 0 {
		resp.State.Raw = req.Plan.Raw
		log.Printf("[TRACE] dynamic %s Update(%s): no update-intent change, skipped", r.spec.Name, id)
		return
	}

	retries := r.failureRetries(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	api := r.api(scope, retries)
	timeout := r.timeout(ctx, req.Plan, "update", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	readySegs := strings.Split(si.ReadyPath, ".")
	readyWaiter := &duplosdk.Waiter[map[string]any]{
		PollInterval:  defaultPollInterval,
		SuccessState:  si.ReadyState,
		FailureStates: si.ReadyFailureStates,
		StatusFn:      func(m *map[string]any) string { return toStringValue(extractPath(*m, readySegs)) },
	}
	waitReady := func() bool {
		if _, clientErr := readyWaiter.Wait(ctx, id, timeout, func() (*map[string]any, duplosdk.ClientError) {
			return api.Get(objID)
		}); clientErr != nil {
			resp.Diagnostics.AddError("Error waiting for "+r.spec.Name+" to be ready for update", clientErr.Error())
			return false
		}
		return true
	}

	discSegs := strings.Split(si.DiscriminatorPath, ".")
	for _, c := range changes {
		if !waitReady() { // must be ready before each single-intent update
			return
		}
		// Start from the normal update body so identity/context fields (name,
		// environment_id, resource_group_id, …) the backend needs to resolve the
		// resource are present, then overlay this one update intent. Create-only
		// and other update-intent fields are excluded by bodyFromRaw.
		body := r.updateBodyFromRaw(req.Plan.Raw, objID, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		setPath(body, discSegs, c.intent.DiscriminatorValue)
		setPath(body, strings.Split(c.intent.ValuePath, "."), c.value)
		log.Printf("[TRACE] dynamic %s Update(%s): single-intent %s", r.spec.Name, id, c.intent.DiscriminatorValue)
		if _, clientErr := api.Update(objID, &body); clientErr != nil {
			resp.Diagnostics.AddError("Error updating "+r.spec.Name+" ("+c.intent.DiscriminatorValue+")", clientErr.Error())
			return
		}
		if !waitReady() { // wait for this update to complete before the next
			return
		}
	}

	final, clientErr := api.Get(objID)
	if clientErr != nil {
		resp.Diagnostics.AddError("Error reading "+r.spec.Name+" after update", clientErr.Error())
		return
	}
	state := r.stateFromResponse(ctx, req.Plan.Raw, *final, scope, id, false, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.Raw = state
	log.Printf("[TRACE] dynamic %s Update(%s): single-intent updates complete (%d)", r.spec.Name, id, len(changes))
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

	// The delete timeout governs both the waiter and the HTTP deadline of the
	// calls that start the teardown. Deprovision/delete can run for minutes on a
	// synchronous backend, and cutting the connection at the client's 60s default
	// cancels the server's work rather than merely losing the reply.
	timeout := r.operationTimeout(ctx, req.State, "delete", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	api = api.WithCallTimeout(timeout)

	// Resources the API refuses to delete while live declare a Deprovision
	// operation: tear down the underlying cloud resources and wait for the
	// deprovisioned state before issuing the delete call. A deprovision
	// "skipWhen" condition (e.g. cloud=K8S_ONLY, which provisions no cloud infra)
	// bypasses this step and deletes directly.
	if r.endpoint.HasDeprovision() && !r.deprovisionSkipped(ctx, req.State) {
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

// deprovisionSkipped reports whether the pre-delete deprovision step should be
// bypassed for this delete, based on the deprovision operation's SkipWhen
// conditions evaluated against prior state (all must hold — logical AND). Use for
// modes with no cloud infra to tear down (e.g. cloud=K8S_ONLY): the engine then
// deletes directly. Returns false when no SkipWhen is configured.
func (r *dynamicResource) deprovisionSkipped(ctx context.Context, state attrReader) bool {
	dp := r.spec.Endpoint.Deprovision
	if dp == nil || len(dp.SkipWhen) == 0 {
		return false
	}
	for _, c := range dp.SkipWhen {
		a := r.spec.attr(c.Attribute)
		if a == nil {
			return false
		}
		if c.IsEmpty || c.IsNotEmpty {
			// Collection-aware presence test — an empty list is "not set".
			if attrEmptyAtPath(ctx, state, *a, path.Root(a.Name)) != c.IsEmpty {
				return false
			}
			continue
		}
		val := readConfigString(ctx, state, *a)
		if val == "" {
			val = defaultString(*a) // attribute omitted — fall back to its spec default
		}
		switch {
		case c.NotEquals != "":
			if val == c.NotEquals {
				return false
			}
		default:
			if val != c.Equals {
				return false
			}
		}
	}
	return true
}

func (r *dynamicResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── Conditional-required validation ───────────────────────────────────────────

func (r *dynamicResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	var vs []resource.ConfigValidator
	if len(r.spec.RequiredIf) > 0 {
		vs = append(vs, requiredIfValidator{spec: r.spec})
	}
	if len(r.spec.ConflictsWith) > 0 {
		vs = append(vs, conflictsWithValidator{spec: r.spec})
	}
	if len(r.spec.InvalidWhen) > 0 {
		vs = append(vs, invalidWhenValidator{spec: r.spec})
	}
	return vs
}

// invalidWhenValidator enforces ResourceSpec.InvalidWhen: a rule whose conditions
// all hold rejects the config at plan time, so a combination the API would refuse
// never reaches apply.
type invalidWhenValidator struct{ spec ResourceSpec }

func (v invalidWhenValidator) Description(_ context.Context) string {
	return "Rejects attribute combinations the API refuses, as declared in the resource spec."
}
func (v invalidWhenValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v invalidWhenValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	for _, rule := range v.spec.InvalidWhen {
		if !v.holds(ctx, req.Config, rule.When) {
			continue
		}
		at := rule.Attribute
		if at == "" {
			at = rule.When[0].Attribute
		}
		resp.Diagnostics.AddAttributeError(dottedPath(at), "Invalid attribute combination", rule.Message)
	}
}

// holds reports whether every condition matches (logical AND). An unknown value
// stops evaluation: it is a reference to another resource in the same apply, so the
// combination cannot be judged yet and must not be reported as invalid.
func (v invalidWhenValidator) holds(ctx context.Context, cfg attrReader, conds []RequiredIfCondition) bool {
	for _, c := range conds {
		target := v.spec.leafAt(c.Attribute)
		if target == nil {
			return false
		}
		p := dottedPath(c.Attribute)
		if readPathUnknown(ctx, cfg, *target, p) {
			return false
		}
		switch {
		case c.GreaterThan != nil, c.LessThan != nil, c.LessThanAttribute != "":
			left, ok := readPathNumber(ctx, cfg, *target, p)
			if !ok {
				return false
			}
			switch {
			case c.GreaterThan != nil:
				if !(left > *c.GreaterThan) {
					return false
				}
			case c.LessThan != nil:
				if !(left < *c.LessThan) {
					return false
				}
			default:
				other := v.spec.leafAt(c.LessThanAttribute)
				if other == nil {
					return false
				}
				op := dottedPath(c.LessThanAttribute)
				if readPathUnknown(ctx, cfg, *other, op) {
					return false
				}
				right, ok := readPathNumber(ctx, cfg, *other, op)
				if !ok || !(left < right) {
					return false
				}
			}
		case c.IsEmpty, c.IsNotEmpty:
			// Collection-aware presence test — an empty list is "not set".
			if attrEmptyAtPath(ctx, cfg, *target, p) != c.IsEmpty {
				return false
			}
		default:
			val := readPathString(ctx, cfg, *target, p)
			if val == "" {
				val = defaultString(*target)
			}
			switch {
			case c.NotEquals != "":
				if val == c.NotEquals {
					return false
				}
			default:
				if val != c.Equals {
					return false
				}
			}
		}
	}
	return len(conds) > 0
}

// dottedPath turns "upgrade_settings.max_surge_type" into the framework path
// Root("upgrade_settings").AtName("max_surge_type").
func dottedPath(dotted string) path.Path {
	segs := strings.Split(dotted, ".")
	p := path.Root(segs[0])
	for _, seg := range segs[1:] {
		p = p.AtName(seg)
	}
	return p
}

// conflictsWithValidator enforces ResourceSpec.ConflictsWith: within each group,
// at most one attribute may be set in the config.
type conflictsWithValidator struct{ spec ResourceSpec }

func (v conflictsWithValidator) Description(_ context.Context) string {
	return "Enforces mutually-exclusive attribute groups declared in the resource spec."
}
func (v conflictsWithValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v conflictsWithValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	for _, group := range v.spec.ConflictsWith {
		set := make([]string, 0, len(group))
		for _, name := range group {
			a := v.spec.attr(name)
			if a != nil && !configAttrNull(req.Config.Raw, a.Name) {
				set = append(set, name)
			}
		}
		if len(set) > 1 {
			for _, name := range set {
				resp.Diagnostics.AddAttributeError(
					path.Root(name),
					"Conflicting attributes",
					fmt.Sprintf("only one of %s may be set; got %s.", strings.Join(group, ", "), strings.Join(set, ", ")),
				)
			}
		}
	}
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
		target := v.spec.attr(rule.Attribute)
		if target == nil || !v.conditionsHold(ctx, req.Config, rule.conditions()) {
			continue
		}
		if configAttrNull(req.Config.Raw, target.Name) {
			resp.Diagnostics.AddAttributeError(
				path.Root(rule.Attribute),
				"Missing required attribute",
				requiredIfMessage(rule),
			)
		}
	}
}

// conditionsHold reports whether every condition matches (logical AND). Each
// condition reads the config value, falling back to the attribute's spec default
// when the user omitted it, so conditions on defaulted fields evaluate correctly.
func (v requiredIfValidator) conditionsHold(ctx context.Context, cfg attrReader, conds []RequiredIfCondition) bool {
	for _, c := range conds {
		when := v.spec.attr(c.Attribute)
		if when == nil {
			return false
		}
		if readConfigUnknown(ctx, cfg, *when) {
			// Unresolved reference to another resource created in the same apply
			// (e.g. network_baseline_id = duploai_network_baseline.x.network_id).
			// It is configured, just not yet known — don't evaluate conditions
			// against it, matching configAttrNull's treatment of unknown as "set"
			// for ConflictsWith. Otherwise IsEmpty would wrongly hold (readConfigString
			// collapses unknown to "" the same as null) and fire a false requiredIf error.
			return false
		}
		if c.IsEmpty || c.IsNotEmpty {
			// Collection-aware presence test — an empty list is "not set".
			if attrEmptyAtPath(ctx, cfg, *when, path.Root(when.Name)) != c.IsEmpty {
				return false
			}
			continue
		}
		val := readConfigString(ctx, cfg, *when)
		if val == "" {
			val = defaultString(*when) // user omitted it — use the spec default
		}
		switch {
		case c.NotEquals != "":
			if val == c.NotEquals {
				return false
			}
		default:
			if val != c.Equals {
				return false
			}
		}
	}
	return len(conds) > 0
}

// defaultString renders an attribute's static default as a string, or "" if none.
func defaultString(a AttributeSpec) string {
	if a.Default == nil {
		return ""
	}
	var v any
	if err := json.Unmarshal(*a.Default, &v); err != nil {
		return ""
	}
	return toStringValue(v)
}

// requiredIfMessage builds a human-readable explanation of a requiredIf rule.
func requiredIfMessage(rule RequiredIfRule) string {
	parts := make([]string, 0, len(rule.conditions()))
	for _, c := range rule.conditions() {
		switch {
		case c.IsEmpty:
			parts = append(parts, fmt.Sprintf("%s is not set", c.Attribute))
		case c.IsNotEmpty:
			parts = append(parts, fmt.Sprintf("%s is set", c.Attribute))
		case c.NotEquals != "":
			parts = append(parts, fmt.Sprintf("%s is not %q", c.Attribute, c.NotEquals))
		default:
			parts = append(parts, fmt.Sprintf("%s is %q", c.Attribute, c.Equals))
		}
	}
	return fmt.Sprintf("%q must be set when %s.", rule.Attribute, strings.Join(parts, " and "))
}

// ── Plan/state plumbing ───────────────────────────────────────────────────────

// attrReader is satisfied by tfsdk.Plan, tfsdk.State, and tfsdk.Config — all of
// which expose GetAttribute. Using the interface lets one set of helpers serve
// every CRUD phase.
type attrReader interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

// bodyFromRaw builds the request body from the plan's raw value. verb is
// "create" or "update" and selects the effective path for each attribute
// (createPath vs updatePath; see AttributeSpec). Working from the raw tftypes
// lets one code path serialize attributes of any type — scalars, collections,
// and arbitrarily nested objects — without per-type plan reads.
// findAttr returns the attribute with the given name, or nil.
func findAttr(attrs []AttributeSpec, name string) *AttributeSpec {
	for i := range attrs {
		if attrs[i].Name == name {
			return &attrs[i]
		}
	}
	return nil
}

// anyToStringSlice coerces a decoded JSON value (or tftypesToGo output) into a
// []string, ignoring non-string elements.
func anyToStringSlice(v any) []string {
	out := []string{}
	arr, ok := v.([]any)
	if !ok {
		return out
	}
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// rawStringSet decodes a tftypes list/set of strings into a membership set,
// returning an empty set for null/unknown values.
func rawStringSet(v tftypes.Value) map[string]bool {
	out := map[string]bool{}
	if v.IsNull() || !v.IsKnown() {
		return out
	}
	var elems []tftypes.Value
	if err := v.As(&elems); err != nil {
		return out
	}
	for _, e := range elems {
		if e.IsNull() || !e.IsKnown() {
			continue
		}
		var s string
		if err := e.As(&s); err == nil {
			out[s] = true
		}
	}
	return out
}

// preserveUnion returns the de-duplicated union of a PreserveUnmanagedInto
// attribute's own values and those of its computed sibling, as a []any of
// strings ready for the request body. Either side may be null/unknown.
func preserveUnion(a AttributeSpec, attrs []AttributeSpec, top map[string]tftypes.Value) []any {
	seen := map[string]bool{}
	out := []any{}
	add := func(spec AttributeSpec) {
		v, ok := top[spec.Name]
		if !ok {
			return
		}
		g, ok := attrToRequest(spec, v)
		if !ok {
			return
		}
		for _, s := range anyToStringSlice(g) {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	add(a)
	if sib := findAttr(attrs, a.PreserveUnmanagedInto); sib != nil {
		add(*sib)
	}
	return out
}

// resendCreatePath writes an attribute's CREATE path into an update body as well,
// when the spec sets ResendCreatePathsOnUpdate. Backends that rebuild their stored
// document from the request body need the create shape alongside the update
// envelope, or every field the envelope does not treat as changed falls back to a
// type default. No-op on create, and on any attribute whose two paths coincide.
func (r *dynamicResource) resendCreatePath(body map[string]any, a AttributeSpec, val any, verb, reqPath string) {
	if verb != "update" || !r.spec.ResendCreatePathsOnUpdate {
		return
	}
	if cp := a.effectiveCreatePath(); cp != "" && cp != reqPath {
		setPath(body, strings.Split(cp, "."), val)
	}
}

func (r *dynamicResource) bodyFromRaw(raw tftypes.Value, verb string, diags *diag.Diagnostics) map[string]any {
	var top map[string]tftypes.Value
	if err := raw.As(&top); err != nil {
		diags.AddError("Internal error", fmt.Sprintf("decoding plan: %s", err))
		return nil
	}
	body := map[string]any{}
	for _, a := range r.spec.Attributes {
		var reqPath string
		if verb == "update" {
			reqPath = a.effectiveUpdatePath()
		} else {
			reqPath = a.effectiveCreatePath()
		}
		if reqPath == "" || a.NoSend || (!a.Required && !a.Optional && !a.SendFromState) {
			continue
		}
		if a.PreserveUnmanagedInto != "" {
			// Send the union of this attribute and its computed sibling so the
			// server-managed entries the sibling holds survive a full-document
			// update instead of being cleared.
			if union := preserveUnion(a, r.spec.Attributes, top); len(union) > 0 {
				setPath(body, strings.Split(reqPath, "."), union)
				r.resendCreatePath(body, a, union, verb, reqPath)
			}
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
		// The create shape keeps the untransformed value: UpdateBoolTrueValue rewrites
		// what the UPDATE path carries, not what the create path expects.
		createVal := val
		// String-enum → bool on update: e.g. image_tag_mutability "IMMUTABLE"
		// (create string) maps to enableTagImmutability true (update bool).
		if verb == "update" && a.UpdateBoolTrueValue != "" {
			s, _ := val.(string)
			val = s == a.UpdateBoolTrueValue
		}
		setPath(body, strings.Split(reqPath, "."), val)
		r.resendCreatePath(body, a, createVal, verb, reqPath)
	}
	applyConstants(body, r.spec.RequestConstants, diags)
	if verb == "update" {
		applyConstants(body, r.spec.UpdateConstants, diags)
	} else {
		applyConstants(body, r.spec.CreateConstants, diags)
	}
	return body
}

// updateBodyFromRaw builds the PUT body and, when the spec sets idRequestPath,
// writes the backend object id into it. Required by APIs that validate a
// full-document update against the id in the BODY rather than the one in the
// route — see ResourceSpec.IDRequestPath. Used by every real update call; the
// no-op comparison in ModifyPlan deliberately skips it, since the id is equal on
// both sides and would only add noise.
func (r *dynamicResource) updateBodyFromRaw(raw tftypes.Value, objID string, diags *diag.Diagnostics) map[string]any {
	body := r.bodyFromRaw(raw, "update", diags)
	if body != nil && r.spec.IDRequestPath != "" && objID != "" {
		setPath(body, strings.Split(r.spec.IDRequestPath, "."), objID)
	}
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

// operationTimeout is timeout() for callers that must work whether or not the
// resource declares a waiter. Without a waiter there is no timeouts block in the
// schema to read, so the engine default for the operation applies.
//
// It doubles as the per-request HTTP deadline for the call that STARTS the
// operation (see RESTResource.WithCallTimeout). The client default is 60s, which
// is fine for metadata calls but far too short for a backend that performs a
// cloud create or teardown synchronously — and a client disconnect there does
// not merely lose the response, it cancels the server's work.
func (r *dynamicResource) operationTimeout(ctx context.Context, reader attrReader, op string, diags *diag.Diagnostics) time.Duration {
	if r.spec.Waiter != nil {
		return r.timeout(ctx, reader, op, diags)
	}
	switch op {
	case "create":
		return defaultCreateTimeout
	case "update":
		return defaultUpdateTimeout
	default:
		return defaultDeleteTimeout
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
	// An association id carries only the path parameters — there is no object id
	// segment to account for.
	want := len(params) + 1
	if r.spec.Association != nil {
		want = len(params)
	}
	parts, splitErr := splitID(id, want)
	if splitErr != nil {
		diags.AddError("Invalid resource ID", splitErr.Error())
		return nil, "", id, splitErr
	}
	scope = make(map[string]string, len(params))
	for i, p := range params {
		scope[p] = parts[i]
	}
	if r.spec.Association == nil {
		objID = parts[len(params)]
	}
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
	case "set(string)":
		var v types.Set
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
	case "map(string)":
		var v types.Map
		diags.Append(plan.GetAttribute(ctx, path.Root(a.Name), &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		out := make(map[string]string, len(v.Elements()))
		for k, e := range v.Elements() {
			if s, ok := e.(types.String); ok {
				out[k] = s.ValueString()
			}
		}
		return out, true
	case "number":
		var v types.Float64
		diags.Append(plan.GetAttribute(ctx, path.Root(a.Name), &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		return v.ValueFloat64(), true
	case "object":
		var v types.Object
		diags.Append(plan.GetAttribute(ctx, path.Root(a.Name), &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		return v.Attributes(), true
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

// attrEmptyAtPath reports whether the value at p is empty, for isEmpty /
// isNotEmpty conditions. Collections are answered by element count rather than by
// their string rendering: readPathString renders every non-scalar as "" and
// readConfigString renders an empty list as "[]", so neither form tells an empty
// collection apart from an unset one. Scalars fall back to the string render with
// the attribute's static default applied, so a condition on a defaulted field
// still evaluates. An unknown value reads as empty here, which is only safe
// because the config-time callers bail out on unknown first (readConfigUnknown /
// readPathUnknown) and the deprovision caller reads prior state, where nothing is
// unknown.
func attrEmptyAtPath(ctx context.Context, cfg attrReader, a AttributeSpec, p path.Path) bool {
	switch {
	case strings.HasPrefix(a.Type, "list("):
		var v types.List
		cfg.GetAttribute(ctx, p, &v)
		return v.IsNull() || v.IsUnknown() || len(v.Elements()) == 0
	case strings.HasPrefix(a.Type, "set("):
		var v types.Set
		cfg.GetAttribute(ctx, p, &v)
		return v.IsNull() || v.IsUnknown() || len(v.Elements()) == 0
	case strings.HasPrefix(a.Type, "map("):
		var v types.Map
		cfg.GetAttribute(ctx, p, &v)
		return v.IsNull() || v.IsUnknown() || len(v.Elements()) == 0
	case a.Type == "object":
		var v types.Object
		cfg.GetAttribute(ctx, p, &v)
		return v.IsNull() || v.IsUnknown()
	}
	val := readPathString(ctx, cfg, a, p)
	if val == "" {
		val = defaultString(a)
	}
	return val == ""
}

// readPathUnknown reports whether the value at p is unknown. Same distinction as
// readConfigUnknown, but at an arbitrary path so a leaf inside an object can be
// tested.
func readPathUnknown(ctx context.Context, cfg attrReader, a AttributeSpec, p path.Path) bool {
	switch a.Type {
	case "string":
		var v types.String
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case "bool":
		var v types.Bool
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case "int":
		var v types.Int64
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case "number":
		var v types.Float64
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case "list(string)":
		var v types.List
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case "set(string)":
		var v types.Set
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case "map(string)":
		var v types.Map
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case "object":
		var v types.Object
		cfg.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	default:
		return false
	}
}

// readPathString renders the leaf value at p as a string, or "" when it is null,
// unknown or not a leaf type.
func readPathString(ctx context.Context, cfg attrReader, a AttributeSpec, p path.Path) string {
	switch a.Type {
	case "string":
		var v types.String
		cfg.GetAttribute(ctx, p, &v)
		if v.IsNull() || v.IsUnknown() {
			return ""
		}
		return v.ValueString()
	case "bool":
		var v types.Bool
		cfg.GetAttribute(ctx, p, &v)
		if v.IsNull() || v.IsUnknown() {
			return ""
		}
		return toStringValue(v.ValueBool())
	case "int":
		var v types.Int64
		cfg.GetAttribute(ctx, p, &v)
		if v.IsNull() || v.IsUnknown() {
			return ""
		}
		return toStringValue(v.ValueInt64())
	case "number":
		var v types.Float64
		cfg.GetAttribute(ctx, p, &v)
		if v.IsNull() || v.IsUnknown() {
			return ""
		}
		return toStringValue(v.ValueFloat64())
	default:
		return ""
	}
}

// readPathNumber reads the numeric leaf at p, falling back to the attribute's
// static default when the config leaves it null — so a rule comparing an explicit
// value against a defaulted one still evaluates. Reports false when there is no
// value to compare at all.
func readPathNumber(ctx context.Context, cfg attrReader, a AttributeSpec, p path.Path) (float64, bool) {
	switch a.Type {
	case "int":
		var v types.Int64
		cfg.GetAttribute(ctx, p, &v)
		if !v.IsNull() && !v.IsUnknown() {
			return float64(v.ValueInt64()), true
		}
	case "number":
		var v types.Float64
		cfg.GetAttribute(ctx, p, &v)
		if !v.IsNull() && !v.IsUnknown() {
			return v.ValueFloat64(), true
		}
	default:
		return 0, false
	}
	if a.Default == nil {
		return 0, false
	}
	var d float64
	if err := json.Unmarshal(*a.Default, &d); err != nil {
		return 0, false
	}
	return d, true
}

// readConfigUnknown reports whether a's value in cfg is unknown — an
// unresolved reference to another resource being created in the same apply
// (e.g. network_baseline_id = duploai_network_baseline.x.network_id). Unlike
// readConfigString, which collapses unknown to "" the same as a genuinely
// null value, this lets requiredIf condition checks distinguish the two —
// the same distinction configAttrNull makes for ConflictsWith.
func readConfigUnknown(ctx context.Context, cfg attrReader, a AttributeSpec) bool {
	return readPathUnknown(ctx, cfg, a, path.Root(a.Name))
}

// configAttrNull reports whether a top-level attribute is genuinely unset (null)
// in the raw config. An UNKNOWN value — e.g. a reference to another resource
// created in the same apply (network_id = duploai_network_baseline.x.network_id)
// — is treated as SET (returns false): it is configured, just not yet resolved,
// so config-time validators must not flag it as missing.
func configAttrNull(raw tftypes.Value, name string) bool {
	if !raw.IsKnown() || raw.IsNull() {
		return false // whole config unknown/null — don't flag individual attrs
	}
	obj := map[string]tftypes.Value{}
	if err := raw.As(&obj); err != nil {
		return false
	}
	v, ok := obj[name]
	if !ok {
		return true // attribute absent from config → null
	}
	if !v.IsKnown() {
		return false // unknown (unresolved reference) → treat as set
	}
	return v.IsNull()
}

// buildStateRaw builds a tftypes.Value state from a base raw value overlaid
// with API response data. It is shared by dynamicResource and dynamicDataSource.
//
// refreshInputs=false (Create/Update): keep configured values; only fill
// computed-only and still-unknown fields from the response.
// refreshInputs=true (Read / data source): replace all API-mapped attributes
// with live response values.
func buildStateRaw(attrs []AttributeSpec, baseRaw tftypes.Value, resp map[string]any, scope map[string]string, id string, refreshInputs, applyPreserveSplit bool, diags *diag.Diagnostics) tftypes.Value {
	objType, ok := baseRaw.Type().(tftypes.Object)
	if !ok {
		diags.AddError("Internal error", "state is not an object")
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

	for _, a := range attrs {
		if applyPreserveSplit && a.PreserveUnmanagedInto != "" {
			// Split the server's full list into the user-managed set (this
			// attribute) and the server-managed remainder (the computed
			// sibling). The managed set is whatever is already in baseRaw — the
			// plan on create/update, prior state on read.
			attrType, hasType := objType.AttributeTypes[a.Name]
			sib := findAttr(attrs, a.PreserveUnmanagedInto)
			if hasType && sib != nil {
				managed := rawStringSet(next[a.Name])
				server := anyToStringSlice(extractPath(resp, strings.Split(a.responsePath(), ".")))
				scopeSlice, otherSlice := []any{}, []any{}
				for _, s := range server {
					if managed[s] {
						scopeSlice = append(scopeSlice, s)
					} else {
						otherSlice = append(otherSlice, s)
					}
				}
				// Keep the configured plan value when fully known (avoids
				// "inconsistent result after apply"); otherwise fill from the
				// response. On read, always reflect the server intersection.
				if refreshInputs || !next[a.Name].IsFullyKnown() {
					next[a.Name] = attrFromResponse(a, attrType, scopeSlice)
				}
				next[sib.Name] = attrFromResponse(*sib, objType.AttributeTypes[sib.Name], otherSlice)
			}
			continue
		}
		respPaths := a.responsePathList()
		if len(respPaths) == 0 {
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
		goVal := extractFirstNonEmpty(resp, respPaths)
		if len(a.FilterResponseKeys) > 0 {
			goVal = filterMapKeys(goVal, a.FilterResponseKeys)
		}
		if !computedOnly && !refreshInputs {
			// Preserve the user's configured (known) leaves and fill only the
			// still-unknown ones from the response, so we never replace a
			// configured value with a server-normalized one.
			next[a.Name] = mergeUnknownFromResponse(a, attrType, next[a.Name], goVal)
		} else {
			next[a.Name] = attrFromResponse(a, attrType, goVal)
		}
		// Write-only leaves (e.g. a credential secret the API redacts on every
		// read) keep whatever the base value already held rather than the empty
		// value the response carries.
		next[a.Name] = restorePreservedValues(a, current[a.Name], next[a.Name])
	}

	return tftypes.NewValue(objType, next)
}

func (r *dynamicResource) stateFromResponse(_ context.Context, baseRaw tftypes.Value, resp map[string]any, scope map[string]string, id string, refreshInputs bool, diags *diag.Diagnostics) tftypes.Value {
	return buildStateRaw(r.spec.Attributes, baseRaw, resp, scope, id, refreshInputs, true, diags)
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

// extractFirstNonEmpty returns the value at the first path (of an ordered list)
// that yields something present — non-nil, and, for a string, non-empty. It
// backs AttributeSpec.ResponsePaths, letting a single cloud-agnostic output read
// from whichever per-cloud path is populated. Returns nil when none match.
func extractFirstNonEmpty(resp map[string]any, paths []string) any {
	var firstPresent any
	seen := false
	for _, p := range paths {
		v := extractPath(resp, strings.Split(p, "."))
		if v == nil {
			continue
		}
		if !seen {
			firstPresent, seen = v, true
		}
		// An empty string is "present but empty" — prefer a later path that may
		// hold a real value, but fall back to it if none does (so a legitimately
		// empty single-path value like description="" is preserved, not dropped).
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		return v
	}
	return firstPresent
}

// extractPath walks a decoded JSON value following dot-separated segments. A
// segment suffixed with "[]" treats the current value as an array and maps the
// remaining path over each element, yielding []any. A segment of the form
// "key[filterKey=filterValue]" instead selects the first element of the array
// at key whose filterKey field stringifies to filterValue — e.g. a
// Kubernetes-style conditions array: "conditions[type=Ready]" — and continues
// extraction from that single matched element.
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
	if open := strings.IndexByte(seg, '['); open >= 0 && strings.HasSuffix(seg, "]") {
		key := seg[:open]
		filter := seg[open+1 : len(seg)-1]
		if eq := strings.IndexByte(filter, '='); eq >= 0 {
			filterKey, filterVal := filter[:eq], filter[eq+1:]
			arr, ok := mapIndex(cur, key).([]any)
			if !ok {
				return nil
			}
			for _, el := range arr {
				if m, ok := el.(map[string]any); ok && toStringValue(m[filterKey]) == filterVal {
					return extractPath(m, segs[1:])
				}
			}
			return nil
		}
	}
	return extractPath(mapIndex(cur, seg), segs[1:])
}

// filterMapKeys returns cur with matching keys removed, when cur is a decoded
// JSON object (map[string]any). Used to drop backend-injected map entries (e.g.
// ALB annotations, platform duplocloud.ai/* labels) that the user does not
// manage, preventing perpetual drift. A pattern ending in "*" is a prefix match
// (e.g. "duplocloud.ai/*"); otherwise it's an exact-key match. Non-map values
// pass through unchanged.
func filterMapKeys(cur any, keys []string) any {
	m, ok := cur.(map[string]any)
	if !ok {
		return cur
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if !matchesFilterKey(k, keys) {
			out[k] = v
		}
	}
	return out
}

// flattenMapValues unwraps a response map whose entries are objects into a plain
// key → string map, taking each value from the valuePath field. When dropFlag is
// set, an entry whose named boolean is true is omitted entirely — a soft-deleted
// entry the backend still stores but that must not reach state (see
// AttributeSpec.MapDropWhenTrue). Entries already carrying a plain string pass
// through unchanged, so a backend that serves both the wrapped and the flat shape
// is handled; anything else is dropped rather than surfaced as a broken value.
func flattenMapValues(cur any, valuePath, dropFlag string) any {
	m, ok := cur.(map[string]any)
	if !ok {
		return cur
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch entry := v.(type) {
		case string:
			out[k] = entry
		case map[string]any:
			if dropFlag != "" {
				if flag, ok := entry[dropFlag].(bool); ok && flag {
					continue
				}
			}
			if s, ok := entry[valuePath].(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

// matchesFilterKey reports whether key matches any pattern — exact, or prefix
// when the pattern ends in "*".
func matchesFilterKey(key string, patterns []string) bool {
	for _, p := range patterns {
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(key, strings.TrimSuffix(p, "*")) {
				return true
			}
		} else if key == p {
			return true
		}
	}
	return false
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
