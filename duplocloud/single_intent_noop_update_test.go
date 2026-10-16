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

// A single-intent update with no intent change (only a create-time value
// adopted after import) sends nothing, but must still return a fully known
// state: the plan holds computed outputs as unknown, and Terraform rejects an
// apply that leaves any unknown ("Provider returned invalid result object").
// Seen live on native_host after import.
func TestSingleIntentUpdate_NoIntentChangeFillsComputed(t *testing.T) {
	var mu sync.Mutex
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPut {
			mu.Lock()
			puts++
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"data":{"id":"obj1","status":"Complete","result":{"liveState":"running"},"spec":{"tags":{"a":"b"}}}}`))
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
			Name:   "sinoop",
			IDPath: "id",
			Attributes: []AttributeSpec{
				{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
				{Name: "hop_limit", Type: "int", Optional: true, Computed: true, RequestPath: "spec.hopLimit"},
				{Name: "tags", Type: "map(string)", Optional: true, APIPath: "spec.tags",
					UpdateIntent: &UpdateIntentSpec{DiscriminatorValue: "UpdateTags", ValuePath: "spec.updateRequest.tags"}},
				{Name: "status", Type: "string", Computed: true, APIPath: "status"},
				{Name: "live_state", Type: "string", Computed: true, APIPath: "result.liveState"},
			},
			Waiter:             &WaiterSpec{StatusPath: "status", SuccessState: "Complete"},
			SingleIntentUpdate: &SingleIntentUpdateSpec{DiscriminatorPath: "spec.updateRequest.updateType", ReadyPath: "result.liveState", ReadyState: "running"},
		},
	}

	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	tags := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{"a": tftypes.NewValue(tftypes.String, "b")})
	mk := func(hop, status, live tftypes.Value) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for n, typ := range objType.AttributeTypes {
			vals[n] = tftypes.NewValue(typ, nil)
		}
		vals["id"] = tftypes.NewValue(tftypes.String, "ws1/obj1")
		vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws1")
		vals["tags"] = tags
		vals["hop_limit"], vals["status"], vals["live_state"] = hop, status, live
		return tftypes.NewValue(objType, vals)
	}
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	state := mk(tftypes.NewValue(tftypes.Number, nil), tftypes.NewValue(tftypes.String, "Complete"), tftypes.NewValue(tftypes.String, "running"))
	plan := mk(tftypes.NewValue(tftypes.Number, 1), unknown, unknown)

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: sr.Schema, Raw: plan},
		State: tfsdk.State{Schema: sr.Schema, Raw: state},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsFullyKnown() {
		t.Fatalf("state after a no-intent update must be fully known, got %v", resp.State.Raw)
	}
	got := map[string]tftypes.Value{}
	_ = resp.State.Raw.As(&got)
	if !got["status"].Equal(tftypes.NewValue(tftypes.String, "Complete")) || !got["hop_limit"].Equal(tftypes.NewValue(tftypes.Number, 1)) {
		t.Errorf("status = %v (want Complete from the read), hop_limit = %v (want the planned 1)", got["status"], got["hop_limit"])
	}
	mu.Lock()
	defer mu.Unlock()
	if puts != 0 {
		t.Errorf("no intent changed, so no PUT may be sent; got %d", puts)
	}
}
