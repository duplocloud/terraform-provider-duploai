package duplocloud

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

// A computed+forceNew attribute must get UseStateForUnknown so an unchanged
// auto-populated value (e.g. region/vpc_id from a linked network) does not go
// unknown and spuriously force replacement. A pure-output computed attribute
// (e.g. status) must NOT get it — it has to recompute each apply.
func TestAttrSchema_UseStateForUnknownPlanModifier(t *testing.T) {
	cases := []struct {
		name string
		a    AttributeSpec
		want int // number of plan modifiers
	}{
		{"optional+computed+forceNew string", AttributeSpec{Type: "string", Optional: true, Computed: true, ForceNew: true}, 2}, // UseStateForUnknown + RequiresReplace
		{"optional+computed string", AttributeSpec{Type: "string", Optional: true, Computed: true}, 1},                          // UseStateForUnknown only
		{"pure computed string", AttributeSpec{Type: "string", Computed: true}, 0},                                              // recomputes each apply
		{"required forceNew string", AttributeSpec{Type: "string", Required: true, ForceNew: true}, 1},                          // RequiresReplace only
		{"optional+computed+forceNew list", AttributeSpec{Type: "list(string)", Optional: true, Computed: true, ForceNew: true}, 2},
		{"pure computed list", AttributeSpec{Type: "list(string)", Computed: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := attrSchema(tc.a)
			var n int
			switch v := s.(type) {
			case schema.StringAttribute:
				n = len(v.PlanModifiers)
			case schema.ListAttribute:
				n = len(v.PlanModifiers)
			default:
				t.Fatalf("unexpected attribute type %T", s)
			}
			if n != tc.want {
				t.Errorf("plan modifiers = %d, want %d", n, tc.want)
			}
		})
	}
}

func TestLoadResourceSpecs(t *testing.T) {
	// The framework branch ships no resource specs; the loader must succeed and
	// return an empty set (and must validate every spec that is present).
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, s := range specs {
		if err := s.validate(); err != nil {
			t.Errorf("embedded spec %q invalid: %v", s.Name, err)
		}
	}
}

