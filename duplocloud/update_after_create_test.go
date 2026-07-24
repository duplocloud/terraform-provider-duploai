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

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// updateAfterCreate: when set, Create issues a follow-up PUT (carrying the update
// body) once the resource is ready, so fields the backend applies only on the
// update path (e.g. Azure storage data protection) take effect on first apply.
// When unset, Create issues no PUT. Verified by driving the real Create() flow
// against an httptest server that counts PUTs.
func TestCreate_UpdateAfterCreate(t *testing.T) {
	cases := []struct {
		name     string
		flag     bool
		wantPuts int
	}{
		{"flag on issues one follow-up update", true, 1},
		{"flag off issues no update", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			puts := 0
			var putBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == http.MethodPut {
					mu.Lock()
					puts++
					if b, _ := io.ReadAll(req.Body); len(b) > 0 {
						_ = json.Unmarshal(b, &putBody)
					}
					mu.Unlock()
				}
				_, _ = w.Write([]byte(`{"data":{"id":"obj1","status":"Complete","spec":{"val":"x"}}}`))
			}))
			t.Cleanup(srv.Close)

			client, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			spec := ResourceSpec{
				Name:   "uac",
				IDPath: "id",
				Attributes: []AttributeSpec{
					{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
					{Name: "name", Type: "string", Required: true, APIPath: "name"},
					{
						Name: "val", Type: "string", Optional: true, Computed: true,
						CreatePath: "spec.val", UpdatePath: "spec.updateRequest.val", ResponsePath: "spec.val",
					},
				},
				Waiter:            &WaiterSpec{StatusPath: "status", SuccessState: "Complete"},
				UpdateAfterCreate: tc.flag,
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
				vals[n] = tftypes.NewValue(typ, nil) // null default
			}
			vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws1")
			vals["name"] = tftypes.NewValue(tftypes.String, "n")
			vals["val"] = tftypes.NewValue(tftypes.String, "x")
			raw := tftypes.NewValue(objType, vals)

			req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: raw}}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
			r.Create(ctx, req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Create diagnostics: %v", resp.Diagnostics.Errors())
			}

			mu.Lock()
			defer mu.Unlock()
			if puts != tc.wantPuts {
				t.Fatalf("PUT count = %d, want %d", puts, tc.wantPuts)
			}
			if tc.flag {
				bodySpec, _ := putBody["spec"].(map[string]any)
				ur, _ := bodySpec["updateRequest"].(map[string]any)
				if ur["val"] != "x" {
					t.Errorf("post-create update body spec.updateRequest.val = %v, want x", ur["val"])
				}
			}
		})
	}
}
