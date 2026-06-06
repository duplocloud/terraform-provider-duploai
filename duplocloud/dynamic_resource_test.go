package duplocloud

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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
		{"pure computed string", AttributeSpec{Type: "string", Computed: true}, 0},                                             // recomputes each apply
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
			"instance_type":   tftypes.NewValue(tftypes.String, "t3.medium"),          // configured
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
	body := r.bodyFromRaw(raw, &diags)
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
