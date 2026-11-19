package duplocloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A resource deleted outside Terraform must plan a recreate, not fail. Through
// the real framework server: Terraform below 1.12 (or state saved before
// identity support) sends no identity, the API answers 404, Read removes the
// resource — and the framework still errors "Missing Resource Identity After
// Read" unless Read returns an identity anyway.
func TestReadResource_RemovedWithoutPriorIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"message":"not found"}`))
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}

	provType := schemas.Provider.ValueType().(tftypes.Object)
	pvals := map[string]tftypes.Value{}
	for n, typ := range provType.AttributeTypes {
		pvals[n] = tftypes.NewValue(typ, nil)
	}
	pvals["duplo_host"] = tftypes.NewValue(tftypes.String, srv.URL)
	pvals["duplo_token"] = tftypes.NewValue(tftypes.String, "tok")
	pcfg, err := tfprotov6.NewDynamicValue(provType, tftypes.NewValue(provType, pvals))
	if err != nil {
		t.Fatal(err)
	}
	cresp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &pcfg})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range cresp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("ConfigureProvider: %s: %s", d.Summary, d.Detail)
		}
	}

	const name = "duploai_resource_group"
	typ := schemas.ResourceSchemas[name].ValueType().(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		vals[n] = tftypes.NewValue(at, nil)
	}
	vals["id"] = tftypes.NewValue(tftypes.String, "ws1/obj1")
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws1")
	state, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, vals))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: name, CurrentState: &state})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("ReadResource: %s: %s", d.Summary, d.Detail)
		}
	}
	got, err := resp.NewState.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNull() {
		t.Errorf("a 404 must remove the resource (null state), got %v", got)
	}
}
