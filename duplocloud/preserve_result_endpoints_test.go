package duplocloud

import (
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The connection outputs of duploai_rds_instance and duploai_elasticache come
// from the response's result block, which the platform leaves out of the
// response to an in-place update (storage type, Performance Insights, a
// replica, snapshot settings). Without stable + preserveOnEmptyResponse the
// endpoint landed in state as null and every consumer that formats it —
// "${endpoint}:${port}", split(":", primary_endpoint) — failed the run.

// specAttrs returns the named attributes exactly as the embedded spec declares
// them, so the test exercises the shipped flags rather than a copy of them.
func specAttrs(t *testing.T, resource string, names ...string) []AttributeSpec {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, s := range specs {
		if s.Name != resource {
			continue
		}
		var out []AttributeSpec
		for _, n := range names {
			a := findAttr(s.Attributes, n)
			if a == nil {
				t.Fatalf("%s: no attribute %q", resource, n)
			}
			if !a.Stable || !a.PreserveOnEmptyResponse {
				t.Errorf("%s.%s: stable=%v preserveOnEmptyResponse=%v, want both true", resource, n, a.Stable, a.PreserveOnEmptyResponse)
			}
			out = append(out, *a)
		}
		return out
	}
	t.Fatalf("no embedded spec %q", resource)
	return nil
}

type endpointCase struct {
	resource string
	attrs    []string
	types    map[string]tftypes.Type
	prior    map[string]any
	// fresh is a non-empty result block, which must replace the prior values.
	fresh map[string]any
	want  map[string]any
}

func endpointCases() []endpointCase {
	return []endpointCase{
		{
			resource: "rds_instance",
			attrs:    []string{"endpoint", "port", "stack_id"},
			types:    map[string]tftypes.Type{"endpoint": tftypes.String, "port": tftypes.Number, "stack_id": tftypes.String},
			prior:    map[string]any{"endpoint": "db.abc.us-west-2.rds.amazonaws.com", "port": int64(5432), "stack_id": "stack-1"},
			fresh: map[string]any{"result": map[string]any{
				"stackId": "stack-1",
				"details": map[string]any{"Endpoint": map[string]any{"Address": "db.new.us-west-2.rds.amazonaws.com", "Port": 6432}},
			}},
			want: map[string]any{"endpoint": "db.new.us-west-2.rds.amazonaws.com", "port": int64(6432)},
		},
		{
			resource: "elasticache",
			attrs:    []string{"primary_endpoint", "reader_endpoint", "configuration_endpoint", "stack_id"},
			types: map[string]tftypes.Type{
				"primary_endpoint": tftypes.String, "reader_endpoint": tftypes.String,
				"configuration_endpoint": tftypes.String, "stack_id": tftypes.String,
			},
			prior: map[string]any{
				"primary_endpoint": "master.tenant-redis.abc.usw2.cache.amazonaws.com",
				"reader_endpoint":  "replica.tenant-redis.abc.usw2.cache.amazonaws.com",
				"stack_id":         "stack-2",
			},
			fresh: map[string]any{"result": map[string]any{
				"stackId":         "stack-2",
				"primaryEndpoint": "master.renamed.abc.usw2.cache.amazonaws.com",
			}},
			want: map[string]any{"primary_endpoint": "master.renamed.abc.usw2.cache.amazonaws.com"},
		},
	}
}

func (c endpointCase) base() tftypes.Value {
	types := map[string]tftypes.Type{"id": tftypes.String}
	vals := map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "x1")}
	for k, ty := range c.types {
		types[k] = ty
		vals[k] = tftypes.NewValue(ty, c.prior[k])
	}
	return tftypes.NewValue(tftypes.Object{AttributeTypes: types}, vals)
}

func stateValues(t *testing.T, v tftypes.Value) map[string]any {
	t.Helper()
	m := map[string]tftypes.Value{}
	if err := v.As(&m); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for k, tv := range m {
		if tv.IsNull() {
			out[k] = nil
			continue
		}
		switch {
		case tv.Type().Is(tftypes.String):
			var s string
			_ = tv.As(&s)
			out[k] = s
		case tv.Type().Is(tftypes.Number):
			var f big.Float
			if err := tv.As(&f); err != nil {
				t.Fatal(err)
			}
			i, _ := f.Int64()
			out[k] = i
		}
	}
	return out
}

// Update (refreshInputs=false, base = plan carrying the prior values because
// the attributes are stable) and refresh (refreshInputs=true, base = prior
// state): a response with no result block keeps every connection output.
func TestResultEndpoints_KeptAcrossResponseWithoutResult(t *testing.T) {
	for _, c := range endpointCases() {
		attrs := specAttrs(t, c.resource, c.attrs...)
		for _, refresh := range []bool{false, true} {
			var diags diag.Diagnostics
			out := buildStateRaw(attrs, c.base(),
				map[string]any{"id": "x1", "status": "InProgress"},
				map[string]string{}, "x1", refresh, true, &diags)
			if diags.HasError() {
				t.Fatalf("%s refresh=%v: diags: %v", c.resource, refresh, diags)
			}
			got := stateValues(t, out)
			for k, want := range c.prior {
				if got[k] != want {
					t.Errorf("%s refresh=%v: %s = %v, want the prior %v", c.resource, refresh, k, got[k], want)
				}
			}
		}
	}
}

// Preservation only fills in for a missing value: an endpoint the response
// does carry still replaces the prior one.
func TestResultEndpoints_NonEmptyResultWins(t *testing.T) {
	for _, c := range endpointCases() {
		attrs := specAttrs(t, c.resource, c.attrs...)
		resp := map[string]any{"id": "x1", "status": "Complete"}
		for k, v := range c.fresh {
			resp[k] = v
		}
		var diags diag.Diagnostics
		out := buildStateRaw(attrs, c.base(), resp, map[string]string{}, "x1", true, true, &diags)
		if diags.HasError() {
			t.Fatalf("%s: diags: %v", c.resource, diags)
		}
		got := stateValues(t, out)
		for k, want := range c.want {
			if got[k] != want {
				t.Errorf("%s: %s = %v, want the response value %v", c.resource, k, got[k], want)
			}
		}
	}
}
