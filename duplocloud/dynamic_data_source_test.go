package duplocloud

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
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
		map[string]string{}, "obj-1", true, true, &diags)
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
		map[string]string{}, "obj-1", true, true, &diags)
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

// Every embedded spec that registers a data source — dataSource:true or
// dataSourceOnly:true — must do so cleanly: endpoint builds, path parameters
// resolve to string attributes, and the derived data source schema constructs
// without diagnostics. The filter mirrors provider.DataSources, which panics on
// any of these failures at startup.
func TestEmbeddedDataSourceSpecsRegister(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	var count int
	for _, spec := range specs {
		if !spec.DataSource && !spec.DataSourceOnly {
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
			// Every data source exposes exactly one Required lookup key: "id" by
			// default, or spec.lookupAttribute when the spec renames it.
			lookup := spec.lookupName()
			if _, ok := resp.Schema.Attributes[lookup]; !ok {
				t.Errorf("derived schema is missing the engine lookup attribute %q", lookup)
			}
			if lookup != "id" {
				if _, ok := resp.Schema.Attributes["id"]; ok {
					t.Error("a renamed lookup attribute must replace \"id\", not coexist with it")
				}
			}
		})
	}
	t.Logf("validated %d data source specs", count)
}

// A dataSourceOnly spec describes a read-only API, so it must register a data
// source and no managed resource. Without this, a spec whose endpoint has no
// POST/PUT/DELETE would still expose a resource that fails on the first apply.
//
// Asserted per spec rather than on totals so a failure names the offending
// spec instead of reporting a count mismatch.
func TestDataSourceOnlySpecsRegisterNoResource(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}

	// TypeName is "<provider>_<spec name>", so the spec name is recoverable from
	// each registered factory — that is what lets this name the culprit.
	registered := func(typeNames map[string]bool, name string) bool {
		return typeNames["duploai_"+name]
	}
	resourceNames := map[string]bool{}
	for _, f := range (&duploaiProvider{}).Resources(context.Background()) {
		var resp resource.MetadataResponse
		f().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "duploai"}, &resp)
		resourceNames[resp.TypeName] = true
	}
	dataSourceNames := map[string]bool{}
	for _, f := range (&duploaiProvider{}).DataSources(context.Background()) {
		var resp datasource.MetadataResponse
		f().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "duploai"}, &resp)
		dataSourceNames[resp.TypeName] = true
	}

	var only int
	for _, spec := range specs {
		if !spec.DataSourceOnly {
			continue
		}
		only++
		t.Run(spec.Name, func(t *testing.T) {
			if registered(resourceNames, spec.Name) {
				t.Errorf("dataSourceOnly spec %q registered a managed resource; it has no create/update/delete", spec.Name)
			}
			if !registered(dataSourceNames, spec.Name) {
				t.Errorf("dataSourceOnly spec %q registered no data source", spec.Name)
			}
		})
	}
	if only == 0 {
		t.Skip("no dataSourceOnly specs embedded")
	}
}

// idPath names where an object id lives in a create response, so a
// dataSourceOnly spec — which never creates anything — must not be forced to
// declare one. Guards the validate() carve-out against a well-meaning
// simplification back to "idPath is always required".
func TestDataSourceOnlySpecNeedsNoIDPath(t *testing.T) {
	base := func() ResourceSpec {
		return ResourceSpec{
			Name:     "readonly_thing",
			Endpoint: EndpointSpec{UriBase: "/v1/things"},
			Attributes: []AttributeSpec{
				{Name: "value", Type: "string", Computed: true, APIPath: "value"},
			},
		}
	}

	s := base()
	s.DataSourceOnly = true
	if err := s.validate(); err != nil {
		t.Errorf("dataSourceOnly spec without idPath must validate, got: %v", err)
	}

	// A spec that does create objects still needs it.
	s2 := base()
	if err := s2.validate(); err == nil {
		t.Error("a creating spec without idPath must be rejected")
	}
}

