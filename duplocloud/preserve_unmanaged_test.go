package duplocloud

import (
	"context"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

var preserveSetType = tftypes.Set{ElementType: tftypes.String}

func preserveSpec() ResourceSpec {
	s := ResourceSpec{
		Name:     "ws",
		IDPath:   "id",
		Endpoint: EndpointSpec{UriBase: "/v1/x/Workspaces"},
		Attributes: []AttributeSpec{
			{Name: "scope_ids", Type: "set(string)", Optional: true, Computed: true, APIPath: "scopeIds", PreserveUnmanagedInto: "other_scope_ids"},
			{Name: "other_scope_ids", Type: "set(string)", Computed: true, NoSend: true},
		},
	}
	markPreserveTargets(s.Attributes)
	return s
}

func strSet(vals ...string) tftypes.Value {
	elems := make([]tftypes.Value, 0, len(vals))
	for _, v := range vals {
		elems = append(elems, tftypes.NewValue(tftypes.String, v))
	}
	return tftypes.NewValue(preserveSetType, elems)
}

func readStrSet(t *testing.T, v tftypes.Value) []string {
	t.Helper()
	var elems []tftypes.Value
	if err := v.As(&elems); err != nil {
		t.Fatalf("decoding set: %v", err)
	}
	out := []string{}
	for _, e := range elems {
		var s string
		_ = e.As(&s)
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The computed sibling of a PreserveUnmanagedInto attribute must adopt
// UseStateForUnknown so its prior-state value is known at plan time.
func TestPreserve_SiblingGetsUseStateForUnknown(t *testing.T) {
	s := preserveSpec()
	sib := findAttr(s.Attributes, "other_scope_ids")
	if sib == nil || !sib.preserveTarget {
		t.Fatal("sibling should be marked preserveTarget")
	}
	if !useStateForUnknown(*sib) {
		t.Error("computed sibling must use state for unknown")
	}
}

// On write, the request body sends the union of the managed attribute and its
// computed sibling, so server-managed entries survive a full-document update.
func TestPreserve_WriteSendsUnion(t *testing.T) {
	r := &dynamicResource{spec: preserveSpec()}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"scope_ids":       preserveSetType,
		"other_scope_ids": preserveSetType,
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"scope_ids":       strSet("u1"),
		"other_scope_ids": strSet("sys1"),
	})
	var diags diag.Diagnostics
	body := r.bodyFromRaw(raw, "update", &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	got := anyToStringSlice(body["scopeIds"])
	sort.Strings(got)
	if !eqStrings(got, []string{"sys1", "u1"}) {
		t.Errorf("scopeIds body = %v, want [sys1 u1] (union of managed + sibling)", got)
	}
}

// When the user manages no scopes, the write must not clear server-managed ones:
// scope_ids unset + sibling holding sys1 still sends sys1.
func TestPreserve_WriteUnsetKeepsSystem(t *testing.T) {
	r := &dynamicResource{spec: preserveSpec()}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"scope_ids":       preserveSetType,
		"other_scope_ids": preserveSetType,
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"scope_ids":       tftypes.NewValue(preserveSetType, nil), // user unset (null)
		"other_scope_ids": strSet("sys1"),
	})
	var diags diag.Diagnostics
	body := r.bodyFromRaw(raw, "update", &diags)
	got := anyToStringSlice(body["scopeIds"])
	if !eqStrings(got, []string{"sys1"}) {
		t.Errorf("scopeIds body = %v, want [sys1] (system scope preserved)", got)
	}
}

// On read (refresh), the server's full list is split: ids in the managed set
// stay in scope_ids, the rest (backend-attached) go to other_scope_ids.
func TestPreserve_ReadSplitsServerList(t *testing.T) {
	r := &dynamicResource{spec: preserveSpec()}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":              tftypes.String,
		"scope_ids":       preserveSetType,
		"other_scope_ids": preserveSetType,
	}}
	prior := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, "obj-1"),
		"scope_ids":       strSet("u1"), // user manages u1
		"other_scope_ids": strSet(),
	})
	var diags diag.Diagnostics
	// Backend attached sys1 out of band.
	out := r.stateFromResponse(context.Background(), prior,
		map[string]any{"scopeIds": []any{"u1", "sys1"}},
		map[string]string{}, "obj-1", true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	_ = out.As(&m)
	if got := readStrSet(t, m["scope_ids"]); !eqStrings(got, []string{"u1"}) {
		t.Errorf("scope_ids = %v, want [u1] (managed set only — no drift)", got)
	}
	if got := readStrSet(t, m["other_scope_ids"]); !eqStrings(got, []string{"sys1"}) {
		t.Errorf("other_scope_ids = %v, want [sys1] (backend-attached)", got)
	}
}

// On create/update with a known plan value, scope_ids keeps the plan (avoiding
// inconsistent-result), while other_scope_ids still captures the remainder.
func TestPreserve_CreateKeepsPlanValue(t *testing.T) {
	r := &dynamicResource{spec: preserveSpec()}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":              tftypes.String,
		"scope_ids":       preserveSetType,
		"other_scope_ids": preserveSetType,
	}}
	plan := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"scope_ids":       strSet("u1"),
		"other_scope_ids": tftypes.NewValue(preserveSetType, tftypes.UnknownValue),
	})
	var diags diag.Diagnostics
	out := r.stateFromResponse(context.Background(), plan,
		map[string]any{"scopeIds": []any{"u1"}},
		map[string]string{}, "obj-1", false, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	_ = out.As(&m)
	if got := readStrSet(t, m["scope_ids"]); !eqStrings(got, []string{"u1"}) {
		t.Errorf("scope_ids = %v, want [u1]", got)
	}
	if got := readStrSet(t, m["other_scope_ids"]); !eqStrings(got, []string{}) {
		t.Errorf("other_scope_ids = %v, want []", got)
	}
}

func TestPreserve_Validation(t *testing.T) {
	cases := []struct {
		name    string
		attrs   []AttributeSpec
		wantErr bool
	}{
		{"valid", []AttributeSpec{
			{Name: "scope_ids", Type: "set(string)", Optional: true, APIPath: "scopeIds", PreserveUnmanagedInto: "other_scope_ids"},
			{Name: "other_scope_ids", Type: "set(string)", Computed: true, NoSend: true},
		}, false},
		{"unknown sibling", []AttributeSpec{
			{Name: "scope_ids", Type: "set(string)", Optional: true, APIPath: "scopeIds", PreserveUnmanagedInto: "nope"},
		}, true},
		{"sibling not computed-only", []AttributeSpec{
			{Name: "scope_ids", Type: "set(string)", Optional: true, APIPath: "scopeIds", PreserveUnmanagedInto: "other_scope_ids"},
			{Name: "other_scope_ids", Type: "set(string)", Optional: true, Computed: true, NoSend: true},
		}, true},
		{"sibling not noSend", []AttributeSpec{
			{Name: "scope_ids", Type: "set(string)", Optional: true, APIPath: "scopeIds", PreserveUnmanagedInto: "other_scope_ids"},
			{Name: "other_scope_ids", Type: "set(string)", Computed: true},
		}, true},
		{"wrong source type", []AttributeSpec{
			{Name: "scope_ids", Type: "string", Optional: true, APIPath: "scopeIds", PreserveUnmanagedInto: "other_scope_ids"},
			{Name: "other_scope_ids", Type: "string", Computed: true, NoSend: true},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePreservePairs(tc.attrs)
			if (err != nil) != tc.wantErr {
				t.Errorf("validatePreservePairs err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
