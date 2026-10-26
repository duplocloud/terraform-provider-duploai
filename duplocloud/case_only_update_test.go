package duplocloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// A case-only change to a caseInsensitive ARM id is a state-only change: the
// update body carries the stored casing, so it matches state and no PUT is sent
// (a PUT can re-trigger provisioning). ModifyPlan saw the bodies differ and left
// computed outputs unknown, so the skipped update reads the resource to fill
// them; the new casing is what state records.
func TestUpdate_CaseOnlyIDChangeSendsNoPut(t *testing.T) {
	var mu sync.Mutex
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPut {
			mu.Lock()
			puts++
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"data":{"id":"obj1","status":"Complete","spec":{"subnetId":"/subscriptions/s/resourceGroups/rg/providers/x/subnets/a"}}}`))
	}))
	t.Cleanup(srv.Close)
	client, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	r := &dynamicResource{
		baseResource: baseResource{Client: client},
		endpoint:     duplosdk.Endpoint{UriBase: "/v1/items/{workspace_id}", Update: duplosdk.Operation{Verb: http.MethodPut}},
		spec: ResourceSpec{
			Name:   "caseonly",
			IDPath: "id",
			Attributes: []AttributeSpec{
				{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
				{Name: "subnet_id", Type: "string", Required: true, ForceNew: true, CaseInsensitive: true, APIPath: "spec.subnetId"},
				{Name: "status", Type: "string", Computed: true, APIPath: "status"},
			},
		},
	}

	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	mk := func(subnet, status tftypes.Value) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for n, typ := range objType.AttributeTypes {
			vals[n] = tftypes.NewValue(typ, nil)
		}
		vals["id"] = tftypes.NewValue(tftypes.String, "ws1/obj1")
		vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws1")
		vals["subnet_id"], vals["status"] = subnet, status
		return tftypes.NewValue(objType, vals)
	}
	stored := tftypes.NewValue(tftypes.String, "/subscriptions/s/resourceGroups/rg/providers/x/subnets/a")
	recased := tftypes.NewValue(tftypes.String, "/subscriptions/s/resourcegroups/rg/providers/x/subnets/a")
	state := mk(stored, tftypes.NewValue(tftypes.String, "Complete"))
	plan := mk(recased, tftypes.NewValue(tftypes.String, tftypes.UnknownValue))

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: sr.Schema, Raw: plan},
		State: tfsdk.State{Schema: sr.Schema, Raw: state},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsFullyKnown() {
		t.Fatalf("state after a case-only update must be fully known, got %v", resp.State.Raw)
	}
	got := map[string]tftypes.Value{}
	_ = resp.State.Raw.As(&got)
	if !got["status"].Equal(tftypes.NewValue(tftypes.String, "Complete")) {
		t.Errorf("status = %v, want Complete from the read", got["status"])
	}
	if !got["subnet_id"].Equal(recased) {
		t.Errorf("subnet_id = %v, want the configured casing %v", got["subnet_id"], recased)
	}
	mu.Lock()
	defer mu.Unlock()
	if puts != 0 {
		t.Errorf("a case-only id change must not send a PUT; got %d", puts)
	}
}
