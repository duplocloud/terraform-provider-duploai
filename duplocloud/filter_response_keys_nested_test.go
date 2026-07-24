package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A map(string) attribute nested inside an object (e.g. azure.tags) must honor
// filterResponseKeys, dropping server-stamped keys before they reach state so a
// partially-managed map does not show perpetual drift.
func TestAttrFromResponse_FiltersNestedMapKeys(t *testing.T) {
	azure := AttributeSpec{
		Name: "azure", Type: "object", Optional: true, Computed: true,
		Attributes: []AttributeSpec{
			{
				Name: "tags", Type: "map(string)", Optional: true, Computed: true,
				FilterResponseKeys: []string{"duplocloud-ai-*"},
			},
		},
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"tags": tftypes.Map{ElementType: tftypes.String},
	}}
	data := map[string]any{
		"tags": map[string]any{
			"team":                       "platform",        // user tag, kept
			"duplocloud-ai-managed-by":   "duplocloud-ai",   // stamped, dropped
			"duplocloud-ai-workspace":    "azure-workspace", // stamped, dropped
			"duplocloud-ai-resourcetype": "network",         // stamped, dropped
		},
	}

	got := attrFromResponse(azure, objType, data)

	var obj map[string]tftypes.Value
	if err := got.As(&obj); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	var tags map[string]tftypes.Value
	if err := obj["tags"].As(&tags); err != nil {
		t.Fatalf("decode tags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected only the user tag to survive, got %d: %v", len(tags), tags)
	}
	if _, ok := tags["team"]; !ok {
		t.Errorf("user tag %q should be preserved", "team")
	}
}
