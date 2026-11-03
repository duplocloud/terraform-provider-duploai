package duplocloud

// Unit tests of the backward-compatibility checker itself (see
// compat_schema_test.go): each breaking mutation of a real v0.6.7 spec must be
// reported with its exact key, additive changes must pass, and the allowlist
// must neither hide unrelated breaks nor keep stale entries.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const compatFixtureSpec = "admin_quota_definition.json"

// compatFixture loads the v0.6.7 admin_quota_definition spec as a generic map
// and applies mutate to its attribute list before building a snapshot.
func compatFixture(t *testing.T, mutate func(attrs []map[string]any) []map[string]any) compatSnapshot {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(compatDir, compatBaselineVersion+"-specs", compatFixtureSpec))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var attrs []map[string]any
	for _, a := range raw["attributes"].([]any) {
		attrs = append(attrs, a.(map[string]any))
	}
	if mutate != nil {
		attrs = mutate(attrs)
	}
	raw["attributes"] = attrs
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := compatParseSpec(data)
	if err != nil {
		t.Fatalf("mutated fixture spec is invalid: %v", err)
	}
	snap, err := compatSnapshotFromSpecs([]ResourceSpec{spec}, "fixture", "test")
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func compatAttrNamed(attrs []map[string]any, name string) map[string]any {
	for _, a := range attrs {
		if a["name"] == name {
			return a
		}
	}
	panic("fixture has no attribute " + name)
}

func compatKeys(vs []compatViolation) []string {
	keys := make([]string, 0, len(vs))
	for _, v := range vs {
		keys = append(keys, v.Key)
	}
	sort.Strings(keys)
	return keys
}

const (
	qr = "resource.duploai_admin_quota_definition."
	qd = "data_source.duploai_admin_quota_definition."
)

func TestCompatChecker_DetectsBreaks(t *testing.T) {
	cases := []struct {
		name   string
		old    func([]map[string]any) []map[string]any
		new    func([]map[string]any) []map[string]any
		expect []string
	}{
		{
			name: "attribute removed",
			new: func(as []map[string]any) []map[string]any {
				var out []map[string]any
				for _, a := range as {
					if a["name"] != "description" {
						out = append(out, a)
					}
				}
				return out
			},
			expect: []string{qd + "description:removed", qr + "description:removed"},
		},
		{
			name: "new required attribute",
			new: func(as []map[string]any) []map[string]any {
				return append(as, map[string]any{"name": "owner", "type": "string", "required": true, "apiPath": "owner"})
			},
			expect: []string{qr + "owner:new-required"},
		},
		{
			name: "optional to required",
			new: func(as []map[string]any) []map[string]any {
				a := compatAttrNamed(as, "description")
				delete(a, "optional")
				delete(a, "computed")
				a["required"] = true
				return as
			},
			expect: []string{qr + "description:no-longer-computed", qr + "description:now-required"},
		},
		{
			name: "type changed",
			new: func(as []map[string]any) []map[string]any {
				compatAttrNamed(as, "limit_usd")["type"] = "int"
				return as
			},
			expect: []string{qd + "limit_usd:type-changed", qr + "limit_usd:type-changed"},
		},
		{
			name: "newly forceNew",
			new: func(as []map[string]any) []map[string]any {
				compatAttrNamed(as, "type")["forceNew"] = true
				return as
			},
			expect: []string{qr + "type:now-forcenew"},
		},
		{
			name: "newly createOnly",
			new: func(as []map[string]any) []map[string]any {
				compatAttrNamed(as, "description")["createOnly"] = true
				return as
			},
			expect: []string{qr + "description:now-createonly"},
		},
		{
			name: "default added",
			new: func(as []map[string]any) []map[string]any {
				compatAttrNamed(as, "type")["default"] = "Monthly"
				return as
			},
			expect: []string{qr + "type:default-changed"},
		},
		{
			name: "computed dropped",
			new: func(as []map[string]any) []map[string]any {
				delete(compatAttrNamed(as, "is_active"), "computed")
				return as
			},
			expect: []string{qr + "is_active:no-longer-computed"},
		},
		{
			name: "sensitive dropped",
			old: func(as []map[string]any) []map[string]any {
				compatAttrNamed(as, "description")["sensitive"] = true
				return as
			},
			expect: []string{qd + "description:no-longer-sensitive", qr + "description:no-longer-sensitive"},
		},
		{
			name: "data source dropped",
			new:  func(as []map[string]any) []map[string]any { return as },
			// handled below by deleting the data source from the snapshot
			expect: []string{"data_source.duploai_admin_quota_definition:removed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := compatFixture(t, tc.old)
			cur := compatFixture(t, tc.new)
			if tc.name == "data source dropped" {
				delete(cur.DataSources, "duploai_admin_quota_definition")
			}
			got := compatKeys(compatCompare(old, cur))
			sort.Strings(tc.expect)
			if !reflect.DeepEqual(got, tc.expect) {
				t.Fatalf("violations\n got: %v\nwant: %v", got, tc.expect)
			}
		})
	}
}

