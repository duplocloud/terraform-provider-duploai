package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// DUPLOAI-4739: for a credential that assumes an AWS role (iamRoleArn) without
// an externalId, the backend (AwsExternalIdRules) generates one and appends a
// dataEx element {key: externalId}. It never changes afterwards and is kept
// when an update omits it. The provider must not surface that server-added
// element as a difference from config.
//
// These fixtures mirror the real admin_provider spec: credential_fields
// elements carry optional+computed `type` and `is_sensitive`, which are
// unknown in the plan when the user does not set them.

var (
	extIDFieldType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"key":          tftypes.String,
		"value":        tftypes.String,
		"type":         tftypes.String,
		"is_sensitive": tftypes.Bool,
	}}
	extIDFieldListType = tftypes.List{ElementType: extIDFieldType}
	extIDCredType      = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"name":              tftypes.String,
		"credential_fields": extIDFieldListType,
		"metadata":          tftypes.Map{ElementType: tftypes.String},
	}}
	extIDCredListType = tftypes.List{ElementType: extIDCredType}
	extIDTopType      = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":          tftypes.String,
		"name":        tftypes.String,
		"credentials": extIDCredListType,
	}}
)

func extIDAttrs() []AttributeSpec {
	return []AttributeSpec{
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
		{
			Name: "credentials", Type: "list(object)", Optional: true, Computed: true, APIPath: "credentials",
			Attributes: []AttributeSpec{
				{Name: "name", Type: "string", Required: true, APIPath: "name"},
				{
					Name: "credential_fields", Type: "list(object)", Optional: true, Computed: true, APIPath: "dataEx", MatchByKey: "key",
					Attributes: []AttributeSpec{
						{Name: "key", Type: "string", Required: true, APIPath: "key"},
						{Name: "value", Type: "string", Optional: true, Sensitive: true, APIPath: "value", PreserveOnEmptyResponse: true},
						{Name: "type", Type: "string", Optional: true, Computed: true, APIPath: "type"},
						{Name: "is_sensitive", Type: "bool", Optional: true, Computed: true, APIPath: "isSensitive"},
					},
				},
				{Name: "metadata", Type: "map(string)", Optional: true, Computed: true, APIPath: "metaData"},
			},
		},
	}
}

// extIDPlanField is a configured element as planned: key/value known, the
// unset optional+computed type/is_sensitive unknown.
func extIDPlanField(key, value string) tftypes.Value {
	return tftypes.NewValue(extIDFieldType, map[string]tftypes.Value{
		"key":          tftypes.NewValue(tftypes.String, key),
		"value":        tftypes.NewValue(tftypes.String, value),
		"type":         tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"is_sensitive": tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
	})
}

// extIDStateField is an element as stored in state after a successful apply.
func extIDStateField(key, value string, sensitive bool) tftypes.Value {
	return tftypes.NewValue(extIDFieldType, map[string]tftypes.Value{
		"key":          tftypes.NewValue(tftypes.String, key),
		"value":        tftypes.NewValue(tftypes.String, value),
		"type":         tftypes.NewValue(tftypes.String, "string"),
		"is_sensitive": tftypes.NewValue(tftypes.Bool, sensitive),
	})
}

func extIDTop(fields []tftypes.Value, metadataUnknown bool) tftypes.Value {
	md := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)
	if metadataUnknown {
		md = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, tftypes.UnknownValue)
	}
	return tftypes.NewValue(extIDTopType, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"name": tftypes.NewValue(tftypes.String, "aws"),
		"credentials": tftypes.NewValue(extIDCredListType, []tftypes.Value{
			tftypes.NewValue(extIDCredType, map[string]tftypes.Value{
				"name":              tftypes.NewValue(tftypes.String, "default"),
				"credential_fields": tftypes.NewValue(extIDFieldListType, fields),
				"metadata":          md,
			}),
		}),
	})
}

// extIDResponse: dataEx as returned by the backend. Each entry is {key, value, sensitive}.
func extIDResponse(entries ...[3]any) map[string]any {
	dataEx := make([]any, 0, len(entries))
	for _, e := range entries {
		dataEx = append(dataEx, map[string]any{"key": e[0], "value": e[1], "type": "string", "isSensitive": e[2]})
	}
	return map[string]any{
		"id": "p1", "name": "aws",
		"credentials": []any{map[string]any{"name": "default", "dataEx": dataEx, "metaData": nil}},
	}
}

const (
	extIDRoleArn   = "arn:aws:iam::123456789012:role/duplo"
	extIDGenerated = "6f1c2b9e-0d3a-4c1e-9a77-1b2c3d4e5f60"
)

