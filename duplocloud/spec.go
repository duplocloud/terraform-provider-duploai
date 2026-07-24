package duplocloud

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// all:specs embeds the directory (including the .gitkeep) so the provider still
// compiles when no resource specs are present yet — e.g. on the framework-only
// branch. loadResourceSpecs filters to *.json files.
//
//go:embed all:specs
var specFS embed.FS

// ResourceSpec is the data-driven description of a Terraform resource.
// One JSON file under specs/ produces one fully-functional resource — adding a
// resource requires no Go code, only a new spec file (the boto3 model).
type ResourceSpec struct {
	// Name is the resource type suffix, e.g. "foo" yields "duploai_foo".
	Name string `json:"name"`
	// Description is shown in generated docs and the schema.
	Description string `json:"description"`

	// IDPath is the dot-path into the create/read response that yields the
	// backend-assigned object id, e.g. "id".
	IDPath string `json:"idPath"`

	// Endpoint configures the API URIs and HTTP verbs for all CRUD operations.
	// It replaces the per-resource Go files in duplosdk/ — one spec file is all
	// that is needed to add a new resource.
	Endpoint EndpointSpec `json:"endpoint"`

	// Attributes describes every schema attribute.
	Attributes []AttributeSpec `json:"attributes"`

	// RequestConstants are fixed fields injected into every request body —
	// envelope/discriminator values the API requires that are not schema
	// attributes.
	RequestConstants []ConstantField `json:"requestConstants,omitempty"`

	// CreateConstants / UpdateConstants inject fixed fields into the POST
	// (create) or PUT (update) body only. Each overrides RequestConstants for
	// its verb when both set the same path.
	CreateConstants []ConstantField `json:"createConstants,omitempty"`
	UpdateConstants []ConstantField `json:"updateConstants,omitempty"`

	// RequiredIf declares conditional-required rules evaluated at plan time.
	RequiredIf []RequiredIfRule `json:"requiredIf,omitempty"`

	// ConflictsWith declares mutually-exclusive attribute groups: within each
	// group at most one attribute may be set. Enforced at plan time, e.g.
	// [["snapshot_name", "snapshot_arns"]].
	ConflictsWith [][]string `json:"conflictsWith,omitempty"`

	// Waiter, when present, makes Create/Update poll until the resource
	// reaches a terminal state.
	Waiter *WaiterSpec `json:"waiter,omitempty"`

	// ReadAfterWrite, when true and there is no Waiter, makes Create/Update
	// issue a follow-up GET and build state from that read response rather than
	// from the POST/PUT response. Use it when the write response differs from the
	// canonical read — e.g. the API encrypts a field on save (so the create
	// response carries ciphertext) but decrypts it on read. Without this, such a
	// field trips Terraform's "inconsistent result after apply". (Waiter resources
	// already read back via polling, so this is ignored when a Waiter is set.)
	ReadAfterWrite bool `json:"readAfterWrite,omitempty"`

	// DataSource, when true, registers a read-only data source (data.duploai_<name>)
	// from this spec in addition to the managed resource. The data source schema is
	// derived automatically: path-parameter attributes stay Required, all other
	// readable attributes become Computed, and write-only attributes (those with no
	// apiPath/responsePath) are excluded entirely.
	DataSource bool `json:"dataSource,omitempty"`

	// DataSourceOnly, when true, registers ONLY a read-only data source
	// (data.duploai_<name>) from this spec — no managed resource is registered.
	// Use this for APIs that are purely read-only and have no create/update/delete
	// operations. Implies DataSource semantics; DataSource need not also be set.
	DataSourceOnly bool `json:"dataSourceOnly,omitempty"`

	// SingleIntentUpdate configures the "one update intent per call" flow used by
	// APIs (e.g. AWS MSK) that accept a single discrete update at a time —
	// identified by a discriminator field plus one target value — and require the
	// resource to reach a ready state between updates. When set, attributes that
	// declare an UpdateIntent are mutated in place: for each changed one the engine
	// waits for the ready state, issues a PUT carrying only the discriminator and
	// that attribute's target value, then waits for the ready state again before
	// the next. Attributes without an UpdateIntent should be forceNew/createOnly.
	SingleIntentUpdate *SingleIntentUpdateSpec `json:"singleIntentUpdate,omitempty"`
}