func TestSpecValidate(t *testing.T) {
	tests := []struct {
		name    string
		spec    ResourceSpec
		wantErr bool
	}{
		{
			name: "valid",
			spec: ResourceSpec{
				Name: "x", IDPath: "id",
				Endpoint:   EndpointSpec{UriBase: "/v1/test"},
				Attributes: []AttributeSpec{{Name: "name", Type: "string", Required: true}},
			},
		},
		{name: "missing name", spec: ResourceSpec{IDPath: "id"}, wantErr: true},
		{name: "missing idPath", spec: ResourceSpec{Name: "x"}, wantErr: true},
		{
			name: "bad type",
			spec: ResourceSpec{
				Name: "x", IDPath: "id",
				Attributes: []AttributeSpec{{Name: "y", Type: "float", Optional: true}},
			},
			wantErr: true,
		},
		{
			name: "reserved id",
			spec: ResourceSpec{
				Name: "x", IDPath: "id",
				Attributes: []AttributeSpec{{Name: "id", Type: "string", Computed: true}},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractPath(t *testing.T) {
	resp := map[string]any{
		"id":     "net-1",
		"status": "Complete",
		"result": map[string]any{
			"id": "vpc-123",
			"subnets": []any{
				map[string]any{"subnetId": "sn-a", "type": "public"},
				map[string]any{"subnetId": "sn-b", "type": "private"},
			},
		},
	}
	tests := []struct {
		path string
		want any
	}{
		{"status", "Complete"},
		{"result.id", "vpc-123"},
		{"result.subnets[].subnetId", []any{"sn-a", "sn-b"}},
		{"result.missing", nil},
		{"result.subnets[].nope", []any{}},
	}
	for _, tt := range tests {
		got := extractPath(resp, splitDot(tt.path))
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("extractPath(%q) = %#v, want %#v", tt.path, got, tt.want)
		}
	}
}

func TestComposeID(t *testing.T) {
	if got := composeID([]string{"ws-1"}, "net-2"); got != "ws-1/net-2" {
		t.Errorf("single-scope composeID = %q", got)
	}
	if got := composeID([]string{"t-1", "c-9"}, "np-3"); got != "t-1/c-9/np-3" {
		t.Errorf("multi-scope composeID = %q", got)
	}
	if got := composeID(nil, "obj-7"); got != "obj-7" {
		t.Errorf("no-scope composeID = %q", got)
	}
}

func TestCheckPathParams(t *testing.T) {
	spec := ResourceSpec{
		Name:   "nodepool",
		IDPath: "id",
		Attributes: []AttributeSpec{
			{Name: "tenant_id", Type: "string", Required: true, ForceNew: true},
			{Name: "cluster_id", Type: "string", Required: true, ForceNew: true},
			{Name: "az_count", Type: "int", Optional: true},
		},
	}
	if err := spec.checkPathParams([]string{"tenant_id", "cluster_id"}); err != nil {
		t.Errorf("valid path params rejected: %v", err)
	}
	if err := spec.checkPathParams([]string{"missing"}); err == nil {
		t.Error("expected error for unknown path parameter")
	}
	if err := spec.checkPathParams([]string{"az_count"}); err == nil {
		t.Error("expected error for non-string path parameter")
	}
}

func TestSetPath(t *testing.T) {
	body := map[string]any{}
	setPath(body, []string{"name"}, "net")
	setPath(body, []string{"spec", "region"}, "us-east-1")
	setPath(body, []string{"spec", "cidr"}, "10.0.0.0/16")
	setPath(body, []string{"spec", "provisioner", "type"}, "Cli")

	want := map[string]any{
		"name": "net",
		"spec": map[string]any{
			"region": "us-east-1",
			"cidr":   "10.0.0.0/16",
			"provisioner": map[string]any{
				"type": "Cli",
			},
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("setPath built %#v, want %#v", body, want)
	}
}

func TestRequestResponsePaths(t *testing.T) {
	// Same value, different paths in request vs response.
	a := AttributeSpec{APIPath: "spec.region", RequestPath: "spec.region", ResponsePath: "configuration.region"}
	if a.requestPath() != "spec.region" {
		t.Errorf("requestPath = %q", a.requestPath())
	}
	if a.responsePath() != "configuration.region" {
		t.Errorf("responsePath = %q", a.responsePath())
	}
	// Both fall back to APIPath when unset.
	b := AttributeSpec{APIPath: "name"}
	if b.requestPath() != "name" || b.responsePath() != "name" {
		t.Errorf("fallback failed: req=%q resp=%q", b.requestPath(), b.responsePath())
	}
}

func TestRequiredIf_CompoundAndHelpers(t *testing.T) {
	// conditions(): single form normalizes to one equals condition.
	single := RequiredIfRule{Attribute: "x", WhenAttribute: "engine", WhenEquals: "Memcached"}
	if c := single.conditions(); len(c) != 1 || c[0].Attribute != "engine" || c[0].Equals != "Memcached" {
		t.Errorf("single conditions() = %+v", c)
	}
	// conditions(): compound form returns its When list.
	comp := RequiredIfRule{Attribute: "num_node_groups", When: []RequiredIfCondition{
		{Attribute: "engine", NotEquals: "Memcached"},
		{Attribute: "cluster_mode", Equals: "Enabled"},
	}}
	if c := comp.conditions(); len(c) != 2 {
		t.Fatalf("compound conditions() len = %d", len(c))
	}

	// defaultString renders a static default; empty when none.
	d := jsonRaw(`"Disabled"`)
	if got := defaultString(AttributeSpec{Default: &d}); got != "Disabled" {
		t.Errorf("defaultString string = %q", got)
	}
	if got := defaultString(AttributeSpec{}); got != "" {
		t.Errorf("defaultString(no default) = %q", got)
	}

	// message lists every condition.
	msg := requiredIfMessage(comp)
	if !strings.Contains(msg, "engine is not \"Memcached\"") || !strings.Contains(msg, "cluster_mode is \"Enabled\"") {
		t.Errorf("requiredIfMessage = %q", msg)
	}
}

func TestSpecValidate_CompoundRequiredIf(t *testing.T) {
	base := func(rule RequiredIfRule) ResourceSpec {
		return ResourceSpec{
			Name: "x", IDPath: "id",
			Endpoint: EndpointSpec{UriBase: "/x"},
			Attributes: []AttributeSpec{
				{Name: "engine", Type: "string", Required: true},
				{Name: "cluster_mode", Type: "string", Optional: true, Computed: true},
				{Name: "num_node_groups", Type: "int", Optional: true},
			},
			RequiredIf: []RequiredIfRule{rule},
		}
	}
	valid := base(RequiredIfRule{Attribute: "num_node_groups", When: []RequiredIfCondition{
		{Attribute: "engine", NotEquals: "Memcached"}, {Attribute: "cluster_mode", Equals: "Enabled"},
	}})
	if err := valid.validate(); err != nil {
		t.Errorf("valid compound requiredIf rejected: %v", err)
	}
	unknown := base(RequiredIfRule{Attribute: "num_node_groups", When: []RequiredIfCondition{{Attribute: "nope", Equals: "x"}}})
	if err := unknown.validate(); err == nil {
		t.Error("expected error for condition referencing unknown attribute")
	}
	bothSet := base(RequiredIfRule{Attribute: "num_node_groups", When: []RequiredIfCondition{{Attribute: "engine", Equals: "Redis", NotEquals: "Memcached"}}})
	if err := bothSet.validate(); err == nil {
		t.Error("expected error when a condition sets both equals and notEquals")
	}
}

func TestSpecValidate_ConflictsWithAndIsEmpty(t *testing.T) {
	vErr := func(s ResourceSpec) error {
		s.Name, s.IDPath = "x", "id"
		s.Endpoint = EndpointSpec{UriBase: "/x"}
		s.Attributes = append(s.Attributes,
			AttributeSpec{Name: "engine_version", Type: "string", Optional: true},
			AttributeSpec{Name: "snapshot_name", Type: "string", Optional: true},
			AttributeSpec{Name: "snapshot_arns", Type: "list(string)", Optional: true},
		)
		return s.validate()
	}
	if err := vErr(ResourceSpec{ConflictsWith: [][]string{{"snapshot_name", "snapshot_arns"}}}); err != nil {
		t.Errorf("valid conflictsWith rejected: %v", err)
	}
	if err := vErr(ResourceSpec{ConflictsWith: [][]string{{"snapshot_name", "nope"}}}); err == nil {
		t.Error("expected error for conflictsWith referencing unknown attribute")
	}
	if err := vErr(ResourceSpec{ConflictsWith: [][]string{{"snapshot_name"}}}); err == nil {
		t.Error("expected error for conflictsWith group with < 2 attributes")
	}
	if err := vErr(ResourceSpec{RequiredIf: []RequiredIfRule{
		{Attribute: "engine_version", When: []RequiredIfCondition{{Attribute: "snapshot_name", IsEmpty: true}}},
	}}); err != nil {
		t.Errorf("valid isEmpty requiredIf rejected: %v", err)
	}
	if err := vErr(ResourceSpec{RequiredIf: []RequiredIfRule{
		{Attribute: "engine_version", When: []RequiredIfCondition{{Attribute: "snapshot_name", IsEmpty: true, Equals: "x"}}},
	}}); err == nil {
		t.Error("expected error when a condition sets isEmpty and equals")
	}
}

func TestApplyConstants(t *testing.T) {
	body := map[string]any{"name": "net"}
	constants := []ConstantField{
		{Path: "kind", Value: jsonRaw(`"Network"`)},
		{Path: "spec.apiVersion", Value: jsonRaw(`2`)},
		{Path: "spec.enabled", Value: jsonRaw(`true`)},
	}
	var diags diag.Diagnostics
	applyConstants(body, constants, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	want := map[string]any{
		"name": "net",
		"kind": "Network",
		"spec": map[string]any{
			"apiVersion": float64(2),
			"enabled":    true,
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("applyConstants = %#v, want %#v", body, want)
	}
}

func TestToStringValue(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"hi", "hi"},
		{true, "true"},
		{float64(24), "24"},
	}
	for _, tt := range tests {
		if got := toStringValue(tt.in); got != tt.want {
			t.Errorf("toStringValue(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGoToTftypesValue(t *testing.T) {
	listType := tftypes.List{ElementType: tftypes.String}

	got := goToTftypesValue(listType, []any{"a", "b"})
	want := tftypes.NewValue(listType, []tftypes.Value{
		tftypes.NewValue(tftypes.String, "a"),
		tftypes.NewValue(tftypes.String, "b"),
	})
	if !got.Equal(want) {
		t.Errorf("list = %v, want %v", got, want)
	}

	if null := goToTftypesValue(tftypes.String, nil); !null.IsNull() {
		t.Errorf("nil should produce null value, got %v", null)
	}

	num := goToTftypesValue(tftypes.Number, float64(24))
	if !num.Equal(tftypes.NewValue(tftypes.Number, bigFloatOf(24))) {
		t.Errorf("number = %v", num)
	}

	// Map of numbers.
	mapType := tftypes.Map{ElementType: tftypes.Number}
	gotMap := goToTftypesValue(mapType, map[string]any{"x": float64(1)})
	wantMap := tftypes.NewValue(mapType, map[string]tftypes.Value{
		"x": tftypes.NewValue(tftypes.Number, bigFloatOf(1)),
	})
	if !gotMap.Equal(wantMap) {
		t.Errorf("map = %v, want %v", gotMap, wantMap)
	}
}

func TestFooExampleIsValid(t *testing.T) {
	data, err := os.ReadFile("../examples/adding-a-resource/foo.json")
	if err != nil {
		t.Fatalf("reading foo.json: %v", err)
	}
	var spec ResourceSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parsing foo.json: %v", err)
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("foo.json invalid: %v", err)
	}
	// Schema must build for every type in the catalog without panicking.
	r := &dynamicResource{spec: spec}
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("foo.json schema diagnostics: %v", resp.Diagnostics)
	}
}

// #2 — on create, an Optional+Computed attribute the user left unset (unknown in
// the plan, no static default) is filled from the response; a configured input
// is kept from the plan (not overwritten by the server value).
func TestStateFromResponse_RefreshesUnknownKeepsInput(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":   tftypes.String,
		"size": tftypes.String,
		"name": tftypes.String,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"size": tftypes.NewValue(tftypes.String, tftypes.UnknownValue), // optional+computed, unset
		"name": tftypes.NewValue(tftypes.String, "myname"),             // configured input
	})
	r := &dynamicResource{spec: ResourceSpec{Attributes: []AttributeSpec{
		{Name: "size", Type: "string", Optional: true, Computed: true, APIPath: "spec.size"},
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
	}}}

	var diags diag.Diagnostics
	out := r.stateFromResponse(context.Background(), base,
		map[string]any{"spec": map[string]any{"size": "large"}, "name": "server-name"},
		map[string]string{}, "obj-1", false, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	var id, size, name string
	_ = m["id"].As(&id)
	if id != "obj-1" {
		t.Errorf("id = %q, want obj-1", id)
	}
	if !m["size"].IsKnown() {
		t.Fatal("size still unknown — should have been refreshed from the response")
	}
	_ = m["size"].As(&size)
	if size != "large" {
		t.Errorf("size = %q, want large", size)
	}
	_ = m["name"].As(&name)
	if name != "myname" {
		t.Errorf("name = %q, want myname (configured value must be kept on create)", name)
	}
}

// On Read (refreshInputs=true) every API-mapped attribute — including ones the
// user configured — is replaced by the live response so Terraform detects
// drift, while attributes without an apiPath (e.g. path parameters) keep their
// prior state value.
func TestStateFromResponse_ReadRefreshesForDrift(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":           tftypes.String,
		"workspace_id": tftypes.String,
		"size":         tftypes.String,
	}}
	prior := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, "obj-1"),
		"workspace_id": tftypes.NewValue(tftypes.String, "ws-1"), // path param, no apiPath
		"size":         tftypes.NewValue(tftypes.String, "small"),
	})
	r := &dynamicResource{spec: ResourceSpec{Attributes: []AttributeSpec{
		{Name: "workspace_id", Type: "string", Required: true},
		{Name: "size", Type: "string", Optional: true, APIPath: "spec.size"},
	}}}

	var diags diag.Diagnostics
	out := r.stateFromResponse(context.Background(), prior,
		map[string]any{"spec": map[string]any{"size": "large"}}, // drifted out of band
		map[string]string{}, "obj-1", true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	var ws, size string
	_ = m["workspace_id"].As(&ws)
	if ws != "ws-1" {
		t.Errorf("workspace_id = %q, want ws-1 (no apiPath — must keep prior value)", ws)
	}
	_ = m["size"].As(&size)
	if size != "large" {
		t.Errorf("size = %q, want large (read must surface drift from the response)", size)
	}
}

