package duplocloud

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The platform renders one instant several ways depending on where the value
// came from: the write response carries .NET tick precision straight from
// memory, the read response carries what the datastore kept (milliseconds), and
// the serializer trims trailing zeros and spells a zero offset either way.
// Every spelling must reduce to the same string.
func TestNormalizeTimestampPrecision(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// The reported bug: create/update response vs read response.
		{"tick precision from write response", "2026-06-30T04:37:53.1452297Z", "2026-06-30T04:37:53Z"},
		{"milliseconds from read response", "2026-06-30T04:37:53.145Z", "2026-06-30T04:37:53Z"},

		// Trailing zeros the serializer trims: ".140" arrives as ".14".
		{"trimmed trailing zero", "2026-06-30T04:37:53.14Z", "2026-06-30T04:37:53Z"},
		{"already whole seconds", "2026-06-30T04:37:53Z", "2026-06-30T04:37:53Z"},

		// A zero offset spelled as an offset rather than Z (a .NET
		// DateTimeOffset serializes this way; a UTC DateTime uses Z).
		{"zero offset spelled out", "2026-06-30T04:37:53.145+00:00", "2026-06-30T04:37:53Z"},

		// A real offset is converted to UTC, so the same instant in another
		// spelling still compares equal.
		{"non-zero offset", "2026-06-30T10:07:53.145+05:30", "2026-06-30T04:37:53Z"},

		// Whole seconds are truncated toward the second, never rounded up —
		// which is what makes this immune to the platform truncating or
		// rounding when it persists a higher-precision value.
		{"never rounds up to the next second", "2026-06-30T04:37:53.9999999Z", "2026-06-30T04:37:53Z"},

		// Anything unparseable is stored as the API sent it.
		{"empty", "", ""},
		{"not a timestamp", "n/a", "n/a"},
		{"date only", "2026-06-30", "2026-06-30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeTimestampPrecision(tc.in); got != tc.want {
				t.Fatalf("normalizeTimestampPrecision(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Applying the normalizer to an already-normalized value must not move it —
// state is rebuilt from a response on every refresh.
func TestNormalizeTimestampIsIdempotent(t *testing.T) {
	for _, in := range []string{
		"2026-06-30T04:37:53.1452297Z",
		"2026-06-30T04:37:53.14Z",
		"2026-06-30T10:07:53.145+05:30",
		"not-a-timestamp",
	} {
		once := normalizeTimestampPrecision(in)
		if twice := normalizeTimestampPrecision(once); twice != once {
			t.Errorf("normalizeTimestampPrecision not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}

// The write response and the read response for the same instant must produce
// identical state values — this is exactly what refresh compares, and what was
// reporting "Objects have changed outside of Terraform" on every plan.
func TestAttrFromResponseNormalizesTimestamp(t *testing.T) {
	a := AttributeSpec{Name: "updated_at", Type: "string", Computed: true, APIPath: "updatedAt", NormalizeTimestamp: true}
	fromWrite := attrFromResponse(a, tftypes.String, "2026-06-30T04:37:53.1452297Z")
	fromRead := attrFromResponse(a, tftypes.String, "2026-06-30T04:37:53.145Z")
	if !fromWrite.Equal(fromRead) {
		t.Fatalf("write-response and read-response state values differ: %v vs %v", fromWrite, fromRead)
	}
	if want := tftypes.NewValue(tftypes.String, "2026-06-30T04:37:53Z"); !fromRead.Equal(want) {
		t.Fatalf("got %v, want %v", fromRead, want)
	}
}

// A genuinely later timestamp must still land in state — the normalizer kills
// spelling differences, not real updates.
func TestAttrFromResponseKeepsRealTimestampChange(t *testing.T) {
	a := AttributeSpec{Name: "updated_at", Type: "string", Computed: true, APIPath: "updatedAt", NormalizeTimestamp: true}
	before := attrFromResponse(a, tftypes.String, "2026-06-30T04:37:53.1452297Z")
	after := attrFromResponse(a, tftypes.String, "2026-06-30T04:38:11.9013344Z")
	if before.Equal(after) {
		t.Fatalf("expected a different instant to produce a different state value, both were %v", before)
	}
}

// Without the flag nothing is rewritten, so an existing spec keeps its
// behaviour and the drift is reproducible.
func TestAttrFromResponseWithoutNormalizeTimestamp(t *testing.T) {
	a := AttributeSpec{Name: "updated_at", Type: "string", Computed: true, APIPath: "updatedAt"}
	fromWrite := attrFromResponse(a, tftypes.String, "2026-06-30T04:37:53.1452297Z")
	fromRead := attrFromResponse(a, tftypes.String, "2026-06-30T04:37:53.145Z")
	if fromWrite.Equal(fromRead) {
		t.Fatal("expected the un-normalized values to differ (this is the reported bug)")
	}
}

// normalizeTimestamp on a value the user can set would make state disagree with
// the plan, so the spec loader rejects it rather than shipping a resource that
// fails "inconsistent result after apply". Same for a non-string type, where the
// flag would silently do nothing.
func TestNormalizeTimestampSpecValidation(t *testing.T) {
	cases := []struct {
		name    string
		attr    string
		wantErr string
	}{
		{
			name:    "computed-only is accepted",
			attr:    `{"name":"updated_at","type":"string","computed":true,"apiPath":"updatedAt","normalizeTimestamp":true}`,
			wantErr: "",
		},
		{
			name:    "optional is rejected",
			attr:    `{"name":"expires_on","type":"string","optional":true,"computed":true,"apiPath":"expiresOn","normalizeTimestamp":true}`,
			wantErr: "only valid on a computed-only attribute",
		},
		{
			name:    "required is rejected",
			attr:    `{"name":"expires_on","type":"string","required":true,"apiPath":"expiresOn","normalizeTimestamp":true}`,
			wantErr: "only valid on a computed-only attribute",
		},
		{
			name:    "non-string is rejected",
			attr:    `{"name":"updated_at","type":"int","computed":true,"apiPath":"updatedAt","normalizeTimestamp":true}`,
			wantErr: "requires a string type",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{
			  "name": "ts_probe",
			  "description": "probe",
			  "idPath": "id",
			  "endpoint": {"uriBase": "/v3/admin/things"},
			  "attributes": [` + tc.attr + `]
			}`)
			var spec ResourceSpec
			if err := json.Unmarshal(raw, &spec); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := spec.validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("expected the spec to load, got %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("expected error containing %q, got none", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// A data source and its resource must report the same value for the same
// object: both build state through buildStateRaw, so the flag has to take
// effect on the data source's read path too — otherwise a config comparing
// data.duploai_x.updated_at against duploai_x.updated_at would see two
// different strings for one instant.
func TestBuildStateRawNormalizesTimestampOnDataSourceRead(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":         tftypes.String,
		"name":       tftypes.String,
		"updated_at": tftypes.String,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":         tftypes.NewValue(tftypes.String, "obj-1"),
		"name":       tftypes.NewValue(tftypes.String, nil),
		"updated_at": tftypes.NewValue(tftypes.String, nil),
	})
	attrs := []AttributeSpec{
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
		{Name: "updated_at", Type: "string", Computed: true, APIPath: "updatedAt", NormalizeTimestamp: true},
	}

	// refreshInputs=true is the data source Read path (dynamic_data_source.go).
	var diags diag.Diagnostics
	out := buildStateRaw(attrs, base,
		map[string]any{"name": "thing", "updatedAt": "2026-06-30T04:37:53.1452297Z"},
		map[string]string{}, "obj-1", true, false, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := m["updated_at"].As(&got); err != nil {
		t.Fatal(err)
	}
	if want := "2026-06-30T04:37:53Z"; got != want {
		t.Fatalf("data source updated_at = %q, want %q", got, want)
	}
}
