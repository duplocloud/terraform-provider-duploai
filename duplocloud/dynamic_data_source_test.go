package duplocloud

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

func TestDSAttrSchema_Types(t *testing.T) {
	cases := []struct {
		spec AttributeSpec
		want any // zero value of the expected datasource schema type
	}{
		{AttributeSpec{Name: "s", Type: "string", Computed: true}, dsschema.StringAttribute{}},
		{AttributeSpec{Name: "b", Type: "bool", Computed: true}, dsschema.BoolAttribute{}},
		{AttributeSpec{Name: "i", Type: "int", Computed: true}, dsschema.Int64Attribute{}},
		{AttributeSpec{Name: "n", Type: "number", Computed: true}, dsschema.Float64Attribute{}},
		{AttributeSpec{Name: "l", Type: "list(string)", Computed: true}, dsschema.ListAttribute{}},
		{AttributeSpec{Name: "st", Type: "set(int)", Computed: true}, dsschema.SetAttribute{}},
		{AttributeSpec{Name: "m", Type: "map(bool)", Computed: true}, dsschema.MapAttribute{}},
		{AttributeSpec{Name: "o", Type: "object", Computed: true}, dsschema.SingleNestedAttribute{}},
		{AttributeSpec{Name: "lo", Type: "list(object)", Computed: true}, dsschema.ListNestedAttribute{}},
		{AttributeSpec{Name: "so", Type: "set(object)", Computed: true}, dsschema.SetNestedAttribute{}},
		{AttributeSpec{Name: "mo", Type: "map(object)", Computed: true}, dsschema.MapNestedAttribute{}},
	}
	for _, tc := range cases {
		t.Run(tc.spec.Type, func(t *testing.T) {
			got := dsAttrSchema(tc.spec)
			if reflect.TypeOf(got) != reflect.TypeOf(tc.want) {
				t.Errorf("dsAttrSchema(%s) = %T, want %T", tc.spec.Type, got, tc.want)
			}
		})
	}
}

func TestDSAttrSchema_FlagsAndValidators(t *testing.T) {
	s := dsAttrSchema(AttributeSpec{
		Name: "size", Type: "string", Required: true, Sensitive: true,
		Description: "the size", OneOf: []string{"small", "large"},
	}).(dsschema.StringAttribute)
	if !s.Required || s.Optional || s.Computed {
		t.Errorf("flags = req:%v opt:%v comp:%v, want required only", s.Required, s.Optional, s.Computed)
	}
	if !s.Sensitive {
		t.Error("Sensitive not carried over")
	}
	if s.Description != "the size" {
		t.Errorf("Description = %q", s.Description)
	}
	if len(s.Validators) != 1 {
		t.Errorf("OneOf validator not wired, got %d validators", len(s.Validators))
	}
}

// Nested attributes live inside a Computed parent and are always populated from
// the API response — dsNestedAttrMap must force them all to Computed regardless
// of how they are declared in the spec.
func TestDSNestedAttrMap_ForcesComputed(t *testing.T) {
	nested := dsNestedAttrMap([]AttributeSpec{
		{Name: "host", Type: "string", Required: true},
		{Name: "port", Type: "int", Optional: true},
	})
	host := nested["host"].(dsschema.StringAttribute)
	if host.Required || host.Optional || !host.Computed {
		t.Errorf("host flags = req:%v opt:%v comp:%v, want computed only", host.Required, host.Optional, host.Computed)
	}
	port := nested["port"].(dsschema.Int64Attribute)
	if port.Required || port.Optional || !port.Computed {
		t.Errorf("port flags = req:%v opt:%v comp:%v, want computed only", port.Required, port.Optional, port.Computed)
	}
}

