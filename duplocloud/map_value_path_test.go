package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// resource_group.tags is a map(string) in Terraform but arrives from the API as
// {"key":{"value":"…","remove":false}} — the platform keeps per-entry bookkeeping
// so a reconciler can tell "we set this and the group dropped it" apart from
// "somebody else's tag". mapValuePath unwraps that on read; mapDropWhenTrue hides
// entries the platform has flagged but not yet finished removing. Without the
// second one a removed tag reappears in state on the next read and diffs forever
// against a config that no longer lists it.
var tagsAttr = AttributeSpec{
	Name: "tags", Type: "map(string)", Optional: true, Computed: true,
	APIPath: "tags", MapValuePath: "value", MapDropWhenTrue: "remove",
}

func readTags(t *testing.T, data any) map[string]string {
	t.Helper()
	v := attrFromResponse(tagsAttr, tftypes.Map{ElementType: tftypes.String}, data)
	if v.IsNull() {
		return nil
	}
	var raw map[string]tftypes.Value
	if err := v.As(&raw); err != nil {
		t.Fatalf("As() failed: %v", err)
	}
	out := make(map[string]string, len(raw))
	for k, elem := range raw {
		var s string
		if err := elem.As(&s); err != nil {
			t.Fatalf("element %q: As() failed: %v", k, err)
		}
		out[k] = s
	}
	return out
}

func TestMapValuePath_UnwrapsWrappedEntries(t *testing.T) {
	got := readTags(t, map[string]any{
		"cost-center": map[string]any{"value": "fin-1024", "remove": false},
		"owner":       map[string]any{"value": "platform-team", "remove": false},
	})
	want := map[string]string{"cost-center": "fin-1024", "owner": "platform-team"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("tags[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestMapValuePath_DropsEntriesFlaggedForRemoval(t *testing.T) {
	got := readTags(t, map[string]any{
		"keep":    map[string]any{"value": "yes", "remove": false},
		"going":   map[string]any{"value": "was-here", "remove": true},
		"noFlag":  map[string]any{"value": "also-kept"},
		"novalue": map[string]any{"remove": false},
	})
	if _, present := got["going"]; present {
		t.Error("an entry flagged remove:true leaked into state — it will diff forever")
	}
	if got["keep"] != "yes" {
		t.Errorf("tags[keep] = %q, want \"yes\"", got["keep"])
	}
	if got["noFlag"] != "also-kept" {
		t.Errorf("tags[noFlag] = %q, want \"also-kept\" (absent flag means not removed)", got["noFlag"])
	}
	if _, present := got["novalue"]; present {
		t.Error("an entry with no value field should be dropped, not surfaced as empty")
	}
}

// The API also accepts, and older records may still carry, the flat shorthand.
// Both shapes can appear in one payload from a client mid-rollout.
func TestMapValuePath_AcceptsFlatShorthandAndMixedShapes(t *testing.T) {
	got := readTags(t, map[string]any{
		"flat":    "plain-value",
		"wrapped": map[string]any{"value": "wrapped-value", "remove": false},
	})
	if got["flat"] != "plain-value" {
		t.Errorf("tags[flat] = %q, want \"plain-value\"", got["flat"])
	}
	if got["wrapped"] != "wrapped-value" {
		t.Errorf("tags[wrapped] = %q, want \"wrapped-value\"", got["wrapped"])
	}
}

// The two read-side map knobs must compose: mapValuePath rewrites values,
// filterResponseKeys selects keys, so a filtered key stays gone whichever shape it
// arrived in — and the top-level read loop applies the filter before calling
// attrFromResponse, while nested attributes get both in here.
func TestMapValuePath_ComposesWithFilterResponseKeys(t *testing.T) {
	a := tagsAttr
	a.FilterResponseKeys = []string{"internal-*", "duplo-owned"}

	v := attrFromResponse(a, tftypes.Map{ElementType: tftypes.String}, map[string]any{
		"owner":         map[string]any{"value": "platform-team", "remove": false},
		"internal-cost": map[string]any{"value": "hidden", "remove": false},
		"duplo-owned":   "also-hidden",
		"going":         map[string]any{"value": "bye", "remove": true},
	})
	var raw map[string]tftypes.Value
	if err := v.As(&raw); err != nil {
		t.Fatalf("As() failed: %v", err)
	}
	for _, gone := range []string{"internal-cost", "duplo-owned", "going"} {
		if _, present := raw[gone]; present {
			t.Errorf("key %q survived; filtering and flagged-removal must both apply", gone)
		}
	}
	if _, present := raw["owner"]; !present {
		t.Error("user key owner was dropped")
	}
}

// Null must stay null rather than widening to an empty map: the backend reads a
// null tags map as "not supplied, leave the stored tags alone" and an empty one
// as "remove them all", so collapsing the two would silently delete every tag.
func TestMapValuePath_NullStaysNull(t *testing.T) {
	v := attrFromResponse(tagsAttr, tftypes.Map{ElementType: tftypes.String}, nil)
	if !v.IsNull() {
		t.Errorf("null response became %v, want null", v)
	}
}

// The write direction is unchanged: Terraform sends the flat map the API's
// converter accepts as shorthand, never the wrapped form.
func TestMapValuePath_RequestSendsFlatMap(t *testing.T) {
	spec := ResourceSpec{
		Name: "rg", IDPath: "id",
		Endpoint:   EndpointSpec{UriBase: "/rgs"},
		Attributes: []AttributeSpec{{Name: "name", Type: "string", Required: true, APIPath: "name"}, tagsAttr},
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("spec rejected: %v", err)
	}
	r := &dynamicResource{spec: spec}

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":   tftypes.String,
		"name": tftypes.String,
		"tags": tftypes.Map{ElementType: tftypes.String},
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"name": tftypes.NewValue(tftypes.String, "rg-1"),
		"tags": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
			"owner": tftypes.NewValue(tftypes.String, "platform-team"),
		}),
	})

	body := r.bodyFromRaw(raw, "create", nil)
	tags, ok := body["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags not sent as an object: %#v", body["tags"])
	}
	if got := tags["owner"]; got != "platform-team" {
		t.Errorf("tags[owner] = %#v (%T), want the plain string \"platform-team\"", got, got)
	}
}

// Both knobs are read-side helpers for one specific shape; misuse must fail at
// spec load rather than silently doing nothing.
func TestMapValuePath_SpecValidation(t *testing.T) {
	load := func(a AttributeSpec) error {
		s := ResourceSpec{
			Name: "x", IDPath: "id",
			Endpoint:   EndpointSpec{UriBase: "/x"},
			Attributes: []AttributeSpec{a},
		}
		return s.validate()
	}
	if err := load(AttributeSpec{Name: "tags", Type: "string", Optional: true,
		APIPath: "tags", MapValuePath: "value"}); err == nil {
		t.Error("expected error for mapValuePath on a non-map attribute")
	}
	if err := load(AttributeSpec{Name: "tags", Type: "map(string)", Optional: true,
		APIPath: "tags", MapDropWhenTrue: "remove"}); err == nil {
		t.Error("expected error for mapDropWhenTrue without mapValuePath")
	}
	if err := load(tagsAttr); err != nil {
		t.Errorf("valid mapValuePath attribute rejected: %v", err)
	}
}
