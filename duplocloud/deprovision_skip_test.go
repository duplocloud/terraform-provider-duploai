package duplocloud

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// deprovisionSkipped bypasses the pre-delete deprovision step when the
// deprovision SkipWhen conditions match prior state (e.g. cloud=K8S_ONLY).
func TestDeprovisionSkipped(t *testing.T) {
	def := json.RawMessage(`"Aws"`)
	spec := ResourceSpec{
		Name: "cluster", IDPath: "id",
		Endpoint: EndpointSpec{
			UriBase: "/clusters",
			Deprovision: &OperationSpec{
				SkipWhen: []RequiredIfCondition{{Attribute: "cloud", Equals: "K8S_ONLY"}},
			},
		},
		Attributes: []AttributeSpec{
			{Name: "cloud", Type: "string", Optional: true, Computed: true, Default: &def, APIPath: "spec.cloud"},
		},
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"id": tftypes.String, "cloud": tftypes.String}}
	stateWith := func(cloud tftypes.Value) tfsdk.State {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":    tftypes.NewValue(tftypes.String, "w/c"),
			"cloud": cloud,
		})
		return tfsdk.State{Schema: sr.Schema, Raw: raw}
	}

	cases := []struct {
		name  string
		cloud tftypes.Value
		want  bool
	}{
		{"k8s_only skips deprovision", tftypes.NewValue(tftypes.String, "K8S_ONLY"), true},
		{"aws does not skip", tftypes.NewValue(tftypes.String, "Aws"), false},
		{"null falls back to default Aws — no skip", tftypes.NewValue(tftypes.String, nil), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.deprovisionSkipped(context.Background(), stateWith(tc.cloud)); got != tc.want {
				t.Fatalf("deprovisionSkipped(cloud=%v) = %v, want %v", tc.cloud, got, tc.want)
			}
		})
	}
}

// With no SkipWhen configured the deprovision step is never skipped.
func TestDeprovisionSkipped_NoConfig(t *testing.T) {
	spec := ResourceSpec{
		Name: "x", IDPath: "id",
		Endpoint:   EndpointSpec{UriBase: "/x", Deprovision: &OperationSpec{}},
		Attributes: []AttributeSpec{{Name: "cloud", Type: "string", Optional: true, APIPath: "spec.cloud"}},
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"id": tftypes.String, "cloud": tftypes.String}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":    tftypes.NewValue(tftypes.String, "w/c"),
		"cloud": tftypes.NewValue(tftypes.String, "K8S_ONLY"),
	})
	if r.deprovisionSkipped(context.Background(), tfsdk.State{Schema: sr.Schema, Raw: raw}) {
		t.Fatal("expected no skip when SkipWhen is empty")
	}
}
