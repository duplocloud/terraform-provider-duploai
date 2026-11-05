package duplocloud

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// extIDAttrsMatchByKey is core-qa's extIDAttrs fixture with the matchByKey
// option the shipped admin_provider spec now sets on credential_fields.
func extIDAttrsMatchByKey() []AttributeSpec {
	attrs := extIDAttrs()
	attrs[1].Attributes[1].MatchByKey = "key"
	return attrs
}

// The shipped spec must carry the option, otherwise the fix is inert.
func TestMatchByKey_AdminProviderSpecSetsIt(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Name != "admin_provider" {
			continue
		}
		creds := findAttr(s.Attributes, "credentials")
		if creds == nil {
			t.Fatal("credentials missing")
		}
		if cf := findAttr(creds.Attributes, "credential_fields"); cf == nil || cf.MatchByKey != "key" {
			t.Fatalf("credentials.credential_fields.matchByKey not set to key")
		}
		return
	}
	t.Fatal("admin_provider spec not found")
}

func TestMatchByKey_ApplyDropsServerAddedElement(t *testing.T) {
	for name, roleValue := range map[string]string{"redacted": "", "echoed": extIDRoleArn} {
		t.Run(name, func(t *testing.T) {
			var diags diag.Diagnostics
			plan := extIDTop([]tftypes.Value{extIDPlanField("iamRoleArn", extIDRoleArn)}, true)
			state := buildStateRaw(extIDAttrsMatchByKey(), plan,
				extIDResponse([3]any{"iamRoleArn", roleValue, true}, [3]any{"externalId", extIDGenerated, false}),
				map[string]string{}, "p1", false, true, &diags)
			keys, vals := extIDFieldKeys(t, state)
			if len(keys) != 1 || keys[0] != "iamRoleArn" || vals["iamRoleArn"] != extIDRoleArn {
				t.Errorf("keys=%v vals=%v, want [iamRoleArn] with configured value", keys, vals)
			}
		})
	}
}

func TestMatchByKey_RefreshDropsServerAddedElementAndReorders(t *testing.T) {
	var diags diag.Diagnostics
	prior := extIDTop([]tftypes.Value{
		extIDStateField("region", "us-west-2", false), extIDStateField("iamRoleArn", extIDRoleArn, true),
	}, false)
	// Response in a different order, with the server-added externalId first.
	state := buildStateRaw(extIDAttrsMatchByKey(), prior,
		extIDResponse([3]any{"externalId", extIDGenerated, false}, [3]any{"iamRoleArn", "", true}, [3]any{"region", "us-west-2", false}),
		map[string]string{}, "p1", true, true, &diags)
	keys, vals := extIDFieldKeys(t, state)
	if strings.Join(keys, ",") != "region,iamRoleArn" || vals["iamRoleArn"] != extIDRoleArn {
		t.Errorf("keys=%v vals=%v, want [region iamRoleArn] with prior secret", keys, vals)
	}
}

// A real change (value edited out-of-band) must still be visible.
func TestMatchByKey_RefreshStillReportsValueDrift(t *testing.T) {
	var diags diag.Diagnostics
	prior := extIDTop([]tftypes.Value{extIDStateField("region", "us-west-2", false)}, false)
	state := buildStateRaw(extIDAttrsMatchByKey(), prior,
		extIDResponse([3]any{"region", "eu-west-1", false}, [3]any{"externalId", extIDGenerated, false}),
		map[string]string{}, "p1", true, true, &diags)
	_, vals := extIDFieldKeys(t, state)
	if vals["region"] != "eu-west-1" {
		t.Errorf("region=%q, want drifted eu-west-1", vals["region"])
	}
}

// A configured key the backend no longer returns is drift, not hidden.
func TestMatchByKey_RefreshMissingKeyShrinksList(t *testing.T) {
	var diags diag.Diagnostics
	prior := extIDTop([]tftypes.Value{
		extIDStateField("region", "us-west-2", false), extIDStateField("iamRoleArn", extIDRoleArn, true),
	}, false)
	state := buildStateRaw(extIDAttrsMatchByKey(), prior,
		extIDResponse([3]any{"iamRoleArn", "", true}),
		map[string]string{}, "p1", true, true, &diags)
	keys, _ := extIDFieldKeys(t, state)
	if strings.Join(keys, ",") != "iamRoleArn" {
		t.Errorf("keys=%v, want [iamRoleArn]", keys)
	}
}

// Import: no base, so every element the API returns lands in state.
func TestMatchByKey_ImportKeepsAll(t *testing.T) {
	var diags diag.Diagnostics
	base := tftypes.NewValue(extIDTopType, map[string]tftypes.Value{
		"id":          tftypes.NewValue(tftypes.String, "p1"),
		"name":        tftypes.NewValue(tftypes.String, nil),
		"credentials": tftypes.NewValue(extIDCredListType, nil),
	})
	state := buildStateRaw(extIDAttrsMatchByKey(), base,
		extIDResponse([3]any{"iamRoleArn", "", true}, [3]any{"externalId", extIDGenerated, false}),
		map[string]string{}, "p1", true, true, &diags)
	keys, _ := extIDFieldKeys(t, state)
	if strings.Join(keys, ",") != "iamRoleArn,externalId" {
		t.Errorf("keys=%v, want both on import", keys)
	}
}

func TestMatchByKey_SpecValidation(t *testing.T) {
	obj := func(mbk string, keyAttr AttributeSpec, orderBy string) []AttributeSpec {
		return []AttributeSpec{{Name: "l", Type: "list(object)", Optional: true, Computed: true, APIPath: "l",
			MatchByKey: mbk, OrderByKey: orderBy, Attributes: []AttributeSpec{keyAttr}}}
	}
	reqStr := AttributeSpec{Name: "key", Type: "string", Required: true, APIPath: "key"}
	if _, err := validateAttributes(obj("key", reqStr, "")); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	cases := map[string][]AttributeSpec{
		"unknown":  obj("nope", reqStr, ""),
		"optional": obj("key", AttributeSpec{Name: "key", Type: "string", Optional: true, APIPath: "key"}, ""),
		"both":     obj("key", reqStr, "key"),
		"notlist":  {{Name: "m", Type: "map(string)", Optional: true, APIPath: "m", MatchByKey: "key"}},
	}
	for name, attrs := range cases {
		if _, err := validateAttributes(attrs); err == nil || !strings.Contains(err.Error(), "matchByKey") {
			t.Errorf("%s: err=%v, want matchByKey error", name, err)
		}
	}
}
