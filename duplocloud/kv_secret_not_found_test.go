package duplocloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// kvSecretAPI answers every call with one fixed error, like the Key Vault
// secret API does for a secret deleted out of band (F-11).
type kvSecretAPI struct {
	mu     sync.Mutex
	status int
	body   string
	calls  int
}

func (a *kvSecretAPI) handler(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	w.WriteHeader(a.status)
	_, _ = w.Write([]byte(a.body))
}

// kvSecretResource wires the REAL azure_key_vault_secret spec to the fake API
// and returns the resource plus a state for one existing secret.
func kvSecretResource(t *testing.T, api *kvSecretAPI) (*dynamicResource, tfsdk.State) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(srv.Close)

	spec := *specByName(t, "azure_key_vault_secret")
	ep, err := spec.BuildEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	client, err := duplosdk.NewClient(srv.URL, "tok", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := &dynamicResource{spec: spec, endpoint: ep}
	r.Client = client

	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(typ, nil)
	}
	vals["id"] = tftypes.NewValue(tftypes.String, "ws-1/kv-1/qa-secret")
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws-1")
	vals["key_vault_id"] = tftypes.NewValue(tftypes.String, "kv-1")
	vals["name"] = tftypes.NewValue(tftypes.String, "qa-secret")
	return r, tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(objType, vals)}
}

const kvSecretMissing = `{"Message":"Secret 'qa-secret' was not found in Key Vault 'tf-kv'."}`

func TestKVSecret_RefreshDropsSecretDeletedOutOfBand(t *testing.T) {
	api := &kvSecretAPI{status: http.StatusBadRequest, body: kvSecretMissing}
	r, state := kvSecretResource(t, api)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("refresh failed instead of dropping the missing secret: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("missing secret was kept in state; want it removed so the next plan recreates it")
	}
}

func TestKVSecret_DestroySucceedsForSecretDeletedOutOfBand(t *testing.T) {
	api := &kvSecretAPI{status: http.StatusBadRequest, body: kvSecretMissing}
	r, state := kvSecretResource(t, api)
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("destroy failed for an already-deleted secret: %v", resp.Diagnostics)
	}
}

func TestKVSecret_ValidationErrorStillFailsRefreshAndDestroy(t *testing.T) {
	api := &kvSecretAPI{status: http.StatusBadRequest, body: `{"Message":"Value cannot be null. (Parameter 'Version')"}`}
	r, state := kvSecretResource(t, api)

	rr := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &rr)
	if !rr.Diagnostics.HasError() {
		t.Error("refresh swallowed a real 400")
	}
	if rr.State.Raw.IsNull() {
		t.Error("refresh removed the secret from state on a real 400")
	}

	dr := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &dr)
	if !dr.Diagnostics.HasError() {
		t.Error("destroy swallowed a real 400")
	}
}