// SingleIntentUpdateSpec configures resource-level single-intent updates.
type SingleIntentUpdateSpec struct {
	// DiscriminatorPath is the request body dot-path that names which update is
	// being performed, e.g. "spec.updateRequest.updateType".
	DiscriminatorPath string `json:"discriminatorPath"`
	// ReadyPath is the read-response dot-path whose value must equal ReadyState
	// before and after each update, e.g. "result.cloudDetails.state".
	ReadyPath string `json:"readyPath"`
	// ReadyState is the terminal-ready value at ReadyPath, e.g. "ACTIVE".
	ReadyState string `json:"readyState"`
	// ReadyFailureStates maps values at ReadyPath that mean the update failed (so
	// the wait aborts immediately instead of polling to timeout) to a reason,
	// e.g. {"FAILED": "cluster entered a FAILED state"}.
	ReadyFailureStates map[string]string `json:"readyFailureStates,omitempty"`
}

// UpdateIntentSpec maps one mutable attribute to a single-intent update: when the
// attribute changes, the engine sends DiscriminatorValue at the resource's
// DiscriminatorPath and the new value at ValuePath.
type UpdateIntentSpec struct {
	// DiscriminatorValue is the discriminator value for this attribute's update,
	// e.g. "BrokerCount".
	DiscriminatorValue string `json:"discriminatorValue"`
	// ValuePath is the request body dot-path the new value is written to,
	// e.g. "spec.updateRequest.targetNumberOfBrokerNodes".
	ValuePath string `json:"valuePath"`
}

// OperationSpec overrides the HTTP verb and/or path for one CRUD operation.
// Both fields default to the operation's conventional REST value when omitted.
type OperationSpec struct {
	Verb string `json:"verb,omitempty"`
	Path string `json:"path,omitempty"`

	// SkipWhen applies to the deprovision operation only: when all listed
	// conditions match the prior state (logical AND), the engine skips the
	// pre-delete deprovision step and issues the delete directly. Use for modes
	// with no cloud infrastructure to tear down (e.g. cloud=K8S_ONLY, which
	// registers an existing cluster and provisions nothing). Ignored on
	// create/read/update/delete.
	SkipWhen []RequiredIfCondition `json:"skipWhen,omitempty"`
}

// EndpointSpec configures the API URIs and HTTP verbs for a resource's CRUD
// operations. It lives in the spec JSON alongside the Terraform schema so that
// adding a new resource requires only one file.
type EndpointSpec struct {
	// UriBase is required. Common path prefix for all operations with
	// {placeholder} tokens for path parameters, e.g.
	//   /v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/Plans
	UriBase string `json:"uriBase"`

	// Immutable marks resources that have no in-place Update operation.
	// Any change to a non-computed attribute forces resource replacement.
	// Defaults to false (standard PUT /{id} update).
	Immutable bool `json:"immutable,omitempty"`

	// Create/Read/Update/Delete override the default verb and path for each
	// operation. Leave nil to use the REST conventions (POST/"", GET/PUT/DELETE /{id}).
	Create *OperationSpec `json:"create,omitempty"`
	Read   *OperationSpec `json:"read,omitempty"`
	Update *OperationSpec `json:"update,omitempty"`
	Delete *OperationSpec `json:"delete,omitempty"`

	// Deprovision, when non-nil, adds a pre-delete teardown step.
	// Defaults to POST /{id}/deprovision; set verb/path only to override.
	Deprovision *OperationSpec `json:"deprovision,omitempty"`
}

// BuildEndpoint converts this spec's EndpointSpec into a duplosdk.Endpoint
// ready for use by the dynamic resource engine.
func (s *ResourceSpec) BuildEndpoint() (duplosdk.Endpoint, error) {
	ep := s.Endpoint
	if ep.UriBase == "" {
		return duplosdk.Endpoint{}, fmt.Errorf("endpoint.uriBase is required")
	}
	result := duplosdk.Endpoint{UriBase: ep.UriBase}
	if ep.Create != nil {
		result.Create = duplosdk.Operation{Verb: ep.Create.Verb, Path: ep.Create.Path}
	}
	if ep.Read != nil {
		result.Read = duplosdk.Operation{Verb: ep.Read.Verb, Path: ep.Read.Path}
	}
	if !ep.Immutable {
		// Endpoint.HasUpdate() checks Update != Operation{}, so the value must
		// be non-zero. Start with the REST convention and apply any overrides.
		update := duplosdk.Operation{Verb: "PUT", Path: "/{id}"}
		if ep.Update != nil {
			if ep.Update.Verb != "" {
				update.Verb = ep.Update.Verb
			}
			if ep.Update.Path != "" {
				update.Path = ep.Update.Path
			}
		}
		result.Update = update
	}
	if ep.Delete != nil {
		result.Delete = duplosdk.Operation{Verb: ep.Delete.Verb, Path: ep.Delete.Path}
	}
	if ep.Deprovision != nil {
		path := ep.Deprovision.Path
		if path == "" {
			path = "/{id}/deprovision"
		}
		verb := ep.Deprovision.Verb
		if verb == "" {
			verb = "POST"
		}
		result.Deprovision = duplosdk.Operation{Verb: verb, Path: path}
	}
	return result, nil
}