// extIDFieldKeys returns credentials[0].credential_fields as ordered keys and a key→value map.
func extIDFieldKeys(t *testing.T, state tftypes.Value) ([]string, map[string]string) {
	t.Helper()
	var top map[string]tftypes.Value
	if err := state.As(&top); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	var creds []tftypes.Value
	if err := top["credentials"].As(&creds); err != nil || len(creds) != 1 {
		t.Fatalf("decode credentials: %v (n=%d)", err, len(creds))
	}
	var cred map[string]tftypes.Value
	_ = creds[0].As(&cred)
	var fields []tftypes.Value
	if err := cred["credential_fields"].As(&fields); err != nil {
		t.Fatalf("decode credential_fields: %v", err)
	}
	var keys []string
	vals := map[string]string{}
	for _, f := range fields {
		var fm map[string]tftypes.Value
		_ = f.As(&fm)
		var k, v string
		_ = fm["key"].As(&k)
		_ = fm["value"].As(&v)
		keys = append(keys, k)
		vals[k] = v
	}
	return keys, vals
}

// Create (apply): plan has only iamRoleArn; the response adds the generated
// externalId. Terraform requires the applied credential_fields to keep the
// planned length and known leaves, otherwise it aborts with
// "inconsistent values for sensitive attribute".
func TestAdminProviderExternalID_CreateMatchesPlan(t *testing.T) {
	for name, roleValue := range map[string]string{"redacted": "", "echoed": extIDRoleArn} {
		t.Run(name, func(t *testing.T) {
			var diags diag.Diagnostics
			plan := extIDTop([]tftypes.Value{extIDPlanField("iamRoleArn", extIDRoleArn)}, true)
			state := buildStateRaw(extIDAttrs(), plan,
				extIDResponse([3]any{"iamRoleArn", roleValue, true}, [3]any{"externalId", extIDGenerated, false}),
				map[string]string{}, "p1", false, true, &diags)
			if diags.HasError() {
				t.Fatalf("diags: %v", diags)
			}
			keys, vals := extIDFieldKeys(t, state)
			if len(keys) != 1 || keys[0] != "iamRoleArn" {
				t.Errorf("credential_fields keys = %v, want [iamRoleArn] (plan length must be kept; server-added externalId must not appear)", keys)
			}
			if vals["iamRoleArn"] != extIDRoleArn {
				t.Errorf("iamRoleArn value = %q, want the configured value", vals["iamRoleArn"])
			}
		})
	}
}

// Refresh after a successful apply: prior state holds only iamRoleArn; the
// read returns iamRoleArn + externalId. If the extra element reaches state,
// the next plan (config = [iamRoleArn]) shows a diff that can never converge,
// because the backend keeps externalId when an update omits it.
func TestAdminProviderExternalID_RefreshNoDiff(t *testing.T) {
	var diags diag.Diagnostics
	prior := extIDTop([]tftypes.Value{extIDStateField("iamRoleArn", extIDRoleArn, true)}, false)
	state := buildStateRaw(extIDAttrs(), prior,
		extIDResponse([3]any{"iamRoleArn", "", true}, [3]any{"externalId", extIDGenerated, false}),
		map[string]string{}, "p1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	keys, vals := extIDFieldKeys(t, state)
	if len(keys) != 1 || keys[0] != "iamRoleArn" {
		t.Errorf("credential_fields keys after refresh = %v, want [iamRoleArn] (no diff against config)", keys)
	}
	if vals["iamRoleArn"] != extIDRoleArn {
		t.Errorf("iamRoleArn value = %q, want the prior state value", vals["iamRoleArn"])
	}
}

// Workaround config (externalId set explicitly) must keep round-tripping
// unchanged on both apply and refresh. Passes today; guards the fix.
func TestAdminProviderExternalID_ExplicitExternalIDRoundTrips(t *testing.T) {
	resp := extIDResponse([3]any{"iamRoleArn", "", true}, [3]any{"externalId", "my-ext-id", false})

	var diags diag.Diagnostics
	plan := extIDTop([]tftypes.Value{
		extIDPlanField("iamRoleArn", extIDRoleArn), extIDPlanField("externalId", "my-ext-id"),
	}, true)
	state := buildStateRaw(extIDAttrs(), plan, resp, map[string]string{}, "p1", false, true, &diags)
	if diags.HasError() {
		t.Fatalf("apply diags: %v", diags)
	}
	keys, vals := extIDFieldKeys(t, state)
	if len(keys) != 2 || keys[0] != "iamRoleArn" || keys[1] != "externalId" ||
		vals["iamRoleArn"] != extIDRoleArn || vals["externalId"] != "my-ext-id" {
		t.Errorf("apply: keys=%v vals=%v, want configured iamRoleArn+externalId", keys, vals)
	}

	prior := extIDTop([]tftypes.Value{
		extIDStateField("iamRoleArn", extIDRoleArn, true), extIDStateField("externalId", "my-ext-id", false),
	}, false)
	state = buildStateRaw(extIDAttrs(), prior, resp, map[string]string{}, "p1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("refresh diags: %v", diags)
	}
	if !state.Equal(func() tftypes.Value {
		var m map[string]tftypes.Value
		_ = prior.As(&m)
		m["id"] = tftypes.NewValue(tftypes.String, "p1")
		return tftypes.NewValue(extIDTopType, m)
	}()) {
		keys, vals = extIDFieldKeys(t, state)
		t.Errorf("refresh changed state: keys=%v vals=%v", keys, vals)
	}
}
