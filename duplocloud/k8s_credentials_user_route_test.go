package duplocloud

// duploai_k8s_credentials: optional workspace_id selects the least-privilege
// USER jitAccess route; unset keeps today's ADMIN route byte-for-byte.
//
// Generic, opt-in spec feature these tests pin (top-level spec key):
//
//	"alternateReadEndpoints": [
//	  {
//	    "whenSet": "workspace_id",
//	    "uriBase": "/v1/aiservicedesk/user/data/Workspaces/{workspace_id}/scopes",
//	    "read":    { "verb": "POST", "path": "/{id}/k8s/jitAccess" }
//	  }
//	]
//
// and the selector declared as an ordinary input attribute (no apiPath):
//
//	{ "name": "workspace_id", "type": "string", "optional": true, "description": "..." }
//
// Semantics:
//   - Entries are evaluated in order; the first whose whenSet attribute is
//     known and non-null in config replaces endpoint.uriBase + endpoint.read
//     for the read. None matching → the primary endpoint, unchanged.
//   - whenSet must name a declared attribute; every {placeholder} in uriBase
//     and read.path must be "id" (the lookup key) or a declared string
//     attribute. Those placeholders are Optional (not Required) on the data
//     source schema, and are echoed into state as configured (null if unset).
//   - Only valid with dataSource or dataSourceOnly; anything else fails at
//     spec load (validate()).
//   - read is required in each entry (verb + path).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	ephschema "github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

const (
	k8sCredAdminPath = "/v1/aiservicedesk/admin/data/Scopes/scope-1/k8s/jitAccess"
	k8sCredUserPath  = "/v1/aiservicedesk/user/data/Workspaces/ws-1/scopes/scope-1/k8s/jitAccess"
	k8sCredBody      = `{"data":{"apiServer":"https://k8s.example","token":"tok-123","certificateAuthorityDataBase64":"Q0E=","defaultNamespace":"ns1","name":"c1"}}`
)

type k8sCredCall struct{ method, path, query, body string }

func k8sCredServer(t *testing.T) (*duplosdk.Client, *[]k8sCredCall) {
	t.Helper()
	var calls []k8sCredCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b := new(strings.Builder)
		if req.Body != nil {
			buf := make([]byte, 4096)
			n, _ := req.Body.Read(buf)
			b.Write(buf[:n])
		}
		calls = append(calls, k8sCredCall{req.Method, req.URL.Path, req.URL.RawQuery, b.String()})
		_, _ = w.Write([]byte(k8sCredBody))
	}))
	t.Cleanup(srv.Close)
	c, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c, &calls
}

func k8sCredDataSource(t *testing.T, client *duplosdk.Client) datasource.DataSource {
	t.Helper()
	ctx := context.Background()
	p := New().(*duploaiProvider)
	for _, f := range p.DataSources(ctx) {
		d := f()
		var m datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "duploai"}, &m)
		if m.TypeName == "duploai_k8s_credentials" {
			if client != nil {
				d.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: client}, &datasource.ConfigureResponse{})
			}
			return d
		}
	}
	t.Fatal("duploai_k8s_credentials data source not registered")
	return nil
}

func k8sCredEphemeral(t *testing.T, client *duplosdk.Client) ephemeral.EphemeralResource {
	t.Helper()
	ctx := context.Background()
	p := New().(*duploaiProvider)
	for _, f := range p.EphemeralResources(ctx) {
		r := f()
		var m ephemeral.MetadataResponse
		r.Metadata(ctx, ephemeral.MetadataRequest{ProviderTypeName: "duploai"}, &m)
		if m.TypeName == "duploai_k8s_credentials" {
			if client != nil {
				r.(ephemeral.EphemeralResourceWithConfigure).Configure(ctx, ephemeral.ConfigureRequest{ProviderData: client}, &ephemeral.ConfigureResponse{})
			}
			return r
		}
	}
	t.Fatal("duploai_k8s_credentials ephemeral resource not registered")
	return nil
}