// A computed child nested inside a configured object must be resolved to a known
// value from the response on create — leaving it unknown errors with "provider
// returned invalid result object after apply". Sibling configured leaves are kept.
func TestStateFromResponse_ResolvesNestedComputedUnknown(t *testing.T) {
	ngType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"instance_type":   tftypes.String,
		"node_subnet_ids": tftypes.List{ElementType: tftypes.String},
	}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":                tftypes.String,
		"system_node_group": ngType,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"system_node_group": tftypes.NewValue(ngType, map[string]tftypes.Value{
			"instance_type":   tftypes.NewValue(tftypes.String, "t3.medium"),                                     // configured
			"node_subnet_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, tftypes.UnknownValue), // computed, unknown
		}),
	})
	r := &dynamicResource{spec: ResourceSpec{Attributes: []AttributeSpec{
		{Name: "system_node_group", Type: "object", Optional: true, APIPath: "spec.systemNodeGroup", Attributes: []AttributeSpec{
			{Name: "instance_type", Type: "string", Optional: true, Computed: true, APIPath: "instanceType"},
			{Name: "node_subnet_ids", Type: "list(string)", Computed: true, NoSend: true, APIPath: "nodeSubnetIds"},
		}},
	}}}

	var diags diag.Diagnostics
	out := r.stateFromResponse(context.Background(), base,
		map[string]any{"spec": map[string]any{"systemNodeGroup": map[string]any{
			"instanceType":  "t3.medium",
			"nodeSubnetIds": []any{"subnet-a", "subnet-b"},
		}}},
		map[string]string{}, "obj-1", false, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !out.IsFullyKnown() {
		t.Fatal("result still has unknown values after apply — nested computed not resolved")
	}
	m := map[string]tftypes.Value{}
	_ = out.As(&m)
	ng := map[string]tftypes.Value{}
	_ = m["system_node_group"].As(&ng)
	var instance string
	_ = ng["instance_type"].As(&instance)
	if instance != "t3.medium" {
		t.Errorf("instance_type = %q, want t3.medium (configured value must be kept)", instance)
	}
	var subnets []tftypes.Value
	_ = ng["node_subnet_ids"].As(&subnets)
	if len(subnets) != 2 {
		t.Errorf("node_subnet_ids len = %d, want 2 (resolved from response)", len(subnets))
	}
}

