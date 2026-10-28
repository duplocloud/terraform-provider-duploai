package duplocloud

// Backward-compatibility gate: every resource and data source released in
// v0.6.7 must still accept the configurations and state users already have.
//
// The released schema is frozen in testdata/compat/v0.6.7.json (generated from
// the v0.6.7 specs in testdata/compat/v0.6.7-specs, extracted from the git tag,
// through the engine's real Terraform schema builders). TestCompatSchema_v067
// builds the current provider's schema the same way and fails once per
// breaking difference. Deliberate, reviewed breaks go in
// testdata/compat/allowed.json with a reason — never weaken the checks.
//
// Regenerate the baseline (rarely; only to fix the generator itself):
//
//	UPDATE_COMPAT_BASELINE=1 go test ./duplocloud -run TestCompatBaseline_Generate
//
// Cross-check the baseline against the real released binary's schema:
//
//	terraform providers schema -json > /tmp/s.json   # in a dir pinned to 0.6.7
//	COMPAT_RELEASED_SCHEMA_JSON=/tmp/s.json go test ./duplocloud -run TestCompatBaseline_MatchesReleasedBinary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

const (
	compatBaselineVersion = "v0.6.7"
	compatDir             = "testdata/compat"
)

// compatAttr is the compatibility-relevant metadata of one schema attribute.
// Type comes from the framework schema; nested attributes record their
// nesting mode and contribute their children as separate dotted paths.
type compatAttr struct {
	Type       string `json:"type"`
	Required   bool   `json:"required,omitempty"`
	Optional   bool   `json:"optional,omitempty"`
	Computed   bool   `json:"computed,omitempty"`
	Sensitive  bool   `json:"sensitive,omitempty"`
	WriteOnly  bool   `json:"writeOnly,omitempty"`
	ForceNew   bool   `json:"forceNew,omitempty"`   // own forceNew or inherited from a forceNew parent
	CreateOnly bool   `json:"createOnly,omitempty"` // sent on create only, never on update
	Default    string `json:"default,omitempty"`    // spec default (raw JSON); a change re-plans existing resources
}

type compatEntity struct {
	SchemaVersion int64                 `json:"schemaVersion,omitempty"`
	HasUpdate     bool                  `json:"hasUpdate,omitempty"` // resources: endpoint has an in-place update
	Attributes    map[string]compatAttr `json:"attributes"`
}

type compatSnapshot struct {
	Version     string                  `json:"version"`
	Source      string                  `json:"source"`
	Resources   map[string]compatEntity `json:"resources"`
	DataSources map[string]compatEntity `json:"dataSources"`
}

// compatAllowEntry permits one specific reported violation. Key is the exact
// violation key printed by the checker; Reason is mandatory.
type compatAllowEntry struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
	Ref    string `json:"ref,omitempty"`
}

type compatViolation struct {
	Key string // stable id: <kind>.<name>[.<attr>]:<check>
	Msg string
}

// ── snapshot building ─────────────────────────────────────────────────────────

// compatParseSpec decodes a spec exactly like loadResourceSpecs does.
func compatParseSpec(data []byte) (ResourceSpec, error) {
	spec, warn, err := compatParseSpecLenient(data)
	if err == nil && warn != nil {
		err = warn
	}
	return spec, err
}

// compatParseSpecLenient is compatParseSpec for released specs: checks the
// current engine added later (duplicate keys, newer validate rules) are
// returned as a warning instead of an error, since the released engine
// accepted the spec (encoding/json keeps the last duplicate, as it did then).
func compatParseSpecLenient(data []byte) (spec ResourceSpec, warn, err error) {
	if dupErr := checkDuplicateKeys(data); dupErr != nil {
		warn = dupErr
	}
	if err = json.Unmarshal(data, &spec); err != nil {
		return spec, warn, err
	}
	if vErr := spec.validate(); vErr != nil && warn == nil {
		warn = fmt.Errorf("validate: %w", vErr)
	}
	markPreserveTargets(spec.Attributes)
	if spec.Waiter != nil {
		applyWaiterDefaults(spec.Waiter)
	}
	return spec, warn, nil
}