func k8sCredConfig(t *testing.T, typ tftypes.Object, ws *string) tftypes.Value {
	t.Helper()
	if _, ok := typ.AttributeTypes["workspace_id"]; !ok {
		t.Fatal("schema has no workspace_id attribute (feature not implemented)")
	}
	vals := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		vals[n] = tftypes.NewValue(at, nil)
	}
	vals["scope_id"] = tftypes.NewValue(tftypes.String, "scope-1")
	if ws != nil {
		vals["workspace_id"] = tftypes.NewValue(tftypes.String, *ws)
	}
	return tftypes.NewValue(typ, vals)
}

func k8sCredCheckOut(t *testing.T, raw tftypes.Value, ws *string) {
	t.Helper()
	var out map[string]tftypes.Value
	if err := raw.As(&out); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"endpoint": "https://k8s.example", "token": "tok-123", "ca_certificate_data_base64": "Q0E=", "namespace": "ns1", "name": "c1", "scope_id": "scope-1"}
	for k, v := range want {
		if !out[k].Equal(tftypes.NewValue(tftypes.String, v)) {
			t.Errorf("%s = %v, want %q", k, out[k], v)
		}
	}
	var wantWS tftypes.Value
	if ws == nil {
		wantWS = tftypes.NewValue(tftypes.String, nil)
	} else {
		wantWS = tftypes.NewValue(tftypes.String, *ws)
	}
	if !out["workspace_id"].Equal(wantWS) {
		t.Errorf("workspace_id = %v, want %v (echoed as configured)", out["workspace_id"], wantWS)
	}
}

func k8sCredCheckCalls(t *testing.T, calls []k8sCredCall, wantPath string) {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("got %d HTTP calls %+v, want exactly 1", len(calls), calls)
	}
	c := calls[0]
	if c.method != http.MethodPost || c.path != wantPath || c.query != "" {
		t.Errorf("request = %s %s?%s, want POST %s", c.method, c.path, c.query, wantPath)
	}
}

// 1. Schema: workspace_id Optional-only string on both; scope_id still Required.
func TestK8sCredentialsUserRoute_Schema(t *testing.T) {
	ctx := context.Background()
	var dsr datasource.SchemaResponse
	k8sCredDataSource(t, nil).Schema(ctx, datasource.SchemaRequest{}, &dsr)
	ws, ok := dsr.Schema.Attributes["workspace_id"].(dsschema.StringAttribute)
	if !ok || !ws.Optional || ws.Required || ws.Computed || ws.Sensitive {
		t.Errorf("data source workspace_id must be an Optional (not Required/Computed) string, got %#v", dsr.Schema.Attributes["workspace_id"])
	}
	if ok && !strings.Contains(ws.Description+ws.MarkdownDescription, "workspace") {
		t.Errorf("workspace_id needs a description, got %q", ws.Description)
	}
	if sc, ok := dsr.Schema.Attributes["scope_id"].(dsschema.StringAttribute); !ok || !sc.Required {
		t.Error("scope_id must remain the Required lookup key")
	}

	var er ephemeral.SchemaResponse
	k8sCredEphemeral(t, nil).Schema(ctx, ephemeral.SchemaRequest{}, &er)
	ews, ok := er.Schema.Attributes["workspace_id"].(ephschema.StringAttribute)
	if !ok || !ews.Optional || ews.Required || ews.Computed {
		t.Errorf("ephemeral workspace_id must be an Optional string, got %#v", er.Schema.Attributes["workspace_id"])
	}
	if sc, ok := er.Schema.Attributes["scope_id"].(ephschema.StringAttribute); !ok || !sc.Required {
		t.Error("ephemeral scope_id must remain Required")
	}
}