// #3 — a top-level attribute with no apiPath (e.g. a path parameter) is not sent
// in the request body, while a mapped sibling is.
func TestBodyFromRaw_SkipsTopLevelEmptyPath(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":           tftypes.String,
		"workspace_id": tftypes.String,
		"name":         tftypes.String,
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"workspace_id": tftypes.NewValue(tftypes.String, "ws-1"),
		"name":         tftypes.NewValue(tftypes.String, "n"),
	})
	r := &dynamicResource{spec: ResourceSpec{Attributes: []AttributeSpec{
		{Name: "workspace_id", Type: "string", Required: true}, // no apiPath → path param, not sent
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
	}}}

	var diags diag.Diagnostics
	body := r.bodyFromRaw(raw, "create", &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if _, present := body["workspace_id"]; present {
		t.Error("workspace_id (empty apiPath) must not appear in the request body")
	}
	if body["name"] != "n" {
		t.Errorf("name = %v, want n", body["name"])
	}
}

func TestApiBodyEqual(t *testing.T) {
	a := map[string]any{"name": "n", "spec": map[string]any{"x": float64(1)}}
	b := map[string]any{"spec": map[string]any{"x": float64(1)}, "name": "n"} // key order differs
	if !apiBodyEqual(a, b) {
		t.Error("equal bodies (differing key order) should compare equal")
	}
	if apiBodyEqual(a, map[string]any{"name": "n2"}) {
		t.Error("different bodies should not compare equal")
	}
}