func compatLoadSpecDir(t *testing.T, dir string) ([]ResourceSpec, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var specs []ResourceSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		spec, warn, err := compatParseSpecLenient(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if warn != nil {
			t.Logf("note: released spec %s fails a newer engine check (kept as released): %v", e.Name(), warn)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// compatSnapshotFromSpecs builds the real framework resource and data source
// schemas for specs (the same factories the provider registers) and flattens
// them, adding forceNew/createOnly from the spec (plan modifiers are opaque).
func compatSnapshotFromSpecs(specs []ResourceSpec, version, source string) (compatSnapshot, error) {
	ctx := context.Background()
	snap := compatSnapshot{Version: version, Source: source,
		Resources: map[string]compatEntity{}, DataSources: map[string]compatEntity{}}
	for _, spec := range specs {
		endpoint, err := spec.BuildEndpoint()
		if err != nil {
			return snap, fmt.Errorf("%s: %w", spec.Name, err)
		}
		meta := compatSpecMeta(spec.Attributes)
		if !spec.DataSourceOnly {
			r := newDynamicResourceFactory(spec, endpoint)()
			var mresp resource.MetadataResponse
			r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "duploai"}, &mresp)
			var sresp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sresp)
			if sresp.Diagnostics.HasError() {
				return snap, fmt.Errorf("%s schema: %v", spec.Name, sresp.Diagnostics)
			}
			ent := compatEntity{SchemaVersion: sresp.Schema.Version, HasUpdate: endpoint.HasUpdate(), Attributes: map[string]compatAttr{}}
			compatWalkResourceAttrs("", sresp.Schema.Attributes, ent.Attributes)
			compatWalkResourceBlocks("", sresp.Schema.Blocks, ent.Attributes)
			for p, m := range meta {
				if a, ok := ent.Attributes[p]; ok {
					a.ForceNew, a.CreateOnly, a.Default = m.ForceNew, m.CreateOnly, m.Default
					ent.Attributes[p] = a
				}
			}
			snap.Resources[mresp.TypeName] = ent
		}
		if spec.DataSource || spec.DataSourceOnly {
			d := newDynamicDataSourceFactory(spec, endpoint)()
			var mresp datasource.MetadataResponse
			d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "duploai"}, &mresp)
			var sresp datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &sresp)
			if sresp.Diagnostics.HasError() {
				return snap, fmt.Errorf("%s data source schema: %v", spec.Name, sresp.Diagnostics)
			}
			ent := compatEntity{Attributes: map[string]compatAttr{}}
			compatWalkDSAttrs("", sresp.Schema.Attributes, ent.Attributes)
			snap.DataSources[mresp.TypeName] = ent
		}
	}
	return snap, nil
}

// compatSpecMeta maps dotted attribute paths to spec-only flags.
func compatSpecMeta(attrs []AttributeSpec) map[string]compatAttr {
	out := map[string]compatAttr{}
	var walk func(prefix string, as []AttributeSpec, parentForceNew bool)
	walk = func(prefix string, as []AttributeSpec, parentForceNew bool) {
		for _, a := range as {
			p := prefix + a.Name
			fn := a.ForceNew || parentForceNew
			ca := compatAttr{ForceNew: fn, CreateOnly: a.CreateOnly}
			if a.Default != nil {
				ca.Default = string(*a.Default)
			}
			out[p] = ca
			walk(p+".", a.Attributes, fn)
		}
	}
	walk("", attrs, false)
	return out
}

func compatWalkResourceAttrs(prefix string, attrs map[string]rschema.Attribute, out map[string]compatAttr) {
	for name, a := range attrs {
		p := prefix + name
		ca := compatAttr{Required: a.IsRequired(), Optional: a.IsOptional(), Computed: a.IsComputed(),
			Sensitive: a.IsSensitive(), WriteOnly: a.IsWriteOnly(), Type: a.GetType().String()}
		switch n := a.(type) {
		case rschema.SingleNestedAttribute:
			ca.Type = "nested:single"
			compatWalkResourceAttrs(p+".", n.Attributes, out)
		case rschema.ListNestedAttribute:
			ca.Type = "nested:list"
			compatWalkResourceAttrs(p+".", n.NestedObject.Attributes, out)
		case rschema.SetNestedAttribute:
			ca.Type = "nested:set"
			compatWalkResourceAttrs(p+".", n.NestedObject.Attributes, out)
		case rschema.MapNestedAttribute:
			ca.Type = "nested:map"
			compatWalkResourceAttrs(p+".", n.NestedObject.Attributes, out)
		}
		out[p] = ca
	}
}