// widgetDataSource builds a dynamicDataSource around a representative spec:
// one path parameter, one regular input, one computed output, one write-only
// attribute the API never echoes back (createPath only, no apiPath).
func widgetDataSource(t *testing.T) *dynamicDataSource {
	t.Helper()
	spec := ResourceSpec{
		Name:   "widget",
		IDPath: "id",
		Endpoint: EndpointSpec{
			UriBase: "/v1/test/{workspace_id}/widgets",
		},
		Attributes: []AttributeSpec{
			{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
			{Name: "status", Type: "string", Computed: true, APIPath: "status"},
			{Name: "password", Type: "string", Optional: true, Sensitive: true, CreatePath: "spec.password"},
		},
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("test spec invalid: %v", err)
	}
	endpoint, err := spec.BuildEndpoint()
	if err != nil {
		t.Fatalf("BuildEndpoint: %v", err)
	}
	return &dynamicDataSource{spec: spec, endpoint: endpoint}
}

// The data source schema is derived from the resource spec: the engine "id" and
// path parameters become Required inputs, every readable attribute becomes
// Computed, and write-only attributes (no apiPath/responsePath) are excluded.
func TestDynamicDataSourceSchema_Derivation(t *testing.T) {
	d := widgetDataSource(t)
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diags: %v", resp.Diagnostics)
	}
	attrs := resp.Schema.Attributes

	id, ok := attrs["id"].(dsschema.StringAttribute)
	if !ok || !id.Required {
		t.Error("id must be a Required string attribute")
	}
	ws, ok := attrs["workspace_id"].(dsschema.StringAttribute)
	if !ok || !ws.Required || ws.Optional || ws.Computed {
		t.Error("path parameter workspace_id must be Required only")
	}
	for _, name := range []string{"name", "status"} {
		a, ok := attrs[name].(dsschema.StringAttribute)
		if !ok || a.Required || a.Optional || !a.Computed {
			t.Errorf("%s must be Computed only", name)
		}
	}
	if _, present := attrs["password"]; present {
		t.Error("write-only attribute (createPath only) must be excluded from the data source schema")
	}
}

func TestDynamicDataSourceMetadata(t *testing.T) {
	d := widgetDataSource(t)
	var resp datasource.MetadataResponse
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "duploai"}, &resp)
	if resp.TypeName != "duploai_widget" {
		t.Errorf("TypeName = %q, want duploai_widget", resp.TypeName)
	}
}

// readPathParamScope must work against a tfsdk.Config (the data source Read
// path), returning the name→value scope map and the ordered values used for
// URI substitution.
func TestReadPathParamScope_FromConfig(t *testing.T) {
	d := widgetDataSource(t)
	var schemaResp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &schemaResp)

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":           tftypes.String,
		"workspace_id": tftypes.String,
		"name":         tftypes.String,
		"status":       tftypes.String,
	}}
	cfg := tfsdk.Config{
		Schema: schemaResp.Schema,
		Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":           tftypes.NewValue(tftypes.String, "w-7"),
			"workspace_id": tftypes.NewValue(tftypes.String, "ws-1"),
			"name":         tftypes.NewValue(tftypes.String, nil),
			"status":       tftypes.NewValue(tftypes.String, nil),
		}),
	}

	var diags diag.Diagnostics
	scope, ordered := readPathParamScope(context.Background(), d.endpoint, cfg, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !reflect.DeepEqual(scope, map[string]string{"workspace_id": "ws-1"}) {
		t.Errorf("scope = %v, want {workspace_id: ws-1}", scope)
	}
	if !reflect.DeepEqual(ordered, []string{"ws-1"}) {
		t.Errorf("ordered = %v, want [ws-1]", ordered)
	}
}

// The data source Read path uses buildStateRaw with refreshInputs=true: every
// API-mapped attribute — including ones already holding a value — is replaced
// by the live response, and the engine id is set from the supplied object id.
func TestBuildStateRaw_RefreshInputsReplacesValues(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":     tftypes.String,
		"name":   tftypes.String,
		"status": tftypes.String,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":     tftypes.NewValue(tftypes.String, "obj-1"),
		"name":   tftypes.NewValue(tftypes.String, "stale-name"),
		"status": tftypes.NewValue(tftypes.String, nil),
	})
	attrs := []AttributeSpec{
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
		{Name: "status", Type: "string", Computed: true, APIPath: "status"},
	}

	var diags diag.Diagnostics
	out := buildStateRaw(attrs, base,
		map[string]any{"name": "live-name", "status": "Complete"},
		map[string]string{}, "obj-1", true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	var id, name, status string
	_ = m["id"].As(&id)
	_ = m["name"].As(&name)
	_ = m["status"].As(&status)
	if id != "obj-1" {
		t.Errorf("id = %q, want obj-1", id)
	}
	if name != "live-name" {
		t.Errorf("name = %q, want live-name (refreshInputs must replace stale values)", name)
	}
	if status != "Complete" {
		t.Errorf("status = %q, want Complete", status)
	}
}