// ModifyPlan: when no API-mapped attribute changed (e.g. only the timeouts block
// differs, as after import), computed outputs are held at their prior state value
// instead of churning to "(known after apply)".
func TestModifyPlan_NoApiChangeHoldsComputed(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"workspace_id": tftypes.String,
		"name":         tftypes.String,
		"status":       tftypes.String,
		"vpc_id":       tftypes.String,
	}}
	r := &dynamicResource{spec: ResourceSpec{Attributes: []AttributeSpec{
		{Name: "workspace_id", Type: "string", Required: true},
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
		{Name: "status", Type: "string", Computed: true, APIPath: "status"},
		{Name: "vpc_id", Type: "string", Computed: true, APIPath: "result.id"},
	}}}
	state := tftypes.NewValue(objType, map[string]tftypes.Value{
		"workspace_id": tftypes.NewValue(tftypes.String, "ws"),
		"name":         tftypes.NewValue(tftypes.String, "n"),
		"status":       tftypes.NewValue(tftypes.String, "Complete"),
		"vpc_id":       tftypes.NewValue(tftypes.String, "vpc-1"),
	})
	mkPlan := func(name string) tftypes.Value {
		return tftypes.NewValue(objType, map[string]tftypes.Value{
			"workspace_id": tftypes.NewValue(tftypes.String, "ws"),
			"name":         tftypes.NewValue(tftypes.String, name),
			"status":       tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"vpc_id":       tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		})
	}

	// No API change (name unchanged) → computed held from state.
	plan := mkPlan("n")
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Raw: plan}}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		State: tfsdk.State{Raw: state}, Plan: tfsdk.Plan{Raw: plan},
	}, resp)
	var top map[string]tftypes.Value
	if err := resp.Plan.Raw.As(&top); err != nil {
		t.Fatal(err)
	}
	var status, vpc string
	if err := top["status"].As(&status); err != nil || status != "Complete" {
		t.Errorf("status = %q (err %v), want held value Complete", status, err)
	}
	if err := top["vpc_id"].As(&vpc); err != nil || vpc != "vpc-1" {
		t.Errorf("vpc_id = %q (err %v), want held value vpc-1", vpc, err)
	}

	// Real API change (name differs) → computed must stay unknown to recompute.
	plan2 := mkPlan("n2")
	resp2 := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Raw: plan2}}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		State: tfsdk.State{Raw: state}, Plan: tfsdk.Plan{Raw: plan2},
	}, resp2)
	var top2 map[string]tftypes.Value
	if err := resp2.Plan.Raw.As(&top2); err != nil {
		t.Fatal(err)
	}
	if top2["status"].IsKnown() {
		t.Error("on a real change, computed status must stay unknown (recompute)")
	}
}

// #4 — composite id is "/"-joined and split back by position.
func TestComposeAndSplitID_RoundTrip(t *testing.T) {
	id := composeID([]string{"tenant", "cluster"}, "obj-9")
	if id != "tenant/cluster/obj-9" {
		t.Fatalf("composeID = %q", id)
	}
	parts, err := splitID(id, 3)
	if err != nil {
		t.Fatalf("splitID: %v", err)
	}
	if !reflect.DeepEqual(parts, []string{"tenant", "cluster", "obj-9"}) {
		t.Errorf("splitID = %v", parts)
	}
	if _, err := splitID("only-two/parts", 3); err == nil {
		t.Error("expected error when id has fewer parts than required")
	}
}

func TestNumberPrecisionRoundTrip(t *testing.T) {
	const huge = "9007199254740993" // 2^53 + 1, not representable as float64
	bf, _, _ := big.ParseFloat(huge, 10, 200, big.ToNearestEven)

	// Request side: tftypes.Number → json.Number (exact), not float64.
	got := tftypesToGo(tftypes.NewValue(tftypes.Number, bf))
	if got != json.Number(huge) {
		t.Errorf("tftypesToGo number = %#v, want json.Number(%s)", got, huge)
	}
	b, _ := json.Marshal(map[string]any{"n": got})
	if string(b) != `{"n":`+huge+`}` {
		t.Errorf("marshalled = %s, want exact integer", b)
	}

	// Response side: json.Number (as decoded with UseNumber) → tftypes.Number.
	back := goToTftypesValue(tftypes.Number, json.Number(huge))
	var rt big.Float
	if err := back.As(&rt); err != nil {
		t.Fatal(err)
	}
	if rt.Text('f', 0) != huge {
		t.Errorf("round-trip number = %s, want %s", rt.Text('f', 0), huge)
	}
}

func TestParseType(t *testing.T) {
	cases := map[string]typeInfo{
		"string":       {coll: "", elem: "string"},
		"number":       {coll: "", elem: "number"},
		"list(string)": {coll: "list", elem: "string"},
		"set(int)":     {coll: "set", elem: "int"},
		"map(bool)":    {coll: "map", elem: "bool"},
		"object":       {coll: "", elem: "object"},
		"list(object)": {coll: "list", elem: "object"},
		"map(object)":  {coll: "map", elem: "object"},
	}
	for in, want := range cases {
		got, err := parseType(in)
		if err != nil || got != want {
			t.Errorf("parseType(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"float", "list(float)", "list()", "tuple(string)"} {
		if _, err := parseType(bad); err == nil {
			t.Errorf("parseType(%q) should error", bad)
		}
	}
}

func TestNestedObjectSchemaBuilds(t *testing.T) {
	a := AttributeSpec{
		Name: "endpoints", Type: "list(object)", Optional: true,
		Attributes: []AttributeSpec{
			{Name: "host", Type: "string", Required: true},
			{Name: "port", Type: "int", Optional: true},
			{Name: "opts", Type: "map(string)", Optional: true},
			{Name: "meta", Type: "object", Optional: true, Attributes: []AttributeSpec{
				{Name: "weight", Type: "number", Optional: true},
			}},
		},
	}
	if _, ok := attrSchema(a).(schema.ListNestedAttribute); !ok {
		t.Fatalf("expected ListNestedAttribute, got %T", attrSchema(a))
	}
	// Pure schema construction shouldn't panic on deep nesting.
}

func TestObjectRoundTripNameRemap(t *testing.T) {
	// Schema: an object attribute whose nested field is renamed via apiPath.
	a := AttributeSpec{
		Name: "config", Type: "object", Optional: true,
		Attributes: []AttributeSpec{
			{Name: "max_size", Type: "int", Optional: true, APIPath: "maxSize"},
			{Name: "name", Type: "string", Optional: true},
		},
	}
	objTFType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"max_size": tftypes.Number,
		"name":     tftypes.String,
	}}

	// apiToState: API uses camelCase "maxSize" → schema "max_size".
	apiData := map[string]any{"maxSize": float64(5), "name": "n"}
	state := objectFromResponse(a.Attributes, objTFType, apiData)
	var got map[string]tftypes.Value
	if err := state.As(&got); err != nil {
		t.Fatal(err)
	}
	var ms big.Float
	_ = got["max_size"].As(&ms)
	if f, _ := ms.Float64(); f != 5 {
		t.Errorf("max_size = %v, want 5", f)
	}

	// stateToAPI: schema "max_size" → API "maxSize".
	planVal := tftypes.NewValue(objTFType, map[string]tftypes.Value{
		"max_size": tftypes.NewValue(tftypes.Number, bigFloatOf(5)),
		"name":     tftypes.NewValue(tftypes.String, "n"),
	})
	body := objectToRequest(a.Attributes, planVal)
	if body["maxSize"] != json.Number("5") || body["name"] != "n" {
		t.Errorf("objectToRequest = %#v, want maxSize=5,name=n", body)
	}
	if _, leaked := body["max_size"]; leaked {
		t.Error("schema name leaked into request body")
	}
}

