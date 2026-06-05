package duplocloud

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
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
	//
	// The API URIs and verbs are NOT configured here — they live in the
	// resource's Endpoint, registered by name in the duplosdk package.
	IDPath string `json:"idPath"`

	// Attributes describes every schema attribute.
	Attributes []AttributeSpec `json:"attributes"`

	// RequestConstants are fixed fields injected into every request body —
	// envelope/discriminator values the API requires that are not schema
	// attributes.
	RequestConstants []ConstantField `json:"requestConstants,omitempty"`

	// RequiredIf declares conditional-required rules evaluated at plan time.
	RequiredIf []RequiredIfRule `json:"requiredIf,omitempty"`

	// Waiter, when present, makes Create/Update poll until the resource
	// reaches a terminal state.
	Waiter *WaiterSpec `json:"waiter,omitempty"`
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
