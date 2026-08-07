package duplocloud

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"net/http"
	"net/http/httptest"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// A resource is created successfully (POST succeeds, the object now exists in
// the backend) but then never becomes ready — e.g. a Helm release whose
// underlying pods never come up, tripping the waiter's failure gate. Create
// must still record the object's id in state, even though it returns an
// error: without it, Terraform believes creation never happened, and the next
// apply issues a fresh POST that collides with the object still sitting in
// the backend ("resource already exists"), forcing a manual out-of-band
// delete. Saving what is known so far lets Terraform track the object as
// tainted and reconcile it (destroy + recreate) on the next apply instead.
func TestCreate_WaiterFailureStillSavesID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Every GET/POST reports a terminal failure status, simulating a Helm
		// release whose install never succeeds.
		_, _ = w.Write([]byte(`{"data":{"id":"obj1","status":"Failed"}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	spec := ResourceSpec{
		Name:   "flaky",
		IDPath: "id",
		Attributes: []AttributeSpec{
			{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
		},
		Waiter: &WaiterSpec{
			StatusPath:   "status",
			SuccessState: "Complete",
			FailureStates: map[string]string{
				"Failed": "provisioning failed",
			},
		},
	}
	r := &dynamicResource{
		baseResource: baseResource{Client: client},
		spec:         spec,
		endpoint:     duplosdk.Endpoint{UriBase: "/v1/items/{workspace_id}"},
	}

	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)

	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for n, typ := range objType.AttributeTypes {
		vals[n] = tftypes.NewValue(typ, nil)
	}
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws1")
	vals["name"] = tftypes.NewValue(tftypes.String, "n")
	raw := tftypes.NewValue(objType, vals)

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: raw}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
	r.Create(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected Create to return an error once the waiter observes a failure state")
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("Create left state null after a waiter failure — the backend object (id=obj1) is now untracked, so the next apply will try to create it again and collide with it")
	}

	var m map[string]tftypes.Value
	if err := resp.State.Raw.As(&m); err != nil {
		t.Fatalf("decoding saved state: %v", err)
	}
	var id string
	_ = m["id"].As(&id)
	if id != "ws1/obj1" {
		t.Errorf("saved state id = %q, want %q", id, "ws1/obj1")
	}
}
