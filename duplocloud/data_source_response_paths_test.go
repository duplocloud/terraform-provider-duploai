package duplocloud

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// dsPathsServer stands up the real framework server against a host that
// answers every request with body, and returns it configured.
func dsPathsServer(t *testing.T, body string) (tfprotov6.ProviderServer, *tfprotov6.GetProviderSchemaResponse) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
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
	failOnErrors(t, "ConfigureProvider", cresp.Diagnostics)
	return server, schemas
}

func failOnErrors(t *testing.T, op string, diags []*tfprotov6.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("%s: %s: %s", op, d.Summary, d.Detail)
		}
	}
}

// dsPathsObject returns a value of typ whose attributes are null except set.
func dsPathsObject(typ tftypes.Object, set map[string]tftypes.Value) tftypes.Value {
	vals := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		vals[n] = tftypes.NewValue(at, nil)
	}
	for n, v := range set {
		vals[n] = v
	}
	return tftypes.NewValue(typ, vals)
}

func readDataSource(t *testing.T, server tfprotov6.ProviderServer, schemas *tfprotov6.GetProviderSchemaResponse, name string) map[string]tftypes.Value {
	t.Helper()
	sch, ok := schemas.DataSourceSchemas[name]
	if !ok {
		t.Fatalf("data source %s is not registered", name)
	}
	typ := sch.ValueType().(tftypes.Object)
	cfgVal := dsPathsObject(typ, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, "obj1"),
		"workspace_id": tftypes.NewValue(tftypes.String, "ws1"),
	})
	cfg, err := tfprotov6.NewDynamicValue(typ, cfgVal)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.ReadDataSource(context.Background(), &tfprotov6.ReadDataSourceRequest{TypeName: name, Config: &cfg})
	if err != nil {
		t.Fatal(err)
	}
	failOnErrors(t, "ReadDataSource "+name, resp.Diagnostics)
	got, err := resp.State.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]tftypes.Value{}
	if err := got.As(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func readResource(t *testing.T, server tfprotov6.ProviderServer, schemas *tfprotov6.GetProviderSchemaResponse, name string, set map[string]tftypes.Value) map[string]tftypes.Value {
	t.Helper()
	typ := schemas.ResourceSchemas[name].ValueType().(tftypes.Object)
	base := map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, "ws1/obj1"),
		"workspace_id": tftypes.NewValue(tftypes.String, "ws1"),
	}
	for k, v := range set {
		base[k] = v
	}
	state, err := tfprotov6.NewDynamicValue(typ, dsPathsObject(typ, base))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{TypeName: name, CurrentState: &state})
	if err != nil {
		t.Fatal(err)
	}
	failOnErrors(t, "ReadResource "+name, resp.Diagnostics)
	got, err := resp.NewState.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]tftypes.Value{}
	if err := got.As(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// F-23: v0.6.7's data.duploai_native_host exposed metadata_hop_limit. The
// resource stopped reading it back (a create-only value with no update: an
// out-of-band change must not become drift), and request-only attributes are
// dropped from the data source — so configs referencing
// data.duploai_native_host.x.metadata_hop_limit broke. The data source reads it
// again; the resource still does not.
func TestDataSourceResponsePaths_NativeHostMetadataHopLimit(t *testing.T) {
	server, schemas := dsPathsServer(t, `{"data":{"id":"obj1","name":"h1","status":"Complete","spec":{"metadataHopLimit":3}}}`)

	ds := readDataSource(t, server, schemas, "duploai_native_host")
	got, ok := ds["metadata_hop_limit"]
	if !ok {
		t.Fatal("data source duploai_native_host has no metadata_hop_limit (v0.6.7 had it)")
	}
	if !got.Equal(tftypes.NewValue(tftypes.Number, big.NewFloat(3))) {
		t.Errorf("data source metadata_hop_limit = %v, want 3 from spec.metadataHopLimit", got)
	}

	// The resource keeps the value in state: no read-back, so no drift and no
	// in-place diff it could never apply.
	res := readResource(t, server, schemas, "duploai_native_host", map[string]tftypes.Value{
		"metadata_hop_limit": tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
	})
	if !res["metadata_hop_limit"].Equal(tftypes.NewValue(tftypes.Number, big.NewFloat(1))) {
		t.Errorf("resource metadata_hop_limit after refresh = %v, want the prior 1 (must not be read back)", res["metadata_hop_limit"])
	}
}

// F-21: the k8s_pvc / k8s_resource_quota / k8s_storage_class data sources
// returned labels and annotations as null. They now report the live metadata
// verbatim; the resources keep exactly what state holds (null stays null, a
// declared map is not widened by server-added keys).
func TestDataSourceResponsePaths_K8sMetadata(t *testing.T) {
	body := `{"data":{"id":"obj1","name":"x","status":"Complete","spec":{"k8sResource":{"metadata":{"name":"x"}}},` +
		`"result":{"k8sResource":{"metadata":{"name":"x","labels":{"qa":"b","server":"added"},"annotations":{"note":"n"}}}}}}`
	server, schemas := dsPathsServer(t, body)
	strMap := func(m map[string]string) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for k, v := range m {
			vals[k] = tftypes.NewValue(tftypes.String, v)
		}
		return tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, vals)
	}
	nullMap := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)

	for _, name := range []string{"duploai_k8s_pvc", "duploai_k8s_resource_quota", "duploai_k8s_storage_class"} {
		t.Run(name, func(t *testing.T) {
			ds := readDataSource(t, server, schemas, name)
			if want := strMap(map[string]string{"qa": "b", "server": "added"}); !ds["labels"].Equal(want) {
				t.Errorf("data source labels = %v, want %v", ds["labels"], want)
			}
			if want := strMap(map[string]string{"note": "n"}); !ds["annotations"].Equal(want) {
				t.Errorf("data source annotations = %v, want %v", ds["annotations"], want)
			}

			// Existing state without labels stays without them.
			res := readResource(t, server, schemas, name, nil)
			if !res["labels"].Equal(nullMap) || !res["annotations"].Equal(nullMap) {
				t.Errorf("resource refresh with no labels in state: labels=%v annotations=%v, want both null", res["labels"], res["annotations"])
			}
			// Existing state with labels keeps exactly those.
			declared := strMap(map[string]string{"qa": "b"})
			res = readResource(t, server, schemas, name, map[string]tftypes.Value{"labels": declared})
			if !res["labels"].Equal(declared) {
				t.Errorf("resource refresh with declared labels = %v, want %v unchanged", res["labels"], declared)
			}
		})
	}
}

