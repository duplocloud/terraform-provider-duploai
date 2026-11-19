package duplocloud

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// RCA guard.
//
// The compat gate (compat_schema_test.go) compares the schema with v0.6.7, which
// predates the RCA work (DUPLOAI-3931), so it cannot notice a spec losing one of
// the flags that fixed the dh-uae incident (a tags-only update on storage_account
// made azure_resource_id unknown and replaced 8 dependent containers) or the
// skill-provisioned "every update re-provisions" defect. This test pins them:
// every flag recorded in testdata/compat/rca_flags.json must still be present on
// HEAD. Adding flags is fine; removing one (or the attribute/spec carrying it)
// fails until the snapshot is regenerated on purpose with UPDATE_RCA_FLAGS=1.

const rcaFlagsPath = "testdata/compat/rca_flags.json"

// rcaFlagSnapshot is the on-disk form of testdata/compat/rca_flags.json.
type rcaFlagSnapshot struct {
	Note string `json:"note"`
	// Attributes maps "<resource>.<attr>[.<nested>...]" to its sorted RCA flags.
	// stableUnlessChanged is recorded once per trigger as
	// "stableUnlessChanged:<attr>", so dropping a single trigger is caught too.
	Attributes map[string][]string `json:"attributes"`
	// MergeUpdateIntoCurrent lists the specs that set the resource-level flag.
	MergeUpdateIntoCurrent []string `json:"mergeUpdateIntoCurrent"`
}

func rcaAttrFlags(a AttributeSpec) []string {
	var f []string
	if a.Stable {
		f = append(f, "stable")
	}
	for _, s := range a.StableUnlessChanged {
		f = append(f, "stableUnlessChanged:"+s)
	}
	if a.SendFromState {
		f = append(f, "sendFromState")
	}
	if a.ImportAdoptsConfig {
		f = append(f, "importAdoptsConfig")
	}
	if a.CaseInsensitive {
		f = append(f, "caseInsensitive")
	}
	if a.PreserveOnEmptyResponse {
		f = append(f, "preserveOnEmptyResponse")
	}
	if a.ResendWhenOmitted {
		f = append(f, "resendWhenOmitted")
	}
	sort.Strings(f)
	return f
}

func rcaWalkAttrs(prefix string, attrs []AttributeSpec, out map[string][]string) {
	for _, a := range attrs {
		p := prefix + a.Name
		if f := rcaAttrFlags(a); len(f) > 0 {
			out[p] = f
		}
		rcaWalkAttrs(p+".", a.Attributes, out)
	}
}

func rcaSnapshotFromSpecs(specs []ResourceSpec) rcaFlagSnapshot {
	snap := rcaFlagSnapshot{
		Note:                   "RCA flags pinned by rca_guard_test.go; regenerate with UPDATE_RCA_FLAGS=1 go test ./duplocloud -run TestRCAFlags_Generate",
		Attributes:             map[string][]string{},
		MergeUpdateIntoCurrent: []string{},
	}
	for _, s := range specs {
		rcaWalkAttrs(s.Name+".", s.Attributes, snap.Attributes)
		if s.MergeUpdateIntoCurrent {
			snap.MergeUpdateIntoCurrent = append(snap.MergeUpdateIntoCurrent, s.Name)
		}
	}
	sort.Strings(snap.MergeUpdateIntoCurrent)
	return snap
}

// rcaRemovedFlags returns one message per flag present in base but missing in cur.
func rcaRemovedFlags(base, cur rcaFlagSnapshot) []string {
	var out []string
	for path, flags := range base.Attributes {
		have := map[string]bool{}
		for _, f := range cur.Attributes[path] {
			have[f] = true
		}
		for _, f := range flags {
			if !have[f] {
				out = append(out, "duploai_"+path+": lost "+f)
			}
		}
	}
	curMerge := map[string]bool{}
	for _, n := range cur.MergeUpdateIntoCurrent {
		curMerge[n] = true
	}
	for _, n := range base.MergeUpdateIntoCurrent {
		if !curMerge[n] {
			out = append(out, "duploai_"+n+": lost mergeUpdateIntoCurrent")
		}
	}
	sort.Strings(out)
	return out
}

func rcaCount(s rcaFlagSnapshot) int {
	n := len(s.MergeUpdateIntoCurrent)
	for _, f := range s.Attributes {
		n += len(f)
	}
	return n
}

func rcaCurrentSnapshot(t *testing.T) rcaFlagSnapshot {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loading specs: %v", err)
	}
	return rcaSnapshotFromSpecs(specs)
}

// TestRCAFlags_NotRemoved is the gate.
func TestRCAFlags_NotRemoved(t *testing.T) {
	data, err := os.ReadFile(rcaFlagsPath)
	if err != nil {
		t.Fatalf("reading RCA flag snapshot (regenerate with UPDATE_RCA_FLAGS=1): %v", err)
	}
	var base rcaFlagSnapshot
	if err := json.Unmarshal(data, &base); err != nil {
		t.Fatalf("parsing %s: %v", rcaFlagsPath, err)
	}
	if len(base.Attributes) == 0 || len(base.MergeUpdateIntoCurrent) == 0 {
		t.Fatalf("%s is empty; it must pin the RCA flags", rcaFlagsPath)
	}
	cur := rcaCurrentSnapshot(t)
	removed := rcaRemovedFlags(base, cur)
	for _, r := range removed {
		t.Errorf("RCA flag removed: %s", r)
	}
	if len(removed) > 0 {
		t.Logf("These flags fix the dh-uae replacement cascade (stable*, sendFromState, caseInsensitive, ...) " +
			"and skill-provisioned re-provisioning (mergeUpdateIntoCurrent). If the removal is intended, " +
			"regenerate: UPDATE_RCA_FLAGS=1 go test ./duplocloud -run TestRCAFlags_Generate")
	}
	if added := rcaRemovedFlags(cur, base); len(added) > 0 {
		t.Logf("%d RCA flags added since the snapshot (fine; regenerate to pin them)", len(added))
	}
	t.Logf("pinned %d RCA flags on %d attributes + %d mergeUpdateIntoCurrent specs",
		rcaCount(base), len(base.Attributes), len(base.MergeUpdateIntoCurrent))
}