// buildStateRaw must tolerate spec attributes that are not present in the
// state's object type — the data source schema excludes write-only attributes
// but passes the full spec.Attributes list.
func TestBuildStateRaw_SkipsAttrsAbsentFromType(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":   tftypes.String,
		"name": tftypes.String,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, "obj-1"),
		"name": tftypes.NewValue(tftypes.String, nil),
	})
	attrs := []AttributeSpec{
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
		// In the schema type above there is no "password" — present in the spec only.
		{Name: "password", Type: "string", Optional: true, APIPath: "spec.password"},
	}

	var diags diag.Diagnostics
	out := buildStateRaw(attrs, base,
		map[string]any{"name": "n", "spec": map[string]any{"password": "hunter2"}},
		map[string]string{}, "obj-1", true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	if _, present := m["password"]; present {
		t.Error("attribute absent from the schema type must not appear in state")
	}
	var name string
	_ = m["name"].As(&name)
	if name != "n" {
		t.Errorf("name = %q, want n", name)
	}
}

func TestBaseDataSourceConfigure(t *testing.T) {
	ctx := context.Background()

	// Nil provider data (framework calls Configure before the provider is set up).
	var d baseDataSource
	var resp datasource.ConfigureResponse
	d.Configure(ctx, datasource.ConfigureRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("nil ProviderData must be a no-op, got %v", resp.Diagnostics)
	}
	if d.Client != nil {
		t.Error("client must stay nil when ProviderData is nil")
	}

	// Wrong provider data type → diagnostic, client not set.
	var resp2 datasource.ConfigureResponse
	d.Configure(ctx, datasource.ConfigureRequest{ProviderData: "not-a-client"}, &resp2)
	if !resp2.Diagnostics.HasError() {
		t.Error("unexpected ProviderData type must produce an error diagnostic")
	}
	if d.Client != nil {
		t.Error("client must not be set from a wrong-typed ProviderData")
	}

	// Correct type → client stored.
	client := &duplosdk.Client{}
	var resp3 datasource.ConfigureResponse
	d.Configure(ctx, datasource.ConfigureRequest{ProviderData: client}, &resp3)
	if resp3.Diagnostics.HasError() {
		t.Fatalf("diags: %v", resp3.Diagnostics)
	}
	if d.Client != client {
		t.Error("client not stored from ProviderData")
	}
}

// Every embedded spec with dataSource:true must register cleanly — endpoint
// builds, path parameters resolve to string attributes, and the derived data
// source schema constructs without diagnostics. This mirrors the startup path
// in provider.DataSources, which panics on any of these failures.
func TestEmbeddedDataSourceSpecsRegister(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	var count int
	for _, spec := range specs {
		if !spec.DataSource {
			continue
		}
		count++
		t.Run(spec.Name, func(t *testing.T) {
			endpoint, err := spec.BuildEndpoint()
			if err != nil {
				t.Fatalf("BuildEndpoint: %v", err)
			}
			if err := spec.checkPathParams(endpoint.PathParams()); err != nil {
				t.Fatalf("checkPathParams: %v", err)
			}
			d := &dynamicDataSource{spec: spec, endpoint: endpoint}
			var resp datasource.SchemaResponse
			d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("schema diags: %v", resp.Diagnostics)
			}
			if _, ok := resp.Schema.Attributes["id"]; !ok {
				t.Error("derived schema is missing the engine id attribute")
			}
		})
	}
	t.Logf("validated %d data source specs", count)
}
