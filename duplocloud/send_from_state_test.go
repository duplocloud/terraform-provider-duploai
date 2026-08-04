package duplocloud

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// sendFromState exists for a backend that rebuilds its stored document from the request
// body: a server-assigned field the body omits is dropped. Azure Managed Redis is such
// an API — a computed-only scope_ids was silently wiped from the record by any update,
// losing the resource's link to its cloud provider account.
func sendFromStateSpec(flag bool) ResourceSpec {
	return ResourceSpec{
		Name:   "widget",
		IDPath: "id",
		Endpoint: EndpointSpec{
			UriBase: "/widgets",
		},
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
			// The field under test: server-assigned, so computed-only.
			{Name: "scope_ids", Type: "list(string)", Computed: true, SendFromState: flag,
				APIPath: "spec.scopeIds"},
			// A normal computed-only output must stay out of the body either way.
			{Name: "status", Type: "string", Computed: true, APIPath: "status"},
		},
	}
}

var sfsObjType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
	"id":        tftypes.String,
	"name":      tftypes.String,
	"scope_ids": tftypes.List{ElementType: tftypes.String},
	"status":    tftypes.String,
}}

func sfsRaw() tftypes.Value {
	return tftypes.NewValue(sfsObjType, map[string]tftypes.Value{
		"id":   tftypes.NewValue(tftypes.String, "w-1"),
		"name": tftypes.NewValue(tftypes.String, "widget-1"),
		"scope_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "scope-9"),
		}),
		"status": tftypes.NewValue(tftypes.String, "Complete"),
	})
}

func sfsBody(t *testing.T, flag bool, verb string) map[string]any {
	t.Helper()
	spec := sendFromStateSpec(flag)
	if _, err := validateAttributes(spec.Attributes); err != nil {
		t.Fatalf("spec must be valid: %v", err)
	}
	r := &dynamicResource{spec: spec}
	var diags diag.Diagnostics
	body := r.bodyFromRaw(sfsRaw(), verb, &diags)
	if diags.HasError() {
		t.Fatalf("bodyFromRaw diags: %v", diags)
	}
	return body
}

// Without the flag a computed-only attribute is omitted — this is the behaviour that
// caused the data loss, and it is still correct for ordinary outputs.
func TestSendFromState_OmittedWithoutTheFlag(t *testing.T) {
	body := sfsBody(t, false, "update")
	j, _ := json.Marshal(body)
	if strings.Contains(string(j), "scopeIds") {
		t.Errorf("computed-only attribute must not be sent without sendFromState: %s", j)
	}
}

// With the flag the value Terraform holds in state is sent, so the backend's rebuild
// keeps it.
func TestSendFromState_SentWithTheFlag(t *testing.T) {
	for _, verb := range []string{"update", "create"} {
		body := sfsBody(t, true, verb)
		spec, _ := body["spec"].(map[string]any)
		got, _ := json.Marshal(spec["scopeIds"])
		if string(got) != `["scope-9"]` {
			t.Errorf("%s body: spec.scopeIds = %s, want [\"scope-9\"]", verb, got)
		}
	}
}

// A plain computed-only output must never be swept in by the same change.
func TestSendFromState_OtherComputedOutputsStillOmitted(t *testing.T) {
	body := sfsBody(t, true, "update")
	if _, present := body["status"]; present {
		j, _ := json.Marshal(body)
		t.Errorf("an unflagged computed-only attribute must stay out of the body: %s", j)
	}
}

// The value has to be known at plan time to be sendable, so the flag implies
// UseStateForUnknown. Without that the plan value is unknown and the body builder skips
// it — which is exactly how an earlier attempt at this failed.
func TestSendFromState_ImpliesUseStateForUnknown(t *testing.T) {
	a := AttributeSpec{Name: "scope_ids", Type: "list(string)", Computed: true, SendFromState: true}
	if !useStateForUnknown(a) {
		t.Error("sendFromState must imply UseStateForUnknown, or the value is unknown at plan time")
	}
	b := AttributeSpec{Name: "status", Type: "string", Computed: true}
	if useStateForUnknown(b) {
		t.Error("a plain computed-only attribute should not keep its prior value")
	}
}