// 1b. Nothing but the added optional workspace_id differs from the schema the
// same spec produced before this change (the spec minus alternateReadEndpoints
// and workspace_id is exactly the released k8s_credentials spec).
func TestK8sCredentialsUserRoute_SchemaOnlyAddsWorkspaceID(t *testing.T) {
	ctx := context.Background()
	data, err := specFS.ReadFile("specs/k8s_credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "alternateReadEndpoints")
	var kept []any
	for _, a := range raw["attributes"].([]any) {
		if a.(map[string]any)["name"] != "workspace_id" {
			kept = append(kept, a)
		}
	}
	raw["attributes"] = kept
	before, _ := json.Marshal(raw)
	var spec ResourceSpec
	if err := json.Unmarshal(before, &spec); err != nil {
		t.Fatal(err)
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("pre-change spec no longer valid: %v", err)
	}
	ep, err := spec.BuildEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	var baseResp datasource.SchemaResponse
	newDynamicDataSourceFactory(spec, ep)().Schema(ctx, datasource.SchemaRequest{}, &baseResp)
	var curResp datasource.SchemaResponse
	k8sCredDataSource(t, nil).Schema(ctx, datasource.SchemaRequest{}, &curResp)

	base, cur := baseResp.Schema.Attributes, curResp.Schema.Attributes
	for name, b := range base {
		c, ok := cur[name]
		if !ok {
			t.Errorf("%s removed", name)
			continue
		}
		if c.IsRequired() != b.IsRequired() || c.IsOptional() != b.IsOptional() ||
			c.IsComputed() != b.IsComputed() || c.IsSensitive() != b.IsSensitive() ||
			!c.GetType().Equal(b.GetType()) {
			t.Errorf("%s changed: before %#v, now %#v", name, b, c)
		}
	}
	for name, c := range cur {
		if _, ok := base[name]; ok {
			continue
		}
		if name != "workspace_id" {
			t.Errorf("unexpected new attribute %s", name)
			continue
		}
		if !c.IsOptional() || c.IsRequired() || c.IsComputed() {
			t.Errorf("workspace_id must be optional only, got %#v", c)
		}
	}
	if _, ok := cur["workspace_id"]; !ok {
		t.Error("workspace_id missing from current data source schema")
	}
}

// 2. Routing via the data source.
func TestK8sCredentialsUserRoute_DataSourceRead(t *testing.T) {
	ws := "ws-1"
	for _, tc := range []struct {
		name string
		ws   *string
		path string
	}{
		{"workspace_set_user_route", &ws, k8sCredUserPath},
		{"workspace_unset_admin_route", nil, k8sCredAdminPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client, calls := k8sCredServer(t)
			d := k8sCredDataSource(t, client)
			var sr datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &sr)
			typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
			cfg := tfsdk.Config{Schema: sr.Schema, Raw: k8sCredConfig(t, typ, tc.ws)}
			resp := &datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
			d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Read: %v", resp.Diagnostics)
			}
			k8sCredCheckCalls(t, *calls, tc.path)
			k8sCredCheckOut(t, resp.State.Raw, tc.ws)
		})
	}
}

// 2b. Routing via the ephemeral Open.
func TestK8sCredentialsUserRoute_EphemeralOpen(t *testing.T) {
	ws := "ws-1"
	for _, tc := range []struct {
		name string
		ws   *string
		path string
	}{
		{"workspace_set_user_route", &ws, k8sCredUserPath},
		{"workspace_unset_admin_route", nil, k8sCredAdminPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client, calls := k8sCredServer(t)
			e := k8sCredEphemeral(t, client)
			var sr ephemeral.SchemaResponse
			e.Schema(ctx, ephemeral.SchemaRequest{}, &sr)
			typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
			cfg := tfsdk.Config{Schema: sr.Schema, Raw: k8sCredConfig(t, typ, tc.ws)}
			resp := &ephemeral.OpenResponse{Result: tfsdk.EphemeralResultData{Schema: sr.Schema}}
			e.Open(ctx, ephemeral.OpenRequest{Config: cfg}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Open: %v", resp.Diagnostics)
			}
			k8sCredCheckCalls(t, *calls, tc.path)
			k8sCredCheckOut(t, resp.Result.Raw, tc.ws)
		})
	}
}

