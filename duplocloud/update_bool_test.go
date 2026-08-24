package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// UpdateBoolTrueValue writes the raw string on create (via createPath) but a
// derived bool on update (via updatePath) — e.g. ECR image_tag_mutability
// "IMMUTABLE" (create string) -> enableTagImmutability true (update bool).
func TestBodyFromRaw_UpdateBoolTrueValue(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"image_tag_mutability": tftypes.String,
	}}
	spec := ResourceSpec{Attributes: []AttributeSpec{{
		Name:                "image_tag_mutability",
		Type:                "string",
		Optional:            true,
		Computed:            true,
		CreatePath:          "spec.createRequest.imageTagMutability",
		UpdatePath:          "spec.updateRequest.enableTagImmutability",
		UpdateBoolTrueValue: "IMMUTABLE",
		ResponsePath:        "result.cloudDetails.imageTagMutability",
	}}}
	r := &dynamicResource{spec: spec}

	get := func(body map[string]any, path ...string) any {
		cur := any(body)
		for _, p := range path {
			m, _ := cur.(map[string]any)
			cur = m[p]
		}
		return cur
	}

	for _, tc := range []struct {
		name       string
		value      string
		wantCreate any
		wantUpdate any
	}{
		{"immutable", "IMMUTABLE", "IMMUTABLE", true},
		{"mutable", "MUTABLE", "MUTABLE", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tftypes.NewValue(objType, map[string]tftypes.Value{
				"image_tag_mutability": tftypes.NewValue(tftypes.String, tc.value),
			})
			var diags diag.Diagnostics

			create := r.bodyFromRaw(raw, "create", &diags)
			if diags.HasError() {
				t.Fatalf("create diags: %v", diags)
			}
			if got := get(create, "spec", "createRequest", "imageTagMutability"); got != tc.wantCreate {
				t.Errorf("create imageTagMutability = %#v, want %#v", got, tc.wantCreate)
			}

			update := r.bodyFromRaw(raw, "update", &diags)
			if diags.HasError() {
				t.Fatalf("update diags: %v", diags)
			}
			if got := get(update, "spec", "updateRequest", "enableTagImmutability"); got != tc.wantUpdate {
				t.Errorf("update enableTagImmutability = %#v (%T), want %#v", got, got, tc.wantUpdate)
			}
			// The string must NOT leak into the update body at the create path.
			if got := get(update, "spec", "createRequest", "imageTagMutability"); got != nil {
				t.Errorf("update leaked create-path string: %#v", got)
			}
		})
	}
}