// A list(object) has to reach the collection's plan modifier for the flag to work —
// this is the shape of azure_managed_redis's reported_modules, which the API compares
// against the requested module list inside the same request body.
func TestSendFromState_ListOfObject(t *testing.T) {
	modType := tftypes.List{ElementType: tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"name": tftypes.String,
		"args": tftypes.String,
	}}}
	a := AttributeSpec{
		Name: "reported_modules", Type: "list(object)", Computed: true, SendFromState: true,
		UpdatePath: "result.modules", ResponsePath: "result.modules",
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Computed: true, APIPath: "name"},
			{Name: "args", Type: "string", Computed: true, APIPath: "args"},
		},
	}
	if !useStateForUnknown(a) {
		t.Fatal("a list(object) with sendFromState must keep its prior value, or it is unknown at plan time")
	}
	spec := ResourceSpec{Name: "widget", IDPath: "id", Endpoint: EndpointSpec{UriBase: "/widgets"},
		Attributes: []AttributeSpec{a}}
	if _, err := validateAttributes(spec.Attributes); err != nil {
		t.Fatalf("spec must be valid: %v", err)
	}
	raw := tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{"reported_modules": modType}},
		map[string]tftypes.Value{
			"reported_modules": tftypes.NewValue(modType, []tftypes.Value{
				tftypes.NewValue(modType.ElementType, map[string]tftypes.Value{
					"name": tftypes.NewValue(tftypes.String, "RediSearch"),
					"args": tftypes.NewValue(tftypes.String, ""),
				}),
			}),
		})
	r := &dynamicResource{spec: spec}
	var diags diag.Diagnostics
	body := r.bodyFromRaw(raw, "update", &diags)
	if diags.HasError() {
		t.Fatalf("bodyFromRaw diags: %v", diags)
	}
	got, _ := json.Marshal(nested(body, "result", "modules"))
	if string(got) != `[{"args":"","name":"RediSearch"}]` {
		t.Errorf("result.modules = %s, want [{\"args\":\"\",\"name\":\"RediSearch\"}]", got)
	}
}

// ResendCreatePathsOnUpdate is the sibling problem: an API whose update body is a
// delta envelope, while the stored record is replaced by the body's create-shape
// fields. Send only the envelope and every field the envelope does not treat as
// changed falls back to a type default — Azure Managed Redis reset a non-default
// eviction_policy to NoEviction on any unrelated update.
func resendSpec(flag bool) ResourceSpec {
	return ResourceSpec{
		Name:                      "widget",
		IDPath:                    "id",
		Endpoint:                  EndpointSpec{UriBase: "/widgets"},
		ResendCreatePathsOnUpdate: flag,
		Attributes: []AttributeSpec{
			// Distinct create/update paths: the case the flag exists for.
			{Name: "eviction_policy", Type: "string", Optional: true, Computed: true,
				CreatePath:   "spec.database.evictionPolicy",
				UpdatePath:   "spec.updateRequest.evictionPolicy",
				ResponsePath: "spec.database.evictionPolicy"},
			// One path for both verbs: nothing to duplicate.
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
			// createOnly opts out — the API refuses it on update.
			{Name: "seed", Type: "string", Optional: true, CreateOnly: true, APIPath: "spec.seed"},
		},
	}
}

func resendBody(t *testing.T, flag bool, verb string) map[string]any {
	t.Helper()
	spec := resendSpec(flag)
	if _, err := validateAttributes(spec.Attributes); err != nil {
		t.Fatalf("spec must be valid: %v", err)
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"eviction_policy": tftypes.String,
		"name":            tftypes.String,
		"seed":            tftypes.String,
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"eviction_policy": tftypes.NewValue(tftypes.String, "AllKeysLRU"),
		"name":            tftypes.NewValue(tftypes.String, "widget-1"),
		"seed":            tftypes.NewValue(tftypes.String, "s1"),
	})
	r := &dynamicResource{spec: spec}
	var diags diag.Diagnostics
	body := r.bodyFromRaw(raw, verb, &diags)
	if diags.HasError() {
		t.Fatalf("bodyFromRaw diags: %v", diags)
	}
	return body
}

