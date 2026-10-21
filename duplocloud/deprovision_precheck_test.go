package duplocloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// fakeDeprovisionAPI serves one deprovisionable record. statuses is the status
// sequence returned by successive GETs (the last one repeats); gone makes GET
// answer 404. Deprovision answers 400 like the namespace API does for a record
// whose spec was stripped by an out-of-band delete, so any call to it fails the
// test's Delete.
type fakeDeprovisionAPI struct {
	mu          sync.Mutex
	statuses    []string
	gone        bool
	deprovision int
	deletes     int
}

func (f *fakeDeprovisionAPI) handler(w http.ResponseWriter, req *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/deprovision"):
		f.deprovision++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"message":"Invalid state","errors":"Namespace 'ns-1' has no resourceGroupId on its spec."}`))
	case req.Method == http.MethodDelete:
		f.deletes++
		f.gone = true
		w.WriteHeader(http.StatusOK)
	case req.Method == http.MethodGet:
		if f.gone {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		status := f.statuses[0]
		if len(f.statuses) > 1 {
			f.statuses = f.statuses[1:]
		}
		_, _ = w.Write([]byte(`{"data":{"id":"ns-1","status":"` + status + `"}}`))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// deprovisionDelete runs the engine's Delete for a k8s_namespace-shaped spec
// against the fake API and returns the diagnostics' error state.
func deprovisionDelete(t *testing.T, f *fakeDeprovisionAPI) bool {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()

	spec := ResourceSpec{
		Name: "k8s_namespace", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/ws/{workspace_id}/Namespaces", Deprovision: &OperationSpec{}},
		Attributes: []AttributeSpec{
			{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
		},
		Waiter: &WaiterSpec{DeprovisionedState: "DeProvisioned", PollIntervalSeconds: 1},
	}
	applyWaiterDefaults(spec.Waiter)
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
	vals["id"] = tftypes.NewValue(tftypes.String, "ws-1/ns-1")
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws-1")
	state := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(objType, vals)}

	resp := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Logf("Delete diags: %v", resp.Diagnostics)
	}
	return resp.Diagnostics.HasError()
}

// A record already deprovisioned out of band (deleted from the UI) must not be
// deprovisioned again — the API refuses it — but still deleted.
func TestDelete_SkipsDeprovisionWhenAlreadyDeprovisioned(t *testing.T) {
	f := &fakeDeprovisionAPI{statuses: []string{"DeProvisioned"}}
	if deprovisionDelete(t, f) {
		t.Fatal("Delete should succeed for an already-deprovisioned record")
	}
	if f.deprovision != 0 {
		t.Errorf("deprovision called %d times, want 0", f.deprovision)
	}
	if f.deletes != 1 {
		t.Errorf("delete called %d times, want 1", f.deletes)
	}
}

// A deprovision already in flight is waited on, not re-triggered.
func TestDelete_WaitsOnInFlightDeprovision(t *testing.T) {
	f := &fakeDeprovisionAPI{statuses: []string{"DeProvisioning", "DeProvisioning", "DeProvisioned"}}
	if deprovisionDelete(t, f) {
		t.Fatal("Delete should succeed once the in-flight deprovision finishes")
	}
	if f.deprovision != 0 {
		t.Errorf("deprovision called %d times, want 0", f.deprovision)
	}
	if f.deletes != 1 {
		t.Errorf("delete called %d times, want 1", f.deletes)
	}
}

// A record that is already gone needs neither deprovision nor delete.
func TestDelete_AlreadyGone(t *testing.T) {
	f := &fakeDeprovisionAPI{statuses: []string{"Complete"}, gone: true}
	if deprovisionDelete(t, f) {
		t.Fatal("Delete should succeed for a record that no longer exists")
	}
	if f.deprovision != 0 || f.deletes != 0 {
		t.Errorf("deprovision=%d delete=%d, want 0/0", f.deprovision, f.deletes)
	}
}

// A live record is still deprovisioned first — here the fake refuses, so the
// error must surface rather than be swallowed.
func TestDelete_LiveRecordStillDeprovisions(t *testing.T) {
	f := &fakeDeprovisionAPI{statuses: []string{"Complete"}}
	if !deprovisionDelete(t, f) {
		t.Fatal("Delete should surface the deprovision error for a live record")
	}
	if f.deprovision != 1 {
		t.Errorf("deprovision called %d times, want 1", f.deprovision)
	}
	if f.deletes != 0 {
		t.Errorf("delete called %d times, want 0 after a failed deprovision", f.deletes)
	}
}
