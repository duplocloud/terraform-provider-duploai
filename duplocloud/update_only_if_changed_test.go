package duplocloud

import (
	"context"
	"encoding/json"
	"io"
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

// aws_efs shape: throughput_mode and provisioned_throughput_in_mibps are sent
// only when changed (UpdateFileSystem rejects an unchanged throughput), but the
// throughput is required whenever the mode is provisioned. Switching back to
// provisioned with the same number still in state must send it, or the
// backend rejects the switch.
func TestUpdateOnlyIfChanged_KeepsFieldRequiredByChangedCondition(t *testing.T) {
	var mu sync.Mutex
	var lastPut map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPut {
			b, _ := io.ReadAll(req.Body)
			mu.Lock()
			lastPut = map[string]any{}
			_ = json.Unmarshal(b, &lastPut)
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"data":{"id":"obj1","status":"Complete"}}`))
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
			Name:   "efslike",
			IDPath: "id",
			Attributes: []AttributeSpec{
				{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
				{Name: "description", Type: "string", Optional: true, APIPath: "description"},
				{Name: "throughput_mode", Type: "string", Optional: true, Computed: true, UpdateOnlyIfChanged: true,
					CreatePath: "spec.createRequest.throughputMode", UpdatePath: "spec.updateRequest.throughputMode"},
				{Name: "provisioned_throughput_in_mibps", Type: "int", Optional: true, Computed: true, UpdateOnlyIfChanged: true,
					CreatePath: "spec.createRequest.provisionedThroughputInMibps", UpdatePath: "spec.updateRequest.provisionedThroughputInMibps"},
				{Name: "status", Type: "string", Computed: true, APIPath: "status"},
			},
			RequiredIf: []RequiredIfRule{{
				Attribute: "provisioned_throughput_in_mibps",
				When:      []RequiredIfCondition{{Attribute: "throughput_mode", Equals: "provisioned"}},
			}},
		},
	}

	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	mk := func(desc, mode string, mibps int64) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for n, typ := range objType.AttributeTypes {
			vals[n] = tftypes.NewValue(typ, nil)
		}
		vals["id"] = tftypes.NewValue(tftypes.String, "ws1/obj1")
		vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws1")
		vals["description"] = tftypes.NewValue(tftypes.String, desc)
		vals["throughput_mode"] = tftypes.NewValue(tftypes.String, mode)
		vals["provisioned_throughput_in_mibps"] = tftypes.NewValue(tftypes.Number, mibps)
		vals["status"] = tftypes.NewValue(tftypes.String, "Complete")
		return tftypes.NewValue(objType, vals)
	}
	update := func(state, plan tftypes.Value) map[string]any {
		mu.Lock()
		lastPut = nil
		mu.Unlock()
		resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema}}
		r.Update(ctx, resource.UpdateRequest{
			Plan:   tfsdk.Plan{Schema: sr.Schema, Raw: plan},
			State:  tfsdk.State{Schema: sr.Schema, Raw: state},
			Config: tfsdk.Config{Schema: sr.Schema, Raw: plan},
		}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Update diagnostics: %v", resp.Diagnostics.Errors())
		}
		mu.Lock()
		defer mu.Unlock()
		ur, _ := lastPut["spec"].(map[string]any)
		if ur == nil {
			return nil
		}
		got, _ := ur["updateRequest"].(map[string]any)
		return got
	}

	// bursting -> provisioned with the throughput still 100 in state: both sent.
	got := update(mk("d", "bursting", 100), mk("d", "provisioned", 100))
	if got["throughputMode"] != "provisioned" || got["provisionedThroughputInMibps"] != float64(100) {
		t.Errorf("switch to provisioned: updateRequest = %v, want mode and throughput 100", got)
	}

	// Description-only change: neither throughput field is sent.
	got = update(mk("d", "provisioned", 100), mk("d2", "provisioned", 100))
	if got != nil {
		t.Errorf("description-only change: updateRequest = %v, want none", got)
	}

	// Throughput changed, mode unchanged: only the throughput is sent.
	got = update(mk("d", "provisioned", 100), mk("d", "provisioned", 200))
	if _, ok := got["throughputMode"]; ok || got["provisionedThroughputInMibps"] != float64(200) {
		t.Errorf("throughput change: updateRequest = %v, want throughput 200 only", got)
	}
}
