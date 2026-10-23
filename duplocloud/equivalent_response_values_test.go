package duplocloud

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func tierAttr() AttributeSpec {
	return AttributeSpec{
		Name: "tier", Type: "string", Optional: true, Computed: true,
		OneOf:                    []string{"Standard", "Advanced", "Intelligent-Tiering"},
		CreatePath:               "spec.createRequest.tier",
		ResponsePath:             "result.cloudDetails.tier",
		EquivalentResponseValues: map[string][]string{"Intelligent-Tiering": {"Standard", "Advanced"}},
	}
}

func str(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

func TestKeepEquivalentValue(t *testing.T) {
	a := tierAttr()
	cases := []struct {
		name        string
		prior, next tftypes.Value
		want        tftypes.Value
	}{
		{"resolved Standard keeps configured", str("Intelligent-Tiering"), str("Standard"), str("Intelligent-Tiering")},
		{"resolved Advanced keeps configured", str("Intelligent-Tiering"), str("Advanced"), str("Intelligent-Tiering")},
		{"same value", str("Intelligent-Tiering"), str("Intelligent-Tiering"), str("Intelligent-Tiering")},
		{"Standard configured, Advanced reported: real change", str("Standard"), str("Advanced"), str("Advanced")},
		{"no prior (import): resolved value lands", tftypes.NewValue(tftypes.String, nil), str("Standard"), str("Standard")},
		{"unknown prior (unset optional+computed)", tftypes.NewValue(tftypes.String, tftypes.UnknownValue), str("Standard"), str("Standard")},
		{"null response wins", str("Intelligent-Tiering"), tftypes.NewValue(tftypes.String, nil), tftypes.NewValue(tftypes.String, nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := keepEquivalentValue(a, c.prior, c.next); !got.Equal(c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}

	plain := tierAttr()
	plain.EquivalentResponseValues = nil
	if got := keepEquivalentValue(plain, str("Intelligent-Tiering"), str("Standard")); !got.Equal(str("Standard")) {
		t.Errorf("without the flag the response must win, got %v", got)
	}
}

// A refresh with Intelligent-Tiering in state and Standard in the response keeps
// Intelligent-Tiering, so the next plan is clean.
func TestEquivalentResponseValuesOnRefresh(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"id": tftypes.String, "tier": tftypes.String}}
	resp := map[string]any{"result": map[string]any{"cloudDetails": map[string]any{"tier": "Standard"}}}
	for _, held := range []string{"Intelligent-Tiering", "Advanced"} {
		base := tftypes.NewValue(objType, map[string]tftypes.Value{"id": str("ws/1"), "tier": str(held)})
		var diags diag.Diagnostics
		out := buildStateRaw([]AttributeSpec{tierAttr()}, base, resp, nil, "ws/1", true, true, &diags)
		if diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		var m map[string]tftypes.Value
		_ = out.As(&m)
		want := map[string]string{"Intelligent-Tiering": "Intelligent-Tiering", "Advanced": "Standard"}[held]
		if !m["tier"].Equal(str(want)) {
			t.Errorf("held %s: tier = %v, want %s", held, m["tier"], want)
		}
	}
}

func TestEquivalentResponseValuesValidation(t *testing.T) {
	cases := []struct {
		name    string
		attr    AttributeSpec
		wantErr string
	}{
		{"valid", tierAttr(), ""},
		{"non-string", func() AttributeSpec { a := tierAttr(); a.Type = "int"; a.OneOf = nil; return a }(), "requires a string type"},
		{"computed-only", func() AttributeSpec { a := tierAttr(); a.Optional = false; a.CreatePath = ""; return a }(), "user-settable"},
		{"key not in oneOf", func() AttributeSpec {
			a := tierAttr()
			a.EquivalentResponseValues = map[string][]string{"Fast": {"Standard"}}
			return a
		}(), "not one of its oneOf values"},
		{"value equals key", func() AttributeSpec {
			a := tierAttr()
			a.EquivalentResponseValues = map[string][]string{"Intelligent-Tiering": {"Intelligent-Tiering"}}
			return a
		}(), "other than the key"},
		{"empty values", func() AttributeSpec {
			a := tierAttr()
			a.EquivalentResponseValues = map[string][]string{"Intelligent-Tiering": {}}
			return a
		}(), "at least one value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := validateAttributes([]AttributeSpec{c.attr})
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, c.wantErr)
			}
		})
	}

	nested := AttributeSpec{Name: "cfg", Type: "object", Optional: true, APIPath: "cfg", Attributes: []AttributeSpec{tierAttr()}}
	if _, err := validateAttributes([]AttributeSpec{nested}); err == nil || !strings.Contains(err.Error(), "top-level") {
		t.Fatalf("nested use must be rejected, got %v", err)
	}
}

// The shipped ssm_parameter spec keeps Intelligent-Tiering when AWS reports the
// tier it resolved to.
func TestSsmParameterTierEquivalence(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Name != "ssm_parameter" {
			continue
		}
		a := findAttr(s.Attributes, "tier")
		if a == nil {
			t.Fatal("tier attribute missing")
		}
		got := a.EquivalentResponseValues["Intelligent-Tiering"]
		if len(got) != 2 || got[0] != "Standard" || got[1] != "Advanced" {
			t.Fatalf("tier equivalentResponseValues = %v", a.EquivalentResponseValues)
		}
		return
	}
	t.Fatal("ssm_parameter spec not found")
}