func TestWaiterDefaults(t *testing.T) {
	d := defaultWaiterSpec()

	// Empty waiter gets all defaults applied.
	w := &WaiterSpec{}
	applyWaiterDefaults(w)
	if w.StatusPath != d.StatusPath {
		t.Errorf("StatusPath = %q, want %q", w.StatusPath, d.StatusPath)
	}
	if w.SuccessState != d.SuccessState {
		t.Errorf("SuccessState = %q, want %q", w.SuccessState, d.SuccessState)
	}
	if !reflect.DeepEqual(w.FailureStates, d.FailureStates) {
		t.Errorf("FailureStates = %v, want %v", w.FailureStates, d.FailureStates)
	}
	if w.FailureDetailPath != d.FailureDetailPath {
		t.Errorf("FailureDetailPath = %q, want %q", w.FailureDetailPath, d.FailureDetailPath)
	}
	if w.PollIntervalSeconds != d.PollIntervalSeconds {
		t.Errorf("PollIntervalSeconds = %d, want %d", w.PollIntervalSeconds, d.PollIntervalSeconds)
	}
	if w.CreateTimeoutMinutes != d.CreateTimeoutMinutes {
		t.Errorf("CreateTimeoutMinutes = %d, want %d", w.CreateTimeoutMinutes, d.CreateTimeoutMinutes)
	}
	if w.UpdateTimeoutMinutes != d.UpdateTimeoutMinutes {
		t.Errorf("UpdateTimeoutMinutes = %d, want %d", w.UpdateTimeoutMinutes, d.UpdateTimeoutMinutes)
	}
	if w.DeleteTimeoutMinutes != d.DeleteTimeoutMinutes {
		t.Errorf("DeleteTimeoutMinutes = %d, want %d", w.DeleteTimeoutMinutes, d.DeleteTimeoutMinutes)
	}

	// Explicitly set fields are not overwritten by defaults.
	custom := &WaiterSpec{
		PollIntervalSeconds:  15,
		CreateTimeoutMinutes: 60,
		DeprovisionedState:   "DeProvisioned",
		FailureRetries:       3,
	}
	applyWaiterDefaults(custom)
	if custom.PollIntervalSeconds != 15 {
		t.Errorf("custom PollIntervalSeconds overwritten, got %d", custom.PollIntervalSeconds)
	}
	if custom.CreateTimeoutMinutes != 60 {
		t.Errorf("custom CreateTimeoutMinutes overwritten, got %d", custom.CreateTimeoutMinutes)
	}
	if custom.DeprovisionedState != "DeProvisioned" {
		t.Errorf("DeprovisionedState overwritten, got %q", custom.DeprovisionedState)
	}
	if custom.FailureRetries != 3 {
		t.Errorf("FailureRetries overwritten, got %d", custom.FailureRetries)
	}
	// Unset fields still get defaults.
	if custom.StatusPath != d.StatusPath {
		t.Errorf("StatusPath not defaulted, got %q", custom.StatusPath)
	}
	if custom.UpdateTimeoutMinutes != d.UpdateTimeoutMinutes {
		t.Errorf("UpdateTimeoutMinutes not defaulted, got %d", custom.UpdateTimeoutMinutes)
	}
}

// helpers
func splitDot(s string) []string { return splitOn(s, '.') }