// k8s_credentials reads a sub-resource action path rather than the conventional
// "/{id}", so the endpoint's read override is what makes it hit the right URL.
// A dropped override would silently GET the cluster object instead, which
// returns a shape with no token at all.
func TestK8sCredentialsReadsJitAccessPath(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, spec := range specs {
		if spec.Name != "k8s_credentials" {
			continue
		}
		if !spec.DataSourceOnly {
			t.Error("k8s_credentials mints a short-lived token and must stay dataSourceOnly")
		}
		endpoint, err := spec.BuildEndpoint()
		if err != nil {
			t.Fatalf("BuildEndpoint: %v", err)
		}
		// Credentials are minted per scope, not per cluster: the scope decides
		// which API server is reachable, which token is issued, and which
		// namespaces it may touch. The cluster route hardcodes the namespace to
		// "default", so it cannot express that.
		if got := endpoint.Read.Path; got != "/{id}/k8s/jitAccess" {
			t.Errorf("read path = %q, want %q", got, "/{id}/k8s/jitAccess")
		}
		if got := endpoint.UriBase; got != "/v1/aiservicedesk/admin/data/Scopes" {
			t.Errorf("uriBase = %q, want the Scopes collection", got)
		}
		// The scope id is the only lookup key — no workspace path parameter.
		for _, a := range spec.Attributes {
			if a.Name == "workspace_id" {
				t.Error("k8s_credentials is looked up by scope id alone; workspace_id must not be an input")
			}
		}
		// The lookup key is surfaced as "scope_id", not the generic "id": the value
		// is the cluster's Kubernetes scope, and passing the cluster id (the obvious
		// reading of "id") is a 400.
		if got := spec.lookupName(); got != "scope_id" {
			t.Errorf("lookup attribute = %q, want scope_id", got)
		}
		d := &dynamicDataSource{spec: spec, endpoint: endpoint}
		var schemaResp datasource.SchemaResponse
		d.Schema(context.Background(), datasource.SchemaRequest{}, &schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Fatalf("schema diags: %v", schemaResp.Diagnostics)
		}
		sc, ok := schemaResp.Schema.Attributes["scope_id"].(dsschema.StringAttribute)
		if !ok || !sc.Required {
			t.Error("scope_id must be a Required string attribute")
		}
		if _, present := schemaResp.Schema.Attributes["id"]; present {
			t.Error("id must not remain once the lookup key is renamed to scope_id")
		}
		return
	}
	t.Fatal("k8s_credentials spec not found")
}

// lookupAttribute only affects the generated data source, and the engine injects
// it — so a spec that declares neither data source flag, or that also declares an
// attribute of the same name, is a spec bug rather than a silent no-op.
func TestLookupAttribute_Validation(t *testing.T) {
	base := func() ResourceSpec {
		return ResourceSpec{
			Name: "x", IDPath: "id",
			Endpoint:   EndpointSpec{UriBase: "/v1/test"},
			Attributes: []AttributeSpec{{Name: "name", Type: "string", Computed: true, APIPath: "name"}},
		}
	}
	t.Run("requires a data source flag", func(t *testing.T) {
		s := base()
		s.LookupAttribute = "scope_id"
		if err := s.validate(); err == nil {
			t.Error("want error when lookupAttribute is set without dataSource/dataSourceOnly")
		}
	})
	t.Run("rejects collision with a declared attribute", func(t *testing.T) {
		s := base()
		s.DataSourceOnly = true
		s.LookupAttribute = "name"
		if err := s.validate(); err == nil {
			t.Error("want error when lookupAttribute collides with a declared attribute")
		}
	})
	t.Run("description requires the rename", func(t *testing.T) {
		s := base()
		s.DataSourceOnly = true
		s.LookupDescription = "orphaned"
		if err := s.validate(); err == nil {
			t.Error("want error when lookupDescription is set without lookupAttribute")
		}
	})
	t.Run("accepts a valid rename", func(t *testing.T) {
		s := base()
		s.DataSourceOnly = true
		s.LookupAttribute = "scope_id"
		s.LookupDescription = "The scope id."
		if err := s.validate(); err != nil {
			t.Errorf("valid rename rejected: %v", err)
		}
		if got := s.lookupName(); got != "scope_id" {
			t.Errorf("lookupName = %q", got)
		}
		if got := s.lookupDescription(); got != "The scope id." {
			t.Errorf("lookupDescription = %q", got)
		}
	})
	t.Run("defaults when unset", func(t *testing.T) {
		s := base()
		if got := s.lookupName(); got != "id" {
			t.Errorf("lookupName default = %q, want id", got)
		}
		if got := s.lookupDescription(); got != "ID of the object to look up." {
			t.Errorf("lookupDescription default = %q", got)
		}
	})
}
