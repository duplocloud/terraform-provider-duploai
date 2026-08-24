package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestNormalizeCsvOrder(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"single", "b-1:9094", "b-1:9094"},
		{"already sorted", "b-1:9094,b-2:9094", "b-1:9094,b-2:9094"},
		{"reversed", "b-2:9094,b-1:9094", "b-1:9094,b-2:9094"},
		{"three out of order", "b-3,b-1,b-2", "b-1,b-2,b-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCsvOrder(tc.in); got != tc.want {
				t.Fatalf("normalizeCsvOrder(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A minor-precision version config must not drift when the backend resolves it to
// a patch version (e.g. AKS "1.35.6" → "1.35"); minor-only values pass through.
func TestNormalizeVersionMinor(t *testing.T) {
	cases := map[string]string{
		"1.35.6":   "1.35", // AKS resolved patch
		"1.35":     "1.35", // EKS / already minor
		"1":        "1",    // single component
		"1.35.6.7": "1.35", // extra components
		"":         "",
	}
	for in, want := range cases {
		if got := normalizeVersionMinor(in); got != want {
			t.Errorf("normalizeVersionMinor(%q) = %q, want %q", in, got, want)
		}
	}
}

// Two API responses that differ only in broker order must yield identical state
// values, so a refresh sees no drift.
func TestAttrFromResponseNormalizesCsvOrder(t *testing.T) {
	a := AttributeSpec{Name: "bootstrap_brokers_tls", Type: "string", NormalizeCsvOrder: true}
	v1 := attrFromResponse(a, tftypes.String, "b-2:9094,b-1:9094")
	v2 := attrFromResponse(a, tftypes.String, "b-1:9094,b-2:9094")
	if !v1.Equal(v2) {
		t.Fatalf("expected normalized values to be equal, got %v vs %v", v1, v2)
	}
}