func compatWalkResourceBlocks(prefix string, blocks map[string]rschema.Block, out map[string]compatAttr) {
	for name, b := range blocks {
		p := prefix + name
		switch n := b.(type) {
		case rschema.SingleNestedBlock:
			out[p] = compatAttr{Type: "block:single", Optional: true}
			compatWalkResourceAttrs(p+".", n.Attributes, out)
			compatWalkResourceBlocks(p+".", n.Blocks, out)
		case rschema.ListNestedBlock:
			out[p] = compatAttr{Type: "block:list", Optional: true}
			compatWalkResourceAttrs(p+".", n.NestedObject.Attributes, out)
			compatWalkResourceBlocks(p+".", n.NestedObject.Blocks, out)
		case rschema.SetNestedBlock:
			out[p] = compatAttr{Type: "block:set", Optional: true}
			compatWalkResourceAttrs(p+".", n.NestedObject.Attributes, out)
			compatWalkResourceBlocks(p+".", n.NestedObject.Blocks, out)
		default:
			out[p] = compatAttr{Type: fmt.Sprintf("block:%T", b), Optional: true}
		}
	}
}

func compatWalkDSAttrs(prefix string, attrs map[string]dsschema.Attribute, out map[string]compatAttr) {
	for name, a := range attrs {
		p := prefix + name
		ca := compatAttr{Required: a.IsRequired(), Optional: a.IsOptional(), Computed: a.IsComputed(),
			Sensitive: a.IsSensitive(), WriteOnly: a.IsWriteOnly(), Type: a.GetType().String()}
		switch n := a.(type) {
		case dsschema.SingleNestedAttribute:
			ca.Type = "nested:single"
			compatWalkDSAttrs(p+".", n.Attributes, out)
		case dsschema.ListNestedAttribute:
			ca.Type = "nested:list"
			compatWalkDSAttrs(p+".", n.NestedObject.Attributes, out)
		case dsschema.SetNestedAttribute:
			ca.Type = "nested:set"
			compatWalkDSAttrs(p+".", n.NestedObject.Attributes, out)
		case dsschema.MapNestedAttribute:
			ca.Type = "nested:map"
			compatWalkDSAttrs(p+".", n.NestedObject.Attributes, out)
		}
		out[p] = ca
	}
}

// compatCurrentSnapshot builds the schema of the provider as compiled now,
// and cross-checks that it covers exactly what the provider registers.
func compatCurrentSnapshot(t *testing.T) compatSnapshot {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loading current specs: %v", err)
	}
	snap, err := compatSnapshotFromSpecs(specs, "current", "embedded specs")
	if err != nil {
		t.Fatalf("building current snapshot: %v", err)
	}
	ctx := context.Background()
	p := New()
	for _, f := range p.Resources(ctx) {
		var m resource.MetadataResponse
		f().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "duploai"}, &m)
		if _, ok := snap.Resources[m.TypeName]; !ok {
			t.Fatalf("provider registers resource %s that the snapshot builder missed", m.TypeName)
		}
	}
	for _, f := range p.DataSources(ctx) {
		var m datasource.MetadataResponse
		f().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "duploai"}, &m)
		if _, ok := snap.DataSources[m.TypeName]; !ok {
			t.Fatalf("provider registers data source %s that the snapshot builder missed", m.TypeName)
		}
	}
	if len(p.Resources(ctx)) != len(snap.Resources) || len(p.DataSources(ctx)) != len(snap.DataSources) {
		t.Fatalf("snapshot/provider registration count mismatch: resources %d/%d data sources %d/%d",
			len(snap.Resources), len(p.Resources(ctx)), len(snap.DataSources), len(p.DataSources(ctx)))
	}
	return snap
}

// ── checker ───────────────────────────────────────────────────────────────────

func compatParent(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i]
	}
	return ""
}

