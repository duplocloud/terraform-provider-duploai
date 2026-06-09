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

	// Waiter, when present, makes Create/Update poll until the resource
	// reaches a terminal state.
	Waiter *WaiterSpec `json:"waiter,omitempty"`
}

// OperationSpec overrides the HTTP verb and/or path for one CRUD operation.
// Both fields default to the operation's conventional REST value when omitted.
type OperationSpec struct {
	Verb string `json:"verb,omitempty"`
	Path string `json:"path,omitempty"`
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

	// Type is one of: string, bool, int, list(string).
	Type string `json:"type"`

	Required  bool `json:"required,omitempty"`
	Optional  bool `json:"optional,omitempty"`
	Computed  bool `json:"computed,omitempty"`
	Sensitive bool `json:"sensitive,omitempty"`

	// ForceNew marks the attribute as RequiresReplace.
	ForceNew bool `json:"forceNew,omitempty"`

	// Default supplies a static default for Optional+Computed attributes.
	// Interpreted according to Type.
	Default *json.RawMessage `json:"default,omitempty"`

	// OneOf constrains a string attribute to an enumerated set.
	OneOf []string `json:"oneOf,omitempty"`

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

	// CreatePath / UpdatePath override RequestPath (and APIPath) for the POST
	// and PUT bodies respectively. Use when the API uses different DTOs for
	// create vs update (e.g. spec.createRequest vs spec.updateRequest). Each
	// falls back to RequestPath, then APIPath, when empty.
	CreatePath string `json:"createPath,omitempty"`
	UpdatePath string `json:"updatePath,omitempty"`

	// CreateOnly marks an attribute that is only sent in the POST (create)
	// body and never in the PUT (update) body. Useful for fields that are
	// immutable after creation but not forceNew (e.g. code source for Lambda).
	CreateOnly bool `json:"createOnly,omitempty"`

	// NoSend marks an attribute that maps from the response but is never sent in
	// the request (computed-only fields like status, vpc_id).
	NoSend bool `json:"noSend,omitempty"`

	// Attributes holds the nested fields when Type is an object form
	// ("object", "list(object)", "set(object)", "map(object)"). Each nested
	// field is itself an AttributeSpec, so objects nest to any depth. A nested
	// field's apiPath/requestPath/responsePath is relative to its parent
	// object; an empty path defaults to the field name.
	Attributes []AttributeSpec `json:"attributes,omitempty"`
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

// RequiredIfRule requires Attribute to be set when WhenAttribute equals WhenValue.
type RequiredIfRule struct {
	Attribute     string `json:"attribute"`
	WhenAttribute string `json:"whenAttribute"`
	WhenEquals    string `json:"whenEquals"`
}

// WaiterSpec drives the generic poller.
type WaiterSpec struct {
	// StatusPath is the dot-path to the status string in the read response.
	StatusPath string `json:"statusPath"`
	// SuccessState is the terminal success value.
	SuccessState string `json:"successState"`
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
		if !seen[r.Attribute] || !seen[r.WhenAttribute] {
			return fmt.Errorf("requiredIf references unknown attribute")
		}
	}
	return nil
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