// TestRCAFlags_DetectsRemoval proves the comparison catches each kind of loss
// (fixture-based, so it does not depend on the live specs).
func TestRCAFlags_DetectsRemoval(t *testing.T) {
	base := rcaSnapshotFromSpecs([]ResourceSpec{
		{Name: "a", MergeUpdateIntoCurrent: true, Attributes: []AttributeSpec{
			{Name: "arn", Stable: true},
			{Name: "secret", StableUnlessChanged: []string{"name", "kms"}},
			{Name: "subnet", CaseInsensitive: true, ImportAdoptsConfig: true},
			{Name: "obj", Attributes: []AttributeSpec{{Name: "pw", PreserveOnEmptyResponse: true, ResendWhenOmitted: true}}},
			{Name: "scope_ids", SendFromState: true},
		}},
	})
	if got := rcaRemovedFlags(base, base); len(got) != 0 {
		t.Fatalf("identical snapshots reported removals: %v", got)
	}
	cases := []struct {
		name string
		mut  func(s *ResourceSpec)
		want string
	}{
		{"stable", func(s *ResourceSpec) { s.Attributes[0].Stable = false }, "duploai_a.arn: lost stable"},
		{"one stableUnlessChanged trigger", func(s *ResourceSpec) { s.Attributes[1].StableUnlessChanged = []string{"name"} }, "duploai_a.secret: lost stableUnlessChanged:kms"},
		{"caseInsensitive", func(s *ResourceSpec) { s.Attributes[2].CaseInsensitive = false }, "duploai_a.subnet: lost caseInsensitive"},
		{"importAdoptsConfig", func(s *ResourceSpec) { s.Attributes[2].ImportAdoptsConfig = false }, "duploai_a.subnet: lost importAdoptsConfig"},
		{"nested preserveOnEmptyResponse", func(s *ResourceSpec) { s.Attributes[3].Attributes[0].PreserveOnEmptyResponse = false }, "duploai_a.obj.pw: lost preserveOnEmptyResponse"},
		{"nested resendWhenOmitted", func(s *ResourceSpec) { s.Attributes[3].Attributes[0].ResendWhenOmitted = false }, "duploai_a.obj.pw: lost resendWhenOmitted"},
		{"sendFromState", func(s *ResourceSpec) { s.Attributes[4].SendFromState = false }, "duploai_a.scope_ids: lost sendFromState"},
		{"mergeUpdateIntoCurrent", func(s *ResourceSpec) { s.MergeUpdateIntoCurrent = false }, "duploai_a: lost mergeUpdateIntoCurrent"},
		{"attribute renamed", func(s *ResourceSpec) { s.Attributes[0].Name = "arn2" }, "duploai_a.arn: lost stable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := ResourceSpec{Name: "a", MergeUpdateIntoCurrent: true, Attributes: []AttributeSpec{
				{Name: "arn", Stable: true},
				{Name: "secret", StableUnlessChanged: []string{"name", "kms"}},
				{Name: "subnet", CaseInsensitive: true, ImportAdoptsConfig: true},
				{Name: "obj", Attributes: []AttributeSpec{{Name: "pw", PreserveOnEmptyResponse: true, ResendWhenOmitted: true}}},
				{Name: "scope_ids", SendFromState: true},
			}}
			c.mut(&s)
			got := rcaRemovedFlags(base, rcaSnapshotFromSpecs([]ResourceSpec{s}))
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("got %v, want [%s]", got, c.want)
			}
		})
	}
	// Additions are not removals.
	more := ResourceSpec{Name: "a", MergeUpdateIntoCurrent: true, Attributes: []AttributeSpec{
		{Name: "arn", Stable: true, SendFromState: true},
		{Name: "secret", StableUnlessChanged: []string{"name", "kms", "tags"}},
		{Name: "subnet", CaseInsensitive: true, ImportAdoptsConfig: true},
		{Name: "obj", Attributes: []AttributeSpec{{Name: "pw", PreserveOnEmptyResponse: true, ResendWhenOmitted: true}}},
		{Name: "scope_ids", SendFromState: true},
		{Name: "new", Stable: true},
	}}
	if got := rcaRemovedFlags(base, rcaSnapshotFromSpecs([]ResourceSpec{more, {Name: "b", MergeUpdateIntoCurrent: true}})); len(got) != 0 {
		t.Fatalf("additions reported as removals: %v", got)
	}
}

// TestRCAFlags_Generate rewrites testdata/compat/rca_flags.json from HEAD.
// Only runs with UPDATE_RCA_FLAGS=1.
func TestRCAFlags_Generate(t *testing.T) {
	if os.Getenv("UPDATE_RCA_FLAGS") != "1" {
		t.Skip("set UPDATE_RCA_FLAGS=1 to regenerate the RCA flag snapshot")
	}
	snap := rcaCurrentSnapshot(t)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := rcaFlagsPath
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s: %d flags on %d attributes, mergeUpdateIntoCurrent on %s",
		out, rcaCount(snap), len(snap.Attributes), strings.Join(snap.MergeUpdateIntoCurrent, ","))
}