// compatCompare reports every breaking difference from old to new. Additive
// changes (new resources, new optional/computed attributes, required →
// optional, forceNew removed) are allowed.
func compatCompare(old, cur compatSnapshot) []compatViolation {
	var out []compatViolation
	add := func(key, format string, args ...any) {
		out = append(out, compatViolation{Key: key, Msg: fmt.Sprintf(format, args...)})
	}
	check := func(kind string, oldSet, newSet map[string]compatEntity, isResource bool) {
		for _, name := range compatSortedKeys(oldSet) {
			o := oldSet[name]
			n, ok := newSet[name]
			if !ok {
				add(fmt.Sprintf("%s.%s:removed", kind, name), "%s %s was released in %s and is no longer registered", kind, name, old.Version)
				continue
			}
			base := kind + "." + name
			if isResource && o.HasUpdate && !n.HasUpdate {
				add(base+":lost-update", "%s %s no longer supports in-place update: every change now fails or replaces", kind, name)
			}
			if n.SchemaVersion < o.SchemaVersion {
				add(base+":schema-version-decreased", "%s %s schema version went %d -> %d; stored state cannot be read", kind, name, o.SchemaVersion, n.SchemaVersion)
			}
			for _, p := range compatSortedAttrKeys(o.Attributes) {
				oa := o.Attributes[p]
				na, ok := n.Attributes[p]
				k := base + "." + p
				if !ok {
					add(k+":removed", "%s %s: attribute %q removed (configs/state that set or reference it fail)", kind, name, p)
					continue
				}
				if oa.Type != na.Type {
					add(k+":type-changed", "%s %s: attribute %q type changed %s -> %s", kind, name, p, oa.Type, na.Type)
				}
				if !oa.Required && na.Required {
					add(k+":now-required", "%s %s: attribute %q became required (configs that omit it fail)", kind, name, p)
				}
				if isResource && !oa.ForceNew && na.ForceNew {
					add(k+":now-forcenew", "%s %s: attribute %q became forceNew (an in-place change now replaces the resource)", kind, name, p)
				}
				if isResource && !oa.CreateOnly && na.CreateOnly && !na.ForceNew {
					add(k+":now-createonly", "%s %s: attribute %q became createOnly (changes are no longer sent on update)", kind, name, p)
				}
				if isResource && oa.Default != na.Default {
					add(k+":default-changed", "%s %s: attribute %q default changed %q -> %q (existing resources that omit it now plan a change)", kind, name, p, oa.Default, na.Default)
				}
				if oa.Computed && !na.Computed {
					add(k+":no-longer-computed", "%s %s: attribute %q is no longer computed (stored/unset values now plan a diff)", kind, name, p)
				}
				if oa.Sensitive && !na.Sensitive {
					add(k+":no-longer-sensitive", "%s %s: attribute %q is no longer sensitive (value would be shown in plans/output)", kind, name, p)
				}
				if !oa.WriteOnly && na.WriteOnly {
					add(k+":now-write-only", "%s %s: attribute %q became write-only (no longer stored in state)", kind, name, p)
				}
			}
			for _, p := range compatSortedAttrKeys(n.Attributes) {
				if _, existed := o.Attributes[p]; existed || !n.Attributes[p].Required {
					continue
				}
				// A new required child under a new parent only binds configs that
				// opt into the new parent; under an existing parent it breaks them.
				if parent := compatParent(p); parent == "" || o.Attributes[parent] != (compatAttr{}) {
					add(base+"."+p+":new-required", "%s %s: new required attribute %q (existing configs omit it)", kind, name, p)
				}
			}
		}
	}
	check("resource", old.Resources, cur.Resources, true)
	check("data_source", old.DataSources, cur.DataSources, false)
	return out
}