func splitOn(s string, sep rune) []string {
	out := []string{}
	cur := ""
	for _, c := range s {
		if c == sep {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	return append(out, cur)
}

func bigFloatOf(i int64) any { return toBigFloat(float64(i)) }

// A `default` on a list/set/map/object attribute must actually reach the
// framework schema (it was previously parsed into the spec but silently
// dropped, so the declared default never applied). Verify each collection kind
// wires a Default and that it resolves to the configured value.
func TestAttrSchema_CollectionAndObjectDefaults(t *testing.T) {
	ctx := context.Background()
	rawPtr := func(s string) *json.RawMessage { r := jsonRaw(s); return &r }

	// list(string): default resolves to the configured elements.
	la, ok := attrSchema(AttributeSpec{
		Name: "tags", Type: "list(string)", Optional: true, Computed: true,
		Default: rawPtr(`["a","b"]`),
	}).(schema.ListAttribute)
	if !ok {
		t.Fatal("expected ListAttribute")
	}
	if la.Default == nil {
		t.Fatal("list(string) default not wired")
	}
	var lr defaults.ListResponse
	la.Default.DefaultList(ctx, defaults.ListRequest{}, &lr)
	if lr.Diagnostics.HasError() {
		t.Fatalf("list default diags: %v", lr.Diagnostics)
	}
	var got []string
	lr.PlanValue.ElementsAs(ctx, &got, false)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("list(string) default = %v, want [a b]", got)
	}

	// set(int): default resolves to the configured elements.
	sa, ok := attrSchema(AttributeSpec{Name: "ports", Type: "set(int)", Optional: true, Computed: true, Default: rawPtr(`[80,443]`)}).(schema.SetAttribute)
	if !ok {
		t.Fatal("expected SetAttribute")
	}
	if sa.Default == nil {
		t.Fatal("set(int) default not wired")
	}
	var sr defaults.SetResponse
	sa.Default.DefaultSet(ctx, defaults.SetRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("set default diags: %v", sr.Diagnostics)
	}
	var gotSet []int64
	sr.PlanValue.ElementsAs(ctx, &gotSet, false)
	if len(gotSet) != 2 {
		t.Errorf("set(int) default len = %d, want 2", len(gotSet))
	}

	// map(string): default resolves to the configured entries.
	ma, ok := attrSchema(AttributeSpec{Name: "labels", Type: "map(string)", Optional: true, Computed: true, Default: rawPtr(`{"k":"v"}`)}).(schema.MapAttribute)
	if !ok {
		t.Fatal("expected MapAttribute")
	}
	if ma.Default == nil {
		t.Fatal("map(string) default not wired")
	}
	var mr defaults.MapResponse
	ma.Default.DefaultMap(ctx, defaults.MapRequest{}, &mr)
	if mr.Diagnostics.HasError() {
		t.Fatalf("map default diags: %v", mr.Diagnostics)
	}
	var gotMap map[string]string
	mr.PlanValue.ElementsAs(ctx, &gotMap, false)
	if gotMap["k"] != "v" {
		t.Errorf("map(string) default[k] = %q, want v", gotMap["k"])
	}

	// list(object): default resolves through the nested object attributes.
	na, ok := attrSchema(AttributeSpec{
		Name: "rules", Type: "list(object)", Optional: true, Computed: true,
		Attributes: []AttributeSpec{
			{Name: "port", Type: "int", Optional: true},
			{Name: "proto", Type: "string", Optional: true},
		},
		Default: rawPtr(`[{"port":8080,"proto":"tcp"}]`),
	}).(schema.ListNestedAttribute)
	if !ok {
		t.Fatal("expected ListNestedAttribute")
	}
	if na.Default == nil {
		t.Fatal("list(object) default not wired")
	}
	var nr defaults.ListResponse
	na.Default.DefaultList(ctx, defaults.ListRequest{}, &nr)
	if nr.Diagnostics.HasError() {
		t.Fatalf("list(object) default diags: %v", nr.Diagnostics)
	}
	elems := nr.PlanValue.Elements()
	if len(elems) != 1 {
		t.Fatalf("list(object) default len = %d, want 1", len(elems))
	}
	obj := elems[0].(types.Object).Attributes()
	if p := obj["proto"].(types.String).ValueString(); p != "tcp" {
		t.Errorf("nested proto = %q, want tcp", p)
	}

	// A malformed default is ignored (no default wired), not fatal.
	if bad := attrSchema(AttributeSpec{Name: "x", Type: "list(string)", Optional: true, Computed: true, Default: rawPtr(`{not json`)}).(schema.ListAttribute); bad.Default != nil {
		t.Error("malformed default should not be wired")
	}
}

// TestBodyFromRaw_VerbAwareConstants verifies that createConstants are injected
// only on create and updateConstants only on update, and that requestConstants
// are always injected regardless of verb.
func TestBodyFromRaw_VerbAwareConstants(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"name": tftypes.String,
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "fn"),
	})
	r := &dynamicResource{spec: ResourceSpec{
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
		},
		RequestConstants: []ConstantField{{Path: "always", Value: jsonRaw(`"yes"`)}},
		CreateConstants:  []ConstantField{{Path: "spec.mode", Value: jsonRaw(`"Create"`)}},
		UpdateConstants:  []ConstantField{{Path: "spec.mode", Value: jsonRaw(`"Update"`)}},
	}}

	var diags diag.Diagnostics

	create := r.bodyFromRaw(raw, "create", &diags)
	if diags.HasError() {
		t.Fatalf("create diags: %v", diags)
	}
	if create["always"] != "yes" {
		t.Errorf("create: always = %v, want yes", create["always"])
	}
	if spec, _ := create["spec"].(map[string]any); spec["mode"] != "Create" {
		t.Errorf("create: spec.mode = %v, want Create", spec["mode"])
	}

	update := r.bodyFromRaw(raw, "update", &diags)
	if diags.HasError() {
		t.Fatalf("update diags: %v", diags)
	}
	if update["always"] != "yes" {
		t.Errorf("update: always = %v, want yes", update["always"])
	}
	if spec, _ := update["spec"].(map[string]any); spec["mode"] != "Update" {
		t.Errorf("update: spec.mode = %v, want Update", spec["mode"])
	}
}