// dataSourceResponsePaths is consumed only by the generated data source, so it
// is rejected where it would be a silent no-op.
func TestDataSourceResponsePaths_Validation(t *testing.T) {
	base := func() ResourceSpec {
		return ResourceSpec{
			Name: "widget", IDPath: "id", DataSource: true,
			Endpoint: EndpointSpec{UriBase: "/v1/test/widgets"},
			Attributes: []AttributeSpec{
				{Name: "name", Type: "string", Required: true, APIPath: "name"},
				{Name: "hop", Type: "int", Optional: true, RequestPath: "spec.hop", DataSourceResponsePaths: []string{"spec.hop"}},
			},
		}
	}
	if err := func() error { s := base(); return s.validate() }(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}

	noDS := base()
	noDS.DataSource = false
	if err := noDS.validate(); err == nil {
		t.Error("dataSourceResponsePaths without dataSource must be rejected")
	}

	nested := base()
	nested.Attributes = append(nested.Attributes, AttributeSpec{
		Name: "obj", Type: "object", Optional: true, APIPath: "obj",
		Attributes: []AttributeSpec{{Name: "x", Type: "string", Optional: true, DataSourceResponsePaths: []string{"x"}}},
	})
	if err := nested.validate(); err == nil {
		t.Error("dataSourceResponsePaths on a nested attribute must be rejected")
	}

	empty := base()
	empty.Attributes[1].DataSourceResponsePaths = []string{""}
	if err := empty.validate(); err == nil {
		t.Error("an empty dataSourceResponsePaths entry must be rejected")
	}
}