// compatAdditions lists the non-breaking differences (logged by the gate so a
// reviewer sees what changed, not only what broke).
func compatAdditions(old, cur compatSnapshot) []string {
	var out []string
	scan := func(kind string, oldSet, newSet map[string]compatEntity) {
		for _, name := range compatSortedKeys(newSet) {
			n := newSet[name]
			o, ok := oldSet[name]
			if !ok {
				out = append(out, fmt.Sprintf("new %s %s", kind, name))
				continue
			}
			if n.SchemaVersion != o.SchemaVersion {
				out = append(out, fmt.Sprintf("%s %s: schema version %d -> %d", kind, name, o.SchemaVersion, n.SchemaVersion))
			}
			for _, p := range compatSortedAttrKeys(n.Attributes) {
				na := n.Attributes[p]
				oa, existed := o.Attributes[p]
				switch {
				case !existed:
					out = append(out, fmt.Sprintf("%s %s: new attribute %s %+v", kind, name, p, na))
				case oa != na:
					out = append(out, fmt.Sprintf("%s %s: %s %+v -> %+v", kind, name, p, oa, na))
				}
			}
		}
	}
	scan("resource", old.Resources, cur.Resources)
	scan("data_source", old.DataSources, cur.DataSources)
	return out
}

func compatSortedKeys(m map[string]compatEntity) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func compatSortedAttrKeys(m map[string]compatAttr) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// compatApplyAllowlist splits violations into unexpected and allowed, and
// returns allowlist entries that matched nothing (stale entries must go).
func compatApplyAllowlist(vs []compatViolation, allow []compatAllowEntry) (unexpected, allowed []compatViolation, stale []compatAllowEntry) {
	used := map[string]bool{}
	idx := map[string]bool{}
	for _, e := range allow {
		idx[e.Key] = true
	}
	for _, v := range vs {
		if idx[v.Key] {
			used[v.Key] = true
			allowed = append(allowed, v)
		} else {
			unexpected = append(unexpected, v)
		}
	}
	for _, e := range allow {
		if !used[e.Key] {
			stale = append(stale, e)
		}
	}
	return
}

func compatReadBaseline(t *testing.T) compatSnapshot {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(compatDir, compatBaselineVersion+".json"))
	if err != nil {
		t.Fatalf("reading baseline (generate with UPDATE_COMPAT_BASELINE=1): %v", err)
	}
	var snap compatSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("parsing baseline: %v", err)
	}
	return snap
}

func compatReadAllowlist(t *testing.T) []compatAllowEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(compatDir, "allowed.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading allowlist: %v", err)
	}
	var entries []compatAllowEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parsing allowlist: %v", err)
	}
	for _, e := range entries {
		if e.Key == "" || strings.TrimSpace(e.Reason) == "" {
			t.Fatalf("allowlist entry %+v: key and reason are both required", e)
		}
	}
	return entries
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestCompatSchema_v067 is the gate: the current provider must stay
// backward compatible with every resource and data source released in v0.6.7.
func TestCompatSchema_v067(t *testing.T) {
	baseline := compatReadBaseline(t)
	if len(baseline.Resources) == 0 || len(baseline.DataSources) == 0 {
		t.Fatalf("baseline is empty")
	}
	cur := compatCurrentSnapshot(t)
	unexpected, allowed, stale := compatApplyAllowlist(compatCompare(baseline, cur), compatReadAllowlist(t))
	t.Logf("checked %d resources and %d data sources from %s; %d allowlisted differences",
		len(baseline.Resources), len(baseline.DataSources), baseline.Version, len(allowed))
	for _, a := range compatAdditions(baseline, cur) {
		t.Logf("compatible change: %s", a)
	}
	for _, v := range unexpected {
		t.Errorf("BREAKING [%s]: %s", v.Key, v.Msg)
	}
	for _, e := range stale {
		t.Errorf("stale allowlist entry %q matched no difference; remove it", e.Key)
	}
}

// TestCompatBaseline_Generate regenerates testdata/compat/v0.6.7.json from the
// extracted v0.6.7 specs. Only runs with UPDATE_COMPAT_BASELINE=1.
func TestCompatBaseline_Generate(t *testing.T) {
	if os.Getenv("UPDATE_COMPAT_BASELINE") != "1" {
		t.Skip("set UPDATE_COMPAT_BASELINE=1 to regenerate the compat baseline")
	}
	dir := filepath.Join(compatDir, compatBaselineVersion+"-specs")
	specs, err := compatLoadSpecDir(t, dir)
	if err != nil {
		t.Fatalf("loading %s: %v", dir, err)
	}
	snap, err := compatSnapshotFromSpecs(specs, compatBaselineVersion,
		"git show "+compatBaselineVersion+":duplocloud/specs/*.json, built through the engine schema builders")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(compatDir, compatBaselineVersion+".json")
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s: %d resources, %d data sources", out, len(snap.Resources), len(snap.DataSources))
}