// 4. Spec validation for alternateReadEndpoints. Specs are parsed from JSON so
// this file compiles before the Go field exists; before the feature the key
// is silently ignored and the invalid cases wrongly load.
func altReadSpec(t *testing.T, mutate func(m map[string]any)) []byte {
	t.Helper()
	m := map[string]any{
		"name":            "alt_read",
		"dataSourceOnly":  true,
		"lookupAttribute": "scope_id",
		"endpoint": map[string]any{
			"uriBase": "/v1/admin/Scopes",
			"read":    map[string]any{"verb": "POST", "path": "/{id}/x"},
		},
		"alternateReadEndpoints": []any{map[string]any{
			"whenSet": "workspace_id",
			"uriBase": "/v1/user/Workspaces/{workspace_id}/scopes",
			"read":    map[string]any{"verb": "POST", "path": "/{id}/x"},
		}},
		"attributes": []any{
			map[string]any{"name": "workspace_id", "type": "string", "optional": true, "description": "ws"},
			map[string]any{"name": "out", "type": "string", "computed": true, "apiPath": "out"},
		},
	}
	if mutate != nil {
		mutate(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func altReadValidate(data []byte) error {
	var s ResourceSpec
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return s.validate()
}

func altEntry(m map[string]any) map[string]any {
	return m["alternateReadEndpoints"].([]any)[0].(map[string]any)
}

func TestAlternateReadEndpoints_Validation(t *testing.T) {
	if err := altReadValidate(altReadSpec(t, nil)); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	bad := map[string]func(m map[string]any){
		"unknown_whenSet": func(m map[string]any) { altEntry(m)["whenSet"] = "nope" },
		"placeholder_without_attribute": func(m map[string]any) {
			altEntry(m)["uriBase"] = "/v1/user/Workspaces/{workspace_id}/{tenant}/scopes"
		},
		"placeholder_in_read_path_without_attribute": func(m map[string]any) {
			altEntry(m)["read"] = map[string]any{"verb": "POST", "path": "/{id}/{missing}"}
		},
		"without_dataSource": func(m map[string]any) {
			delete(m, "dataSourceOnly")
			delete(m, "lookupAttribute")
			m["idPath"] = "id"
		},
		"missing_read":  func(m map[string]any) { delete(altEntry(m), "read") },
		"empty_uriBase": func(m map[string]any) { altEntry(m)["uriBase"] = "" },
	}
	for name, mut := range bad {
		t.Run(name, func(t *testing.T) {
			if err := altReadValidate(altReadSpec(t, mut)); err == nil {
				t.Error("misconfigured alternateReadEndpoints loaded without error; want a spec-load error")
			}
		})
	}
}

// Feature is opt-in: only k8s_credentials uses it, and the alternate route's
// params become Optional on a synthetic spec.
func TestAlternateReadEndpoints_OptInOnly(t *testing.T) {
	entries, err := specFS.ReadDir("specs")
	if err != nil {
		t.Fatal(err)
	}
	used := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := specFS.ReadFile("specs/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if _, ok := raw["alternateReadEndpoints"]; ok {
			if e.Name() != "k8s_credentials.json" {
				t.Errorf("%s unexpectedly uses alternateReadEndpoints", e.Name())
			}
			used = true
		}
	}
	if !used {
		t.Error("k8s_credentials.json does not declare alternateReadEndpoints")
	}
}

// Every other data source still requires exactly what it did before the
// feature: its lookup key plus its primary endpoint's path parameters, and no
// optional inputs appear (no side effects from the new feature).
func TestAlternateReadEndpoints_OtherDataSourcesUnchanged(t *testing.T) {
	ctx := context.Background()
	p := New().(*duploaiProvider)
	specs := map[string]ResourceSpec{}
	entries, err := specFS.ReadDir("specs")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := specFS.ReadFile("specs/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var s ResourceSpec
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatal(err)
		}
		specs["duploai_"+s.Name] = s
	}
	checked := 0
	for _, f := range p.DataSources(ctx) {
		d := f()
		var m datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "duploai"}, &m)
		if m.TypeName == "duploai_k8s_credentials" {
			continue
		}
		s, ok := specs[m.TypeName]
		if !ok {
			continue
		}
		ep, err := s.BuildEndpoint()
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]bool{s.lookupName(): true}
		for _, pp := range ep.PathParams() {
			want[pp] = true
		}
		var r datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &r)
		for an, a := range r.Schema.Attributes {
			if a.IsRequired() != want[an] {
				t.Errorf("%s.%s required=%v, want %v", m.TypeName, an, a.IsRequired(), want[an])
			}
			if a.IsOptional() {
				t.Errorf("%s.%s unexpectedly optional", m.TypeName, an)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no data sources checked")
	}
}