// AttributeSpec describes one schema attribute and how it maps to the API.
type AttributeSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Deprecated, when set, marks the attribute deprecated: the message is wired
	// to the framework's DeprecationMessage and shown as a warning whenever the
	// attribute is set in config. Use when renaming an attribute — keep the old
	// one with a deprecation message pointing at the replacement.
	Deprecated string `json:"deprecated,omitempty"`

	// Type is one of: string, bool, int, list(string).
	Type string `json:"type"`

	Required  bool `json:"required,omitempty"`
	Optional  bool `json:"optional,omitempty"`
	Computed  bool `json:"computed,omitempty"`
	Sensitive bool `json:"sensitive,omitempty"`

	// ForceNew marks the attribute as RequiresReplace.
	ForceNew bool `json:"forceNew,omitempty"`

	// Stable marks a computed-only attribute (Computed but not Optional or
	// ForceNew) whose value is assigned once at creation and never changes
	// afterward — e.g. the resource's own database ID, or a cloud identifier
	// like a stack ID or security group ID. Without this flag, computed-only
	// attributes are deliberately left out of UseStateForUnknown (see
	// useStateForUnknown in typesystem.go) because most of them — status,
	// load_balancer, etc. — legitimately change between applies and must stay
	// recomputed. Stable opts a specific attribute back into preserving its
	// prior value, so it does not spuriously show "(known after apply)" (and
	// ripple into dependent resources) every time an unrelated attribute on
	// the same resource changes.
	Stable bool `json:"stable,omitempty"`

	// Default supplies a static default for Optional+Computed attributes.
	// Interpreted according to Type.
	Default *json.RawMessage `json:"default,omitempty"`

	// OneOf constrains a string attribute to an enumerated set.
	OneOf []string `json:"oneOf,omitempty"`

	// MinItems, when > 0, requires a list/set attribute to have at least this
	// many elements (validated at plan time). Use for collections the API
	// rejects when empty, e.g. a permission set's allowed_workspaces.
	MinItems int `json:"minItems,omitempty"`

	// Min / Max bound a numeric attribute (int or number) inclusively, validated
	// at plan time. Use for ranges the API enforces, e.g. limit_usd >= 0.01.
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`

	// APIPath is the default dot-path in the API body this attribute maps to,
	// e.g. "spec.region". Array element extraction is supported for read-back
	// via "result.subnets[].subnetId". Used for both directions unless
	// overridden below.
	//
	// Defaulting differs by level on purpose: a TOP-LEVEL attribute with an
	// empty path is treated as non-API (not sent, not read back) — this is how
	// path-parameter attributes like workspace_id are excluded from the body. A
	// NESTED field with an empty path defaults to its own name. So a top-level
	// body field must declare its apiPath explicitly (even apiPath:"name"),
	// while an identical nested field does not.
	APIPath string `json:"apiPath,omitempty"`

	// RequestPath / ResponsePath override APIPath when the request and response
	// models place the same value at different paths — e.g. send to
	// "spec.region" but read back from "configuration.region". Each falls back
	// to APIPath when empty.
	RequestPath  string `json:"requestPath,omitempty"`
	ResponsePath string `json:"responsePath,omitempty"`

	// ResponsePaths is an ordered list of response dot-paths for a read-only
	// (computed) attribute; on read the engine stores the value at the FIRST path
	// that yields a non-null, non-empty result. Use for a cloud-agnostic output
	// whose value lives at different paths per cloud (e.g. an EKS cluster id at
	// result.clusterArn but an AKS cluster id at result.azure.clusterId) so a
	// single attribute is populated regardless of cloud. Takes precedence over
	// apiPath/responsePath for the read direction; intended for computed-only
	// attributes (it is not sent in any request).
	ResponsePaths []string `json:"responsePaths,omitempty"`

	// CreatePath / UpdatePath override RequestPath (and APIPath) for the POST
	// and PUT bodies respectively. Use when the API uses different DTOs for
	// create vs update (e.g. spec.createRequest vs spec.updateRequest). Each
	// falls back to RequestPath, then APIPath, when empty.
	CreatePath string `json:"createPath,omitempty"`
	UpdatePath string `json:"updatePath,omitempty"`

	// UpdateBoolTrueValue, for a string attribute, makes the update (PUT) body
	// carry a BOOLEAN at UpdatePath instead of the string: true when the value
	// equals UpdateBoolTrueValue, else false. Use when create and update take
	// different shapes for the same setting — e.g. AWS ECR create takes
	// imageTagMutability="IMMUTABLE" (string) while update takes
	// enableTagImmutability=true (bool). Only applies to the update body (create
	// still writes the raw string via CreatePath). Requires UpdatePath.
	UpdateBoolTrueValue string `json:"updateBoolTrueValue,omitempty"`

	// CreateOnly marks an attribute that is only sent in the POST (create)
	// body and never in the PUT (update) body. Useful for fields that are
	// immutable after creation but not forceNew (e.g. code source for Lambda).
	CreateOnly bool `json:"createOnly,omitempty"`

	// NoSend marks an attribute that maps from the response but is never sent in
	// the request (computed-only fields like status, vpc_id).
	NoSend bool `json:"noSend,omitempty"`

	// FilterResponseKeys, for a map(string) attribute (including one nested inside
	// an object, e.g. azure.tags), drops matching keys from the response map before
	// it is stored in state. Each pattern is an exact key, or a prefix when it ends
	// in "*" (e.g. "duplocloud-ai-*"). Use when the backend injects its own entries
	// into a map the user only partially manages (e.g. the ALB controller adds
	// alb.ingress.kubernetes.io/{security-groups,subnets,...} annotations, or the
	// platform stamps managed duplocloud-ai-* tags), which would otherwise show
	// perpetual drift as Terraform tries to remove the server-added keys. Keys the
	// user sets (not matched here) are preserved, so there is no "cannot remove a
	// key" limitation for those.
	FilterResponseKeys []string `json:"filterResponseKeys,omitempty"`

	// NormalizeCsvOrder, for a string attribute, sorts the comma-separated tokens
	// of the response value into a canonical (lexical) order before storing it in
	// state. Use for backend fields whose elements are order-insensitive but
	// returned in a non-deterministic order (e.g. AWS MSK bootstrap broker
	// strings), which would otherwise show perpetual drift on refresh.
	NormalizeCsvOrder bool `json:"normalizeCsvOrder,omitempty"`

	// NormalizeVersion, for a string attribute, truncates the response value to
	// its major.minor components (first two dot-separated parts) before storing
	// it in state. Use for a Kubernetes/semver version the user specifies at
	// minor precision (e.g. "1.35") but the backend resolves to a patch version
	// (e.g. AKS returns "1.35.6"), which would otherwise show perpetual drift —
	// and on a forceNew field, forced replacement. Values with two or fewer
	// components (e.g. EKS "1.34") are returned unchanged.
	NormalizeVersion bool `json:"normalizeVersion,omitempty"`

	// OrderByKey, for a list(object) attribute, names a nested string attribute
	// whose value is used to sort the list into a canonical (lexical) order. The
	// engine sorts both the planned config (via a plan modifier) and the API
	// response (before storing state), so a backend that returns the elements in a
	// different order than the user declared them does not show order-only drift.
	// Use for order-insensitive collections that have a natural unique key (e.g.
	// Azure network subnets/NAT gateways/NSG rules keyed by name). The named
	// nested attribute must exist and be of type string.
	OrderByKey string `json:"orderByKey,omitempty"`

	// UpdateIntent, when set (and the resource has SingleIntentUpdate), makes this
	// attribute mutable via a single-intent update: a change issues a dedicated
	// PUT carrying the discriminator + this attribute's new value. Requires the
	// resource-level SingleIntentUpdate config.
	UpdateIntent *UpdateIntentSpec `json:"updateIntent,omitempty"`

	// Attributes holds the nested fields when Type is an object form
	// ("object", "list(object)", "set(object)", "map(object)"). Each nested
	// field is itself an AttributeSpec, so objects nest to any depth. A nested
	// field's apiPath/requestPath/responsePath is relative to its parent
	// object; an empty path defaults to the field name.
	Attributes []AttributeSpec `json:"attributes,omitempty"`

	// PreserveUnmanagedInto pairs this writable collection attribute with a
	// computed sibling attribute (named here) so that entries the server adds
	// out-of-band survive Terraform's full-document updates. On READ, ids the
	// API returns that are NOT in this attribute's managed set are routed into
	// the sibling; on WRITE, the request sends the UNION of this attribute and
	// the sibling, so server-owned entries (e.g. a Kubernetes scope the backend
	// attaches when a cluster is created) are not cleared. Only valid on a
	// top-level set(string)/list(string). The named sibling must be a
	// computed + noSend attribute of the same type with no apiPath.
	PreserveUnmanagedInto string `json:"preserveUnmanagedInto,omitempty"`

	// preserveTarget is set internally (not from JSON) when another attribute
	// names this one via PreserveUnmanagedInto. It makes the computed sibling
	// adopt UseStateForUnknown so its prior-state value is known at plan time —
	// required for the union-on-write to keep server-managed entries.
	preserveTarget bool
}

// requestPath / responsePath resolve the effective per-direction path.
func (a AttributeSpec) requestPath() string {
	if a.RequestPath != "" {
		return a.RequestPath
	}
	return a.APIPath
}

func (a AttributeSpec) responsePath() string {
	if a.ResponsePath != "" {
		return a.ResponsePath
	}
	return a.APIPath
}

// responsePathList returns the ordered read-direction paths for an attribute:
// the explicit ResponsePaths fallback list when set, otherwise the single
// resolved responsePath (empty slice when neither is present).
func (a AttributeSpec) responsePathList() []string {
	if len(a.ResponsePaths) > 0 {
		return a.ResponsePaths
	}
	if p := a.responsePath(); p != "" {
		return []string{p}
	}
	return nil
}

// effectiveCreatePath resolves the path used in POST (create) bodies.
// Priority: createPath > requestPath > apiPath.
func (a AttributeSpec) effectiveCreatePath() string {
	if a.CreatePath != "" {
		return a.CreatePath
	}
	return a.requestPath()
}

// effectiveUpdatePath resolves the path used in PUT (update) bodies.
// Returns "" for createOnly fields so they are skipped on update.
// Priority: updatePath > requestPath > apiPath.
func (a AttributeSpec) effectiveUpdatePath() string {
	if a.CreateOnly {
		return ""
	}
	if a.UpdatePath != "" {
		return a.UpdatePath
	}
	return a.requestPath()
}

// ConstantField injects a fixed value into every request body at the given
// dot-path. Use for envelope/discriminator fields the API requires but that are
// not user-configurable schema attributes, e.g. {"path": "kind", "value": "Network"}.
type ConstantField struct {
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// RequiredIfRule requires Attribute to be set when its condition holds. Two
// forms:
//   - Single (back-compat): Attribute is required when WhenAttribute == WhenEquals.
//   - Compound: Attribute is required when ALL of When match (logical AND); each
//     condition is equals or notEquals. Use this for rules like "required when
//     engine != Memcached AND cluster_mode == Enabled".
//
// A condition reads the config value, falling back to the attribute's spec
// default when the user omitted it — so a condition on a defaulted field (e.g.
// cluster_mode defaulting to "Disabled") still evaluates correctly.
type RequiredIfRule struct {
	Attribute     string                `json:"attribute"`
	WhenAttribute string                `json:"whenAttribute,omitempty"`
	WhenEquals    string                `json:"whenEquals,omitempty"`
	When          []RequiredIfCondition `json:"when,omitempty"`
}

// RequiredIfCondition is one term of a compound requiredIf rule. Exactly one of
// Equals / NotEquals / IsEmpty is set. IsEmpty matches when the (config-or-
// default) value is empty — used for "required unless X is set" rules, e.g.
// engine_version required when snapshot_name is empty.
type RequiredIfCondition struct {
	Attribute string `json:"attribute"`
	Equals    string `json:"equals,omitempty"`
	NotEquals string `json:"notEquals,omitempty"`
	IsEmpty   bool   `json:"isEmpty,omitempty"`
}

// conditions normalizes a rule to its list of AND-ed conditions (the single
// WhenAttribute/WhenEquals form becomes one condition).
func (r RequiredIfRule) conditions() []RequiredIfCondition {
	if len(r.When) > 0 {
		return r.When
	}
	return []RequiredIfCondition{{Attribute: r.WhenAttribute, Equals: r.WhenEquals}}
}

// WaiterSpec drives the generic poller.
type WaiterSpec struct {
	// StatusPath is the dot-path to the status string in the read response.
	StatusPath string `json:"statusPath"`
	// SuccessState is the terminal success value.
	SuccessState string `json:"successState"`
	// ReadyPath / ReadyState add an optional secondary success gate: the resource
	// is only considered provisioned once the status reaches SuccessState AND the
	// value at ReadyPath equals ReadyState. Use for resources whose status flips
	// to "complete" before a downstream signal is ready (e.g. an EC2 host whose
	// status is Complete but live_state is not yet "running"). Both must be set
	// to enable the gate; failure detection still uses StatusPath/FailureStates.
	ReadyPath  string `json:"readyPath,omitempty"`
	ReadyState string `json:"readyState,omitempty"`
	// PopulatedPath / PopulatedPathAttribute add an optional, user-gated wait: the
	// engine adds a boolean control attribute named PopulatedPathAttribute (e.g.
	// "wait_for_load_balancer") to the resource. When the user sets it true, Create
	// and Update keep polling — after the normal status/ready gate is met — until
	// the value at PopulatedPath (a read-response dot-path, e.g.
	// "result.k8sResource.status.loadBalancer.ingress") becomes non-empty, or the
	// operation timeout elapses. Used for async cloud side effects the API fills in
	// after the resource itself is Complete (e.g. a load balancer address assigned
	// by the cloud controller). Ignored when the control attribute is false/unset.
	// Both fields must be set together.
	PopulatedPath          string `json:"populatedPath,omitempty"`
	PopulatedPathAttribute string `json:"populatedPathAttribute,omitempty"`
	// FailureStates maps terminal failure values to human-readable reasons.
	FailureStates map[string]string `json:"failureStates"`
	// FailureRetries is how many extra polls to tolerate after first seeing a
	// failure state before treating it as terminal. Use it for backends that
	// report a transient failure mid-provisioning (a first attempt fails, the
	// worker retries) and then recover. Defaults to 0 (abort on first failure).
	FailureRetries int `json:"failureRetries,omitempty"`
	// DeprovisionedState is the terminal status reached after a deprovision step
	// completes (e.g. "DeProvisioned"). Required only for resources whose
	// endpoint declares a Deprovision operation; the delete flow waits for this
	// state before issuing the final delete call.
	DeprovisionedState string `json:"deprovisionedState,omitempty"`
	// FailureDetailPath optionally points to a field with extra error context.
	FailureDetailPath string `json:"failureDetailPath,omitempty"`
	// PollIntervalSeconds defaults to 10 when zero.
	PollIntervalSeconds int `json:"pollIntervalSeconds,omitempty"`
	// FailurePollIntervalSeconds overrides PollIntervalSeconds when the resource
	// is in a failure state during a retry. Use a longer value to give the
	// backend more time to self-recover between polls. Falls back to
	// PollIntervalSeconds when zero.
	FailurePollIntervalSeconds int `json:"failurePollIntervalSeconds,omitempty"`
	// CreateTimeoutMinutes / UpdateTimeoutMinutes / DeleteTimeoutMinutes
	// supply default operation timeouts (overridable via the timeouts block).
	CreateTimeoutMinutes int `json:"createTimeoutMinutes,omitempty"`
	UpdateTimeoutMinutes int `json:"updateTimeoutMinutes,omitempty"`
	DeleteTimeoutMinutes int `json:"deleteTimeoutMinutes,omitempty"`
}

// defaultWaiterSpec returns the waiter defaults shared by all DuploAI resources.
// A spec's "waiter" block needs only the fields that differ from these values.
func defaultWaiterSpec() WaiterSpec {
	return WaiterSpec{
		StatusPath:   "status",
		SuccessState: "Complete",
		FailureStates: map[string]string{
			"Failed":             "provisioning failed",
			"Blocked":            "provisioning is blocked",
			"WaitingForApproval": "provisioning is waiting for manual approval, which Terraform cannot provide",
			"DeprovisionFailed":  "deprovisioning failed",
		},
		FailureDetailPath:    "blockedReason",
		PollIntervalSeconds:  10,
		CreateTimeoutMinutes: 30,
		UpdateTimeoutMinutes: 30,
		DeleteTimeoutMinutes: 15,
	}
}

// applyWaiterDefaults fills zero-value WaiterSpec fields from the shared defaults.
// Per-spec fields (deprovisionedState, failureRetries) are left unchanged.
func applyWaiterDefaults(w *WaiterSpec) {
	d := defaultWaiterSpec()
	if w.StatusPath == "" {
		w.StatusPath = d.StatusPath
	}
	if w.SuccessState == "" {
		w.SuccessState = d.SuccessState
	}
	if len(w.FailureStates) == 0 {
		w.FailureStates = d.FailureStates
	}
	if w.FailureDetailPath == "" {
		w.FailureDetailPath = d.FailureDetailPath
	}
	if w.PollIntervalSeconds == 0 {
		w.PollIntervalSeconds = d.PollIntervalSeconds
	}
	if w.CreateTimeoutMinutes == 0 {
		w.CreateTimeoutMinutes = d.CreateTimeoutMinutes
	}
	if w.UpdateTimeoutMinutes == 0 {
		w.UpdateTimeoutMinutes = d.UpdateTimeoutMinutes
	}
	if w.DeleteTimeoutMinutes == 0 {
		w.DeleteTimeoutMinutes = d.DeleteTimeoutMinutes
	}
}

// loadResourceSpecs reads and validates every embedded JSON spec, sorted by
// name for deterministic resource registration order.
func loadResourceSpecs() ([]ResourceSpec, error) {
	entries, err := fs.ReadDir(specFS, "specs")
	if err != nil {
		return nil, fmt.Errorf("reading embedded specs: %w", err)
	}
	specs := make([]ResourceSpec, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, readErr := specFS.ReadFile("specs/" + e.Name())
		if readErr != nil {
			return nil, fmt.Errorf("reading spec %s: %w", e.Name(), readErr)
		}
		var spec ResourceSpec
		if jsonErr := json.Unmarshal(data, &spec); jsonErr != nil {
			return nil, fmt.Errorf("parsing spec %s: %w", e.Name(), jsonErr)
		}
		if validErr := spec.validate(); validErr != nil {
			return nil, fmt.Errorf("invalid spec %s: %w", e.Name(), validErr)
		}
		markPreserveTargets(spec.Attributes)
		if spec.Waiter != nil {
			applyWaiterDefaults(spec.Waiter)
		}
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs, nil
}

// validate checks a spec for internal consistency before it is used to build a
// resource. Failing fast at startup beats a confusing runtime panic.
func (s *ResourceSpec) validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if s.IDPath == "" {
		return fmt.Errorf("idPath is required")
	}
	if s.Endpoint.UriBase == "" {
		return fmt.Errorf("endpoint.uriBase is required")
	}
	seen, err := validateAttributes(s.Attributes)
	if err != nil {
		return err
	}
	if seen["id"] {
		// id is reserved and injected by the engine.
		return fmt.Errorf("attribute %q is reserved", "id")
	}
	for _, r := range s.RequiredIf {
		if !seen[r.Attribute] {
			return fmt.Errorf("requiredIf references unknown attribute %q", r.Attribute)
		}
		conds := r.conditions()
		if len(conds) == 0 {
			return fmt.Errorf("requiredIf rule for %q has no condition", r.Attribute)
		}
		for _, c := range conds {
			if !seen[c.Attribute] {
				return fmt.Errorf("requiredIf references unknown attribute %q", c.Attribute)
			}
			ops := 0
			if c.Equals != "" {
				ops++
			}
			if c.NotEquals != "" {
				ops++
			}
			if c.IsEmpty {
				ops++
			}
			if ops != 1 {
				return fmt.Errorf("requiredIf condition on %q must set exactly one of equals/notEquals/isEmpty", c.Attribute)
			}
		}
	}
	for _, group := range s.ConflictsWith {
		if len(group) < 2 {
			return fmt.Errorf("conflictsWith group must list at least two attributes")
		}
		for _, name := range group {
			if !seen[name] {
				return fmt.Errorf("conflictsWith references unknown attribute %q", name)
			}
		}
	}
	if err := validatePreservePairs(s.Attributes); err != nil {
		return err
	}
	if err := validateSingleIntentUpdate(s); err != nil {
		return err
	}
	if s.Waiter != nil {
		pp, pa := s.Waiter.PopulatedPath, s.Waiter.PopulatedPathAttribute
		if (pp == "") != (pa == "") {
			return fmt.Errorf("waiter.populatedPath and waiter.populatedPathAttribute must be set together")
		}
		if pa != "" {
			if pa == "id" || pa == "failure_retries" || pa == "timeouts" {
				return fmt.Errorf("waiter.populatedPathAttribute %q collides with a reserved attribute", pa)
			}
			if seen[pa] {
				return fmt.Errorf("waiter.populatedPathAttribute %q collides with a declared attribute", pa)
			}
		}
	}
	return nil
}

// validateSingleIntentUpdate checks the single-intent update configuration: any
// attribute with an UpdateIntent requires the resource-level SingleIntentUpdate
// block (with all three paths set) and a complete UpdateIntent.
func validateSingleIntentUpdate(s *ResourceSpec) error {
	var intents int
	for _, a := range s.Attributes {
		if a.UpdateIntent == nil {
			continue
		}
		intents++
		if a.UpdateIntent.DiscriminatorValue == "" || a.UpdateIntent.ValuePath == "" {
			return fmt.Errorf("attribute %q: updateIntent requires discriminatorValue and valuePath", a.Name)
		}
	}
	if s.SingleIntentUpdate != nil {
		si := s.SingleIntentUpdate
		if si.DiscriminatorPath == "" || si.ReadyPath == "" || si.ReadyState == "" {
			return fmt.Errorf("singleIntentUpdate requires discriminatorPath, readyPath, and readyState")
		}
	} else if intents > 0 {
		return fmt.Errorf("updateIntent on an attribute requires a resource-level singleIntentUpdate block")
	}
	return nil
}

// validatePreservePairs checks every PreserveUnmanagedInto pairing: the source
// must be a top-level string set/list, and the named sibling must exist as a
// computed + noSend attribute of the same type with no apiPath (the engine
// populates it on read and merges it on write, so it is never sent or path-mapped
// on its own).
func validatePreservePairs(attrs []AttributeSpec) error {
	byName := make(map[string]AttributeSpec, len(attrs))
	for _, a := range attrs {
		byName[a.Name] = a
	}
	for _, a := range attrs {
		if a.PreserveUnmanagedInto == "" {
			continue
		}
		if a.Type != "set(string)" && a.Type != "list(string)" {
			return fmt.Errorf("attribute %q: preserveUnmanagedInto is only valid on set(string)/list(string)", a.Name)
		}
		sib, ok := byName[a.PreserveUnmanagedInto]
		if !ok {
			return fmt.Errorf("attribute %q: preserveUnmanagedInto references unknown attribute %q", a.Name, a.PreserveUnmanagedInto)
		}
		if sib.Type != a.Type {
			return fmt.Errorf("attribute %q: preserveUnmanagedInto sibling %q must have the same type (%s)", a.Name, sib.Name, a.Type)
		}
		if !sib.Computed || sib.Required || sib.Optional {
			return fmt.Errorf("attribute %q: preserveUnmanagedInto sibling %q must be computed-only", a.Name, sib.Name)
		}
		if !sib.NoSend {
			return fmt.Errorf("attribute %q: preserveUnmanagedInto sibling %q must set noSend (the engine merges it on write)", a.Name, sib.Name)
		}
		if sib.APIPath != "" || sib.ResponsePath != "" {
			return fmt.Errorf("attribute %q: preserveUnmanagedInto sibling %q must not set apiPath/responsePath (the engine populates it)", a.Name, sib.Name)
		}
	}
	return nil
}

// markPreserveTargets flags each attribute named by a PreserveUnmanagedInto so
// the computed sibling adopts UseStateForUnknown. Must run after validate.
func markPreserveTargets(attrs []AttributeSpec) {
	targets := map[string]bool{}
	for _, a := range attrs {
		if a.PreserveUnmanagedInto != "" {
			targets[a.PreserveUnmanagedInto] = true
		}
	}
	for i := range attrs {
		if targets[attrs[i].Name] {
			attrs[i].preserveTarget = true
		}
	}
}

// validateAttributes checks a list of (possibly nested) attributes and returns
// the set of names seen at this level. It recurses into object attributes.
func validateAttributes(attrs []AttributeSpec) (map[string]bool, error) {
	seen := map[string]bool{}
	for _, a := range attrs {
		if a.Name == "" {
			return nil, fmt.Errorf("attribute with empty name")
		}
		if seen[a.Name] {
			return nil, fmt.Errorf("duplicate attribute %q", a.Name)
		}
		seen[a.Name] = true

		info, err := parseType(a.Type)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", a.Name, err)
		}
		if !a.Required && !a.Optional && !a.Computed {
			return nil, fmt.Errorf("attribute %q must be one of required/optional/computed", a.Name)
		}
		if a.UpdateBoolTrueValue != "" {
			if a.Type != "string" {
				return nil, fmt.Errorf("attribute %q: updateBoolTrueValue requires a string type", a.Name)
			}
			if a.UpdatePath == "" {
				return nil, fmt.Errorf("attribute %q: updateBoolTrueValue requires updatePath", a.Name)
			}
		}
		if a.OrderByKey != "" {
			if a.Type != "list(object)" {
				return nil, fmt.Errorf("attribute %q: orderByKey is only valid on list(object)", a.Name)
			}
			var keyAttr *AttributeSpec
			for i := range a.Attributes {
				if a.Attributes[i].Name == a.OrderByKey {
					keyAttr = &a.Attributes[i]
					break
				}
			}
			if keyAttr == nil {
				return nil, fmt.Errorf("attribute %q: orderByKey references unknown nested attribute %q", a.Name, a.OrderByKey)
			}
			if keyAttr.Type != "string" {
				return nil, fmt.Errorf("attribute %q: orderByKey nested attribute %q must be a string", a.Name, a.OrderByKey)
			}
		}
		if info.elem == "object" {
			if len(a.Attributes) == 0 {
				return nil, fmt.Errorf("object attribute %q requires nested attributes", a.Name)
			}
			if _, err := validateAttributes(a.Attributes); err != nil {
				return nil, err
			}
		} else if len(a.Attributes) > 0 {
			return nil, fmt.Errorf("attribute %q has nested attributes but is not an object type", a.Name)
		}
	}
	return seen, nil
}

// checkPathParams verifies every path parameter the resource's endpoint
// requires maps to a string attribute in the spec. Called at startup once the
// endpoint is known, so a path/schema mismatch fails loudly before serving.
func (s *ResourceSpec) checkPathParams(params []string) error {
	for _, p := range params {
		a := s.attr(p)
		if a == nil {
			return fmt.Errorf("endpoint path references unknown attribute {%s}", p)
		}
		if a.Type != "string" {
			return fmt.Errorf("path parameter {%s} must be a string attribute", p)
		}
	}
	return nil
}