// readAfterWrite makes Create build state from a follow-up GET rather than the
// POST response. This matters when the write response differs from the read —
// e.g. the backend encrypts a field on save (create returns ciphertext) but
// decrypts it on read (admin_provider credentials). Here the mock returns a
// different value for POST vs GET; a computed attribute the user left unknown
// must end up with the GET value when readAfterWrite is set, and the POST value
// when it is not.
func TestCreate_ReadAfterWrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"data":{"id":"o1","secret":"CIPHER"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"o1","secret":"PLAIN"}}`)) // GET
	}))
	defer srv.Close()

	client, err := duplosdk.NewClient(srv.URL, "tok", false, 0)
	if err != nil {
		t.Fatal(err)
	}

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":     tftypes.String,
		"secret": tftypes.String,
	}}
	// secret is left unknown in the plan, so it is filled from the response.
	planRaw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"secret": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	create := func(readAfterWrite bool) string {
		spec := ResourceSpec{
			Name:           "thing",
			IDPath:         "id",
			ReadAfterWrite: readAfterWrite,
			Endpoint:       EndpointSpec{UriBase: "/things"},
			Attributes: []AttributeSpec{
				{Name: "secret", Type: "string", Optional: true, Computed: true, APIPath: "secret"},
			},
		}
		ep, err := spec.BuildEndpoint()
		if err != nil {
			t.Fatal(err)
		}
		r := &dynamicResource{spec: spec, endpoint: ep}
		r.Client = client

		var sr resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
		req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: planRaw}}
		resp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
		r.Create(context.Background(), req, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("readAfterWrite=%v: Create diags: %v", readAfterWrite, resp.Diagnostics)
		}
		m := map[string]tftypes.Value{}
		if err := resp.State.Raw.As(&m); err != nil {
			t.Fatal(err)
		}
		var secret string
		_ = m["secret"].As(&secret)
		return secret
	}

	if got := create(true); got != "PLAIN" {
		t.Errorf("readAfterWrite=true: secret = %q, want PLAIN (from the follow-up GET)", got)
	}
	if got := create(false); got != "CIPHER" {
		t.Errorf("readAfterWrite=false: secret = %q, want CIPHER (from the POST response)", got)
	}
}

// min/max must attach AtLeast/AtMost validators to numeric (int/number)
// attributes and nothing when unset. Guards the engine wiring for ranges like
// a quota definition's limit_usd >= 0.01.
func TestAttrSchema_MinMaxValidator(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		a    AttributeSpec
		want int
	}{
		{"number min", AttributeSpec{Type: "number", Required: true, Min: f(0.01)}, 1},
		{"number min+max", AttributeSpec{Type: "number", Optional: true, Min: f(0), Max: f(100)}, 2},
		{"number none", AttributeSpec{Type: "number", Optional: true}, 0},
		{"int min", AttributeSpec{Type: "int", Optional: true, Min: f(1)}, 1},
		{"int none", AttributeSpec{Type: "int", Optional: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := attrSchema(tc.a)
			var n int
			switch v := s.(type) {
			case schema.Float64Attribute:
				n = len(v.Validators)
			case schema.Int64Attribute:
				n = len(v.Validators)
			default:
				t.Fatalf("unexpected attribute type %T", s)
			}
			if n != tc.want {
				t.Errorf("validators = %d, want %d", n, tc.want)
			}
		})
	}
}

// requiredIf on an object target must read the live config value, not treat a
// populated object as missing. Regression for the readPlanGoValue "object" case
// (e.g. admin_skill's git_repo requiredIf when format == PrivateGitRepo).
func TestRequiredIf_ObjectTargetReadsConfig(t *testing.T) {
	spec := ResourceSpec{
		Name: "thing", IDPath: "id",
		Endpoint:   EndpointSpec{UriBase: "/things"},
		RequiredIf: []RequiredIfRule{{Attribute: "git_repo", WhenAttribute: "format", WhenEquals: "PrivateGitRepo"}},
		Attributes: []AttributeSpec{
			{Name: "format", Type: "string", Optional: true, APIPath: "format"},
			{Name: "git_repo", Type: "object", Optional: true, APIPath: "gitRepo", Attributes: []AttributeSpec{
				{Name: "name", Type: "string", Optional: true, APIPath: "name"},
				{Name: "scope_id", Type: "string", Optional: true, APIPath: "scopeId"},
			}},
		},
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	v := requiredIfValidator{spec: spec}

	gitType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"name": tftypes.String, "scope_id": tftypes.String}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":       tftypes.String,
		"format":   tftypes.String,
		"git_repo": gitType,
	}}

	run := func(gitRepo tftypes.Value) bool {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":       tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"format":   tftypes.NewValue(tftypes.String, "PrivateGitRepo"),
			"git_repo": gitRepo,
		})
		req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: raw}}
		resp := resource.ValidateConfigResponse{}
		v.ValidateResource(context.Background(), req, &resp)
		return resp.Diagnostics.HasError()
	}

	populated := tftypes.NewValue(gitType, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "skills-repo"),
		"scope_id": tftypes.NewValue(tftypes.String, "scope-1"),
	})
	if run(populated) {
		t.Error("requiredIf flagged a populated object as missing (readPlanGoValue object regression)")
	}
	if !run(tftypes.NewValue(gitType, nil)) {
		t.Error("requiredIf did not fire for a null object when the condition held")
	}
}