func TestCompatChecker_ResourceRemovedAndLostUpdate(t *testing.T) {
	old := compatFixture(t, nil)
	cur := compatFixture(t, nil)
	delete(cur.Resources, "duploai_admin_quota_definition")
	if got := compatKeys(compatCompare(old, cur)); !reflect.DeepEqual(got, []string{"resource.duploai_admin_quota_definition:removed"}) {
		t.Fatalf("got %v", got)
	}
	cur = compatFixture(t, nil)
	ent := cur.Resources["duploai_admin_quota_definition"]
	ent.HasUpdate = false
	ent.SchemaVersion = -1
	cur.Resources["duploai_admin_quota_definition"] = ent
	want := []string{qr[:len(qr)-1] + ":lost-update", qr[:len(qr)-1] + ":schema-version-decreased"}
	if got := compatKeys(compatCompare(old, cur)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCompatChecker_AllowsAdditiveChanges(t *testing.T) {
	old := compatFixture(t, func(as []map[string]any) []map[string]any {
		compatAttrNamed(as, "type")["forceNew"] = true
		return as
	})
	cur := compatFixture(t, func(as []map[string]any) []map[string]any {
		// required -> optional, forceNew removed, new optional and computed attributes
		a := compatAttrNamed(as, "limit_usd")
		delete(a, "required")
		a["optional"] = true
		return append(as,
			map[string]any{"name": "owner", "type": "string", "optional": true, "apiPath": "owner"},
			map[string]any{"name": "region", "type": "string", "computed": true, "apiPath": "region"},
			map[string]any{"name": "secret", "type": "string", "optional": true, "sensitive": true, "apiPath": "secret"})
	})
	if vs := compatCompare(old, cur); len(vs) != 0 {
		t.Fatalf("additive changes reported as breaking: %v", compatKeys(vs))
	}
	if vs := compatCompare(old, old); len(vs) != 0 {
		t.Fatalf("identical snapshots reported as breaking: %v", compatKeys(vs))
	}
}

func TestCompatChecker_Allowlist(t *testing.T) {
	vs := []compatViolation{{Key: "a:removed"}, {Key: "b:now-required"}}
	allow := []compatAllowEntry{{Key: "a:removed", Reason: "r"}, {Key: "c:removed", Reason: "r"}}
	unexpected, allowed, stale := compatApplyAllowlist(vs, allow)
	if compatKeys(unexpected)[0] != "b:now-required" || len(unexpected) != 1 {
		t.Fatalf("unexpected = %v", compatKeys(unexpected))
	}
	if len(allowed) != 1 || allowed[0].Key != "a:removed" {
		t.Fatalf("allowed = %v", compatKeys(allowed))
	}
	if len(stale) != 1 || stale[0].Key != "c:removed" {
		t.Fatalf("stale = %v", stale)
	}
}
