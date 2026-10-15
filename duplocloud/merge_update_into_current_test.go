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

// mergeUpdateIntoCurrent: the backend replaces the stored record with the PUT
// body, so a body-only update dropped status, the ticket link and backend-set
// spec fields, and a skill-provisioned network re-ran first-time provisioning on
// a description-only change. With the flag the PUT is the current record with
// the update overlaid; a settable attribute planned null is cleared.
func TestUpdate_MergeUpdateIntoCurrent(t *testing.T) {
	for _, flag := range []bool{true, false} {
		var mu sync.Mutex
		var put map[string]any
		current := `{"data":{"id":"obj1","name":"n","description":"old","status":"Complete",
		  "ticketContext":{"ticketId":"t-1"},
		  "spec":{"cidr":"10.0.0.0/16","importVpcId":"vpc-1","tags":{"a":"b"},"natMode":"None"},
		  "result":{"vpcId":"vpc-1"}}}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodPut {
				b, _ := io.ReadAll(req.Body)
				mu.Lock()
				_ = json.Unmarshal(b, &put)
				mu.Unlock()
			}
			_, _ = w.Write([]byte(current))
		}))
		client, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		r := &dynamicResource{
			baseResource: baseResource{Client: client},
			endpoint:     duplosdk.Endpoint{UriBase: "/v1/items/{workspace_id}", Update: duplosdk.Operation{Verb: http.MethodPut}},
			spec: ResourceSpec{
				Name: "merge", IDPath: "id", MergeUpdateIntoCurrent: flag,
				Attributes: []AttributeSpec{
					{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
					{Name: "name", Type: "string", Required: true, ForceNew: true, APIPath: "name"},
					{Name: "description", Type: "string", Optional: true, APIPath: "description"},
					{Name: "cidr", Type: "string", Optional: true, APIPath: "spec.cidr"},
					{Name: "tags", Type: "map(string)", Optional: true, APIPath: "spec.tags"},
				},
			},
		}
		ctx := context.Background()
		var sr resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sr)
		objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
		str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
		tags := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{"a": str("b")})
		mk := func(desc string, tg tftypes.Value) tftypes.Value {
			vals := map[string]tftypes.Value{}
			for n, typ := range objType.AttributeTypes {
				vals[n] = tftypes.NewValue(typ, nil)
			}
			vals["id"], vals["workspace_id"], vals["name"] = str("ws1/obj1"), str("ws1"), str("n")
			vals["description"], vals["cidr"], vals["tags"] = str(desc), str("10.0.0.0/16"), tg
			return tftypes.NewValue(objType, vals)
		}
		// Description changes; tags removed from config (planned null).
		resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema}}
		r.Update(ctx, resource.UpdateRequest{
			Plan:  tfsdk.Plan{Schema: sr.Schema, Raw: mk("new", tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil))},
			State: tfsdk.State{Schema: sr.Schema, Raw: mk("old", tags)},
		}, resp)
		srv.Close()
		if resp.Diagnostics.HasError() {
			t.Fatalf("flag=%v: Update diagnostics: %v", flag, resp.Diagnostics.Errors())
		}
		mu.Lock()
		spec, _ := put["spec"].(map[string]any)
		if put["description"] != "new" || spec["cidr"] != "10.0.0.0/16" {
			t.Errorf("flag=%v: the update body must still win: %v", flag, put)
		}
		if _, has := spec["tags"]; has {
			t.Errorf("flag=%v: tags planned null must not be sent, got %v", flag, spec["tags"])
		}
		if flag {
			tc, _ := put["ticketContext"].(map[string]any)
			if put["status"] != "Complete" || tc["ticketId"] != "t-1" || spec["importVpcId"] != "vpc-1" || spec["natMode"] != "None" || put["result"] == nil {
				t.Errorf("merge: server-managed fields must be sent back, got %v", put)
			}
		} else if put["status"] != nil || spec["importVpcId"] != nil {
			t.Errorf("flag off: body-only PUT expected, got %v", put)
		}
		mu.Unlock()
	}
}
