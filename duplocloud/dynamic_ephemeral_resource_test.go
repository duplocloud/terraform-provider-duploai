package duplocloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	ephschema "github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// k8s_credentials is also registered as an ephemeral resource: same attributes
// as the data source (token still sensitive), and Open returns what the data
// source would read — but Terraform never stores it.
func TestEphemeralK8sCredentials(t *testing.T) {
	ctx := context.Background()
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath, gotMethod = req.URL.Path, req.Method
		_, _ = w.Write([]byte(`{"data":{"apiServer":"https://k8s.example","token":"tok-123","certificateAuthorityDataBase64":"Q0E=","defaultNamespace":"ns1","name":"c1"}}`))
	}))
	defer srv.Close()
	client, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	p := New().(*duploaiProvider)
	var eph ephemeral.EphemeralResource
	for _, f := range p.EphemeralResources(ctx) {
		r := f()
		var m ephemeral.MetadataResponse
		r.Metadata(ctx, ephemeral.MetadataRequest{ProviderTypeName: "duploai"}, &m)
		if m.TypeName == "duploai_k8s_credentials" {
			eph = r
		}
	}
	if eph == nil {
		t.Fatal("duploai_k8s_credentials is not registered as an ephemeral resource")
	}
	eph.(ephemeral.EphemeralResourceWithConfigure).Configure(ctx, ephemeral.ConfigureRequest{ProviderData: client}, &ephemeral.ConfigureResponse{})

	var sr ephemeral.SchemaResponse
	eph.Schema(ctx, ephemeral.SchemaRequest{}, &sr)
	ds := eph.(*dynamicEphemeralResource).ds
	var dsr datasource.SchemaResponse
	ds.Schema(ctx, datasource.SchemaRequest{}, &dsr)
	if len(sr.Schema.Attributes) != len(dsr.Schema.Attributes) {
		t.Errorf("ephemeral schema has %d attributes, data source %d", len(sr.Schema.Attributes), len(dsr.Schema.Attributes))
	}
	if tok, ok := sr.Schema.Attributes["token"].(ephschema.StringAttribute); !ok || !tok.Sensitive || !tok.Computed {
		t.Errorf("token must stay a computed, sensitive string, got %#v", sr.Schema.Attributes["token"])
	}
	if sc, ok := sr.Schema.Attributes["scope_id"].(ephschema.StringAttribute); !ok || !sc.Required {
		t.Error("scope_id must be the required lookup key")
	}

	typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		vals[n] = tftypes.NewValue(at, nil)
	}
	vals["scope_id"] = tftypes.NewValue(tftypes.String, "scope-1")
	cfg := tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(typ, vals)}
	resp := &ephemeral.OpenResponse{Result: tfsdk.EphemeralResultData{Schema: sr.Schema}}
	eph.Open(ctx, ephemeral.OpenRequest{Config: cfg}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Open: %v", resp.Diagnostics)
	}
	if gotPath != "/v1/aiservicedesk/admin/data/Scopes/scope-1/k8s/jitAccess" || gotMethod != http.MethodPost {
		t.Errorf("read = %s %q, want POST to the jitAccess route", gotMethod, gotPath)
	}
	var out map[string]tftypes.Value
	if err := resp.Result.Raw.As(&out); err != nil {
		t.Fatal(err)
	}
	if !out["token"].Equal(tftypes.NewValue(tftypes.String, "tok-123")) || !out["endpoint"].Equal(tftypes.NewValue(tftypes.String, "https://k8s.example")) {
		t.Errorf("Open result = %v", out)
	}
}