func TestAlternateReadEndpoints_SyntheticSchemaOptional(t *testing.T) {
	var s ResourceSpec
	if err := json.Unmarshal(altReadSpec(t, nil), &s); err != nil {
		t.Fatal(err)
	}
	if err := s.validate(); err != nil {
		t.Fatal(err)
	}
	ep, err := s.BuildEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	d := &dynamicDataSource{spec: s, endpoint: ep}
	var r datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &r)
	ws, ok := r.Schema.Attributes["workspace_id"].(dsschema.StringAttribute)
	if !ok || !ws.Optional || ws.Required {
		t.Errorf("alternate-route param workspace_id must be Optional on the data source, got %#v", r.Schema.Attributes["workspace_id"])
	}
}

// Terraform core never calls ReadDataSource / OpenEphemeralResource with an
// unknown config value (it defers the read to apply / returns an unknown
// result itself). Defense in depth: if the engine were ever invoked with an
// unknown whenSet attribute, it must not fall back to the admin route — a
// non-admin token would get 403 for a config that does set workspace_id.
func TestK8sCredentialsUserRoute_UnknownWorkspaceNeverHitsAdmin(t *testing.T) {
	ctx := context.Background()
	client, calls := k8sCredServer(t)
	d := k8sCredDataSource(t, client)
	var sr datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sr)
	typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	raw := k8sCredConfig(t, typ, nil)
	var vals map[string]tftypes.Value
	_ = raw.As(&vals)
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	cfg := tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(typ, vals)}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	for _, c := range *calls {
		if c.path == k8sCredAdminPath {
			t.Errorf("unknown workspace_id fell back to the admin route %s; want an error (or no call)", c.path)
		}
	}
}

// Review feedback: workspace_id = "" (a variable defaulting to empty)
// passed the null check and called the user route with an empty workspace
// segment, and the backend's error never mentioned workspace_id. An empty or
// blank value must be a plan-readable error on workspace_id with no request.
func TestK8sCredentialsUserRoute_EmptyWorkspaceIsAnError(t *testing.T) {
	ctx := context.Background()
	for _, v := range []string{"", "   "} {
		t.Run("data_source/"+strings.ReplaceAll(v, " ", "_"), func(t *testing.T) {
			client, calls := k8sCredServer(t)
			d := k8sCredDataSource(t, client)
			var sr datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &sr)
			typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
			ws := v
			cfg := tfsdk.Config{Schema: sr.Schema, Raw: k8sCredConfig(t, typ, &ws)}
			resp := &datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
			d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
			k8sCredExpectEmptyWorkspaceError(t, resp.Diagnostics.Errors(), *calls)
		})
		t.Run("ephemeral/"+strings.ReplaceAll(v, " ", "_"), func(t *testing.T) {
			client, calls := k8sCredServer(t)
			e := k8sCredEphemeral(t, client)
			var sr ephemeral.SchemaResponse
			e.Schema(ctx, ephemeral.SchemaRequest{}, &sr)
			typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
			ws := v
			cfg := tfsdk.Config{Schema: sr.Schema, Raw: k8sCredConfig(t, typ, &ws)}
			resp := &ephemeral.OpenResponse{Result: tfsdk.EphemeralResultData{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
			e.Open(ctx, ephemeral.OpenRequest{Config: cfg}, resp)
			k8sCredExpectEmptyWorkspaceError(t, resp.Diagnostics.Errors(), *calls)
		})
	}
}

func k8sCredExpectEmptyWorkspaceError(t *testing.T, errs diag.Diagnostics, calls []k8sCredCall) {
	t.Helper()
	if len(calls) != 0 {
		t.Errorf("empty workspace_id sent %d request(s) %+v; want none", len(calls), calls)
	}
	found := false
	for _, e := range errs {
		if wp, ok := e.(diag.DiagnosticWithPath); ok && wp.Path().Equal(path.Root("workspace_id")) &&
			strings.Contains(e.Summary()+e.Detail(), "workspace_id") {
			found = true
		}
	}
	if !found {
		t.Errorf("want an error on attribute workspace_id that names it, got %v", errs)
	}
}
