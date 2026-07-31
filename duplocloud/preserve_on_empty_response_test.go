package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The admin_provider shape: a list of credentials, each holding a list of
// key/value fields (whose value is write-only and redacted by the API) plus an
// optional+computed metadata map that is unknown at plan time.
var (
	credFieldObjType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"key":          tftypes.String,
		"value":        tftypes.String,
		"is_sensitive": tftypes.Bool,
	}}
	credObjType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"name":              tftypes.String,
		"credential_fields": tftypes.List{ElementType: credFieldObjType},
		"metadata":          tftypes.Map{ElementType: tftypes.String},
	}}
	credListType = tftypes.List{ElementType: credObjType}
	provTopType  = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":          tftypes.String,
		"name":        tftypes.String,
		"credentials": credListType,
	}}
)

func credentialsAttr() AttributeSpec {
	return AttributeSpec{
		Name: "credentials", Type: "list(object)", Optional: true, Computed: true, APIPath: "credentials",
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
			{
				Name: "credential_fields", Type: "list(object)", Optional: true, Computed: true, APIPath: "dataEx",
				Attributes: []AttributeSpec{
					{Name: "key", Type: "string", Required: true, APIPath: "key"},
					{Name: "value", Type: "string", Optional: true, Sensitive: true, APIPath: "value", PreserveOnEmptyResponse: true},
					{Name: "is_sensitive", Type: "bool", Optional: true, Computed: true, APIPath: "isSensitive"},
				},
			},
			{Name: "metadata", Type: "map(string)", Optional: true, Computed: true, APIPath: "metaData"},
		},
	}
}

func provAttrs() []AttributeSpec {
	return []AttributeSpec{
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
		credentialsAttr(),
	}
}

// credField builds one credential_fields element. An empty value string models
// what the API returns for a redacted secret.
func credField(key, value string) tftypes.Value {
	return tftypes.NewValue(credFieldObjType, map[string]tftypes.Value{
		"key":          tftypes.NewValue(tftypes.String, key),
		"value":        tftypes.NewValue(tftypes.String, value),
		"is_sensitive": tftypes.NewValue(tftypes.Bool, true),
	})
}

// planCredentials models the plan on create: every configured leaf is known,
// but the optional+computed metadata map is (known after apply).
func planCredentials(secrets ...[2]string) tftypes.Value {
	fields := make([]tftypes.Value, 0, len(secrets))
	for _, kv := range secrets {
		fields = append(fields, credField(kv[0], kv[1]))
	}
	return tftypes.NewValue(credListType, []tftypes.Value{
		tftypes.NewValue(credObjType, map[string]tftypes.Value{
			"name":              tftypes.NewValue(tftypes.String, "default"),
			"credential_fields": tftypes.NewValue(tftypes.List{ElementType: credFieldObjType}, fields),
			"metadata":          tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, tftypes.UnknownValue),
		}),
	})
}

// redactedResponse is what the AI Helpdesk API returns: the credential is echoed
// back with every sensitive value blanked.
func redactedResponse(keys ...string) map[string]any {
	fields := make([]any, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, map[string]any{"key": k, "value": "", "isSensitive": true})
	}
	return map[string]any{
		"id":   "p1",
		"name": "onprem",
		"credentials": []any{
			map[string]any{"name": "default", "dataEx": fields, "metaData": nil},
		},
	}
}

// secretsInState reads back credentials[0].credential_fields as key→value.
func secretsInState(t *testing.T, state tftypes.Value) map[string]string {
	t.Helper()
	var top map[string]tftypes.Value
	if err := state.As(&top); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	var creds []tftypes.Value
	if err := top["credentials"].As(&creds); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}
	var cred map[string]tftypes.Value
	if err := creds[0].As(&cred); err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	var fields []tftypes.Value
	if err := cred["credential_fields"].As(&fields); err != nil {
		t.Fatalf("decode credential_fields: %v", err)
	}
	out := map[string]string{}
	for _, f := range fields {
		var fm map[string]tftypes.Value
		if err := f.As(&fm); err != nil {
			t.Fatalf("decode credential field: %v", err)
		}
		var k, v string
		_ = fm["key"].As(&k)
		_ = fm["value"].As(&v)
		out[k] = v
	}
	return out
}

func provBase(credentials tftypes.Value) tftypes.Value {
	return tftypes.NewValue(provTopType, map[string]tftypes.Value{
		"id":          tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"name":        tftypes.NewValue(tftypes.String, "onprem"),
		"credentials": credentials,
	})
}