// TestCompatBaseline_MatchesReleasedBinary proves the baseline is the schema
// that actually shipped: it compares it with `terraform providers schema
// -json` output from the registry v0.6.7 binary (type-agnostic fields only:
// presence, required/optional/computed, sensitive). Opt-in via
// COMPAT_RELEASED_SCHEMA_JSON.
func TestCompatBaseline_MatchesReleasedBinary(t *testing.T) {
	path := os.Getenv("COMPAT_RELEASED_SCHEMA_JSON")
	if path == "" {
		t.Skip("set COMPAT_RELEASED_SCHEMA_JSON to a `terraform providers schema -json` dump of v0.6.7")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dump struct {
		ProviderSchemas map[string]struct {
			ResourceSchemas   map[string]tfSchemaEntry `json:"resource_schemas"`
			DataSourceSchemas map[string]tfSchemaEntry `json:"data_source_schemas"`
		} `json:"provider_schemas"`
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	ps, ok := dump.ProviderSchemas["registry.terraform.io/duplocloud/duploai"]
	if !ok {
		t.Fatalf("dump has no registry.terraform.io/duplocloud/duploai schema")
	}
	baseline := compatReadBaseline(t)
	cmp := func(kind string, released map[string]tfSchemaEntry, ours map[string]compatEntity) {
		if len(released) != len(ours) {
			t.Errorf("%s count: released %d, baseline %d", kind, len(released), len(ours))
		}
		for name, rs := range released {
			ent, ok := ours[name]
			if !ok {
				t.Errorf("%s %s in released binary but not in baseline", kind, name)
				continue
			}
			flat := map[string]compatAttr{}
			rs.Block.flatten("", flat)
			if rs.Version != ent.SchemaVersion {
				t.Errorf("%s %s: schema version released %d, baseline %d", kind, name, rs.Version, ent.SchemaVersion)
			}
			for p, ra := range flat {
				ba, ok := ent.Attributes[p]
				if !ok {
					t.Errorf("%s %s: %s in released binary but not in baseline", kind, name, p)
					continue
				}
				if ra.Required != ba.Required || ra.Optional != ba.Optional || ra.Computed != ba.Computed ||
					ra.Sensitive != ba.Sensitive || ra.WriteOnly != ba.WriteOnly {
					t.Errorf("%s %s: %s released %+v, baseline %+v", kind, name, p, ra, ba)
				}
			}
			for p := range ent.Attributes {
				if _, ok := flat[p]; !ok {
					t.Errorf("%s %s: %s in baseline but not in released binary", kind, name, p)
				}
			}
		}
	}
	cmp("resource", ps.ResourceSchemas, baseline.Resources)
	cmp("data_source", ps.DataSourceSchemas, baseline.DataSources)
}

type tfSchemaEntry struct {
	Version int64   `json:"version"`
	Block   tfBlock `json:"block"`
}

type tfBlock struct {
	Attributes map[string]tfAttribute `json:"attributes"`
	BlockTypes map[string]struct {
		NestingMode string  `json:"nesting_mode"`
		Block       tfBlock `json:"block"`
	} `json:"block_types"`
}

type tfAttribute struct {
	Required   bool     `json:"required"`
	Optional   bool     `json:"optional"`
	Computed   bool     `json:"computed"`
	Sensitive  bool     `json:"sensitive"`
	WriteOnly  bool     `json:"write_only"`
	NestedType *tfBlock `json:"nested_type"`
}

func (b tfBlock) flatten(prefix string, out map[string]compatAttr) {
	for name, a := range b.Attributes {
		out[prefix+name] = compatAttr{Required: a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, WriteOnly: a.WriteOnly}
		if a.NestedType != nil {
			a.NestedType.flatten(prefix+name+".", out)
		}
	}
	for name, bt := range b.BlockTypes {
		out[prefix+name] = compatAttr{Optional: true}
		bt.Block.flatten(prefix+name+".", out)
	}
}