func nested(body map[string]any, path ...string) any {
	var cur any = body
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

// With the flag the update body carries BOTH shapes, so the rebuilt record keeps the
// value and the envelope still drives the cloud-side patch.
func TestResendCreatePaths_UpdateSendsBothShapes(t *testing.T) {
	body := resendBody(t, true, "update")
	if got := nested(body, "spec", "updateRequest", "evictionPolicy"); got != "AllKeysLRU" {
		t.Errorf("spec.updateRequest.evictionPolicy = %v, want AllKeysLRU", got)
	}
	if got := nested(body, "spec", "database", "evictionPolicy"); got != "AllKeysLRU" {
		t.Errorf("spec.database.evictionPolicy = %v, want AllKeysLRU (create shape resent)", got)
	}
}

// Without it, only the envelope is sent — the behaviour that lost the value.
func TestResendCreatePaths_OffKeepsUpdateShapeOnly(t *testing.T) {
	body := resendBody(t, false, "update")
	if got := nested(body, "spec", "updateRequest", "evictionPolicy"); got != "AllKeysLRU" {
		t.Errorf("spec.updateRequest.evictionPolicy = %v, want AllKeysLRU", got)
	}
	if got := nested(body, "spec", "database", "evictionPolicy"); got != nil {
		t.Errorf("spec.database.evictionPolicy = %v, want absent without the flag", got)
	}
}

// The flag must not change the create body, and must not resurrect a createOnly field
// on update.
func TestResendCreatePaths_ScopeLimits(t *testing.T) {
	create := resendBody(t, true, "create")
	if got := nested(create, "spec", "updateRequest"); got != nil {
		t.Errorf("create body must not carry the update envelope, got %v", got)
	}
	if got := nested(create, "spec", "database", "evictionPolicy"); got != "AllKeysLRU" {
		t.Errorf("create body: spec.database.evictionPolicy = %v, want AllKeysLRU", got)
	}
	if got := nested(create, "spec", "seed"); got != "s1" {
		t.Errorf("create body: spec.seed = %v, want s1", got)
	}

	update := resendBody(t, true, "update")
	if got := nested(update, "spec", "seed"); got != nil {
		t.Errorf("createOnly field must stay out of the update body, got %v", got)
	}
	if got := update["name"]; got != "widget-1" {
		t.Errorf("single-path attribute = %v, want widget-1 written once", got)
	}
}

// updateBoolTrueValue rewrites what the UPDATE path carries. The create path must
// still receive the enum string, not the bool the envelope gets.
func TestResendCreatePaths_KeepsUntransformedValueOnCreatePath(t *testing.T) {
	spec := ResourceSpec{
		Name: "widget", IDPath: "id", Endpoint: EndpointSpec{UriBase: "/widgets"},
		ResendCreatePathsOnUpdate: true,
		Attributes: []AttributeSpec{
			{Name: "image_tag_mutability", Type: "string", Optional: true, Computed: true,
				CreatePath:          "spec.imageTagMutability",
				UpdatePath:          "spec.updateRequest.enableTagImmutability",
				UpdateBoolTrueValue: "IMMUTABLE"},
		},
	}
	if _, err := validateAttributes(spec.Attributes); err != nil {
		t.Fatalf("spec must be valid: %v", err)
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"image_tag_mutability": tftypes.String}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"image_tag_mutability": tftypes.NewValue(tftypes.String, "IMMUTABLE"),
	})
	r := &dynamicResource{spec: spec}
	var diags diag.Diagnostics
	body := r.bodyFromRaw(raw, "update", &diags)
	if diags.HasError() {
		t.Fatalf("bodyFromRaw diags: %v", diags)
	}
	if got := nested(body, "spec", "updateRequest", "enableTagImmutability"); got != true {
		t.Errorf("update path = %#v, want bool true", got)
	}
	if got := nested(body, "spec", "imageTagMutability"); got != "IMMUTABLE" {
		t.Errorf("create path = %#v, want the string \"IMMUTABLE\"", got)
	}
}

func TestSendFromState_Validation(t *testing.T) {
	tests := []struct {
		name    string
		attr    AttributeSpec
		wantErr string
	}{
		{"valid", AttributeSpec{Name: "x", Type: "string", Computed: true, SendFromState: true}, ""},
		{
			"not computed",
			AttributeSpec{Name: "x", Type: "string", Required: true, SendFromState: true},
			"only valid on a computed attribute",
		},
		{
			"contradicts noSend",
			AttributeSpec{Name: "x", Type: "string", Computed: true, SendFromState: true, NoSend: true},
			"contradictory",
		},
		{
			"redundant on optional",
			AttributeSpec{Name: "x", Type: "string", Optional: true, Computed: true, SendFromState: true},
			"redundant",
		},
		{
			"contradicts createOnly",
			AttributeSpec{Name: "x", Type: "string", Computed: true, SendFromState: true, CreateOnly: true},
			"never be sent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateAttributes([]AttributeSpec{tt.attr})
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("want error containing %q, got nil", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}