// Create: the API echoes the credential back with the secret redacted. The
// configured secret must survive into state — otherwise Terraform aborts with
// "provider produced inconsistent result after apply: .credentials".
func TestPreserveOnEmpty_CreateKeepsConfiguredSecret(t *testing.T) {
	var diags diag.Diagnostics
	state := buildStateRaw(provAttrs(),
		provBase(planCredentials([2]string{"token", "TOKEN-abc"}, [2]string{"certificateAuthorityData", "CA-xyz"})),
		redactedResponse("token", "certificateAuthorityData"),
		map[string]string{}, "p1", false, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	got := secretsInState(t, state)
	if got["token"] != "TOKEN-abc" {
		t.Errorf("token = %q, want the configured secret to be kept", got["token"])
	}
	if got["certificateAuthorityData"] != "CA-xyz" {
		t.Errorf("certificateAuthorityData = %q, want the configured secret to be kept", got["certificateAuthorityData"])
	}
}

// Refresh: the redacted read must not blank the secret already in state, which
// would show as perpetual drift on every plan.
func TestPreserveOnEmpty_RefreshKeepsStateSecret(t *testing.T) {
	prior := tftypes.NewValue(credListType, []tftypes.Value{
		tftypes.NewValue(credObjType, map[string]tftypes.Value{
			"name":              tftypes.NewValue(tftypes.String, "default"),
			"credential_fields": tftypes.NewValue(tftypes.List{ElementType: credFieldObjType}, []tftypes.Value{credField("token", "TOKEN-abc")}),
			"metadata":          tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
		}),
	})

	var diags diag.Diagnostics
	state := buildStateRaw(provAttrs(), provBase(prior), redactedResponse("token"),
		map[string]string{}, "p1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	if got := secretsInState(t, state)["token"]; got != "TOKEN-abc" {
		t.Errorf("token = %q, want the prior state secret to be kept", got)
	}
}

// A non-empty response value always wins: preservation only fills in for what
// the API refuses to return, so a value the API does surface still reaches state.
func TestPreserveOnEmpty_NonEmptyResponseWins(t *testing.T) {
	prior := tftypes.NewValue(credListType, []tftypes.Value{
		tftypes.NewValue(credObjType, map[string]tftypes.Value{
			"name":              tftypes.NewValue(tftypes.String, "default"),
			"credential_fields": tftypes.NewValue(tftypes.List{ElementType: credFieldObjType}, []tftypes.Value{credField("token", "OLD")}),
			"metadata":          tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
		}),
	})
	resp := map[string]any{
		"id": "p1", "name": "onprem",
		"credentials": []any{map[string]any{
			"name":   "default",
			"dataEx": []any{map[string]any{"key": "token", "value": "ROTATED", "isSensitive": true}},
		}},
	}

	var diags diag.Diagnostics
	state := buildStateRaw(provAttrs(), provBase(prior), resp, map[string]string{}, "p1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	if got := secretsInState(t, state)["token"]; got != "ROTATED" {
		t.Errorf("token = %q, want the value the API returned", got)
	}
}

// Import (and the data source path): there is no prior value to fall back on,
// so the redacted empty value is stored as-is rather than erroring.
func TestPreserveOnEmpty_NoPriorValueKeepsEmpty(t *testing.T) {
	var diags diag.Diagnostics
	state := buildStateRaw(provAttrs(),
		provBase(tftypes.NewValue(credListType, nil)),
		redactedResponse("token"),
		map[string]string{}, "p1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	if got := secretsInState(t, state)["token"]; got != "" {
		t.Errorf("token = %q, want empty when there is nothing to preserve", got)
	}
}

// Independent of redaction: a configured leaf inside a list(object) must never
// be replaced by a server-normalized value just because a sibling field in the
// same element is still unknown at plan time.
func TestMergeUnknown_ListObjectKeepsConfiguredLeaves(t *testing.T) {
	attrs := provAttrs()
	// Drop the preservation flag so this exercises the merge alone.
	attrs[1].Attributes[1].Attributes[1].PreserveOnEmptyResponse = false

	resp := map[string]any{
		"id": "p1", "name": "onprem",
		"credentials": []any{map[string]any{
			"name":     "default",
			"dataEx":   []any{map[string]any{"key": "token", "value": "SERVER-MANGLED", "isSensitive": true}},
			"metaData": map[string]any{"rotated": "2026-01-01"},
		}},
	}

	var diags diag.Diagnostics
	state := buildStateRaw(attrs, provBase(planCredentials([2]string{"token", "TOKEN-abc"})), resp,
		map[string]string{}, "p1", false, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	if got := secretsInState(t, state)["token"]; got != "TOKEN-abc" {
		t.Errorf("token = %q, want the configured value to survive the merge", got)
	}

	// The unknown sibling is still filled from the response.
	var top map[string]tftypes.Value
	_ = state.As(&top)
	var creds []tftypes.Value
	_ = top["credentials"].As(&creds)
	var cred map[string]tftypes.Value
	_ = creds[0].As(&cred)
	if !cred["metadata"].IsFullyKnown() {
		t.Error("metadata should have been resolved from the response")
	}
	var meta map[string]tftypes.Value
	_ = cred["metadata"].As(&meta)
	var rotated string
	_ = meta["rotated"].As(&rotated)
	if rotated != "2026-01-01" {
		t.Errorf("metadata[rotated] = %q, want the response value", rotated)
	}
}
