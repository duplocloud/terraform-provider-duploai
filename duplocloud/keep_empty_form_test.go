package duplocloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func str(s string) tftypes.Value          { return tftypes.NewValue(tftypes.String, s) }
func nullOf(t tftypes.Type) tftypes.Value { return tftypes.NewValue(t, nil) }

func TestKeepEmptyForm(t *testing.T) {
	strList := tftypes.List{ElementType: tftypes.String}
	strMap := tftypes.Map{ElementType: tftypes.String}
	emptyList := tftypes.NewValue(strList, []tftypes.Value{})
	emptyMap := tftypes.NewValue(strMap, map[string]tftypes.Value{})

	cases := []struct {
		name        string
		prior, next tftypes.Value
		want        tftypes.Value
	}{
		{"null kept over empty string", nullOf(tftypes.String), str(""), nullOf(tftypes.String)},
		{"empty string kept over null", str(""), nullOf(tftypes.String), str("")},
		{"null kept over empty list", nullOf(strList), emptyList, nullOf(strList)},
		{"null kept over empty map", nullOf(strMap), emptyMap, nullOf(strMap)},
		{"empty list kept over null", emptyList, nullOf(strList), emptyList},
		{"real value replaces null", nullOf(tftypes.String), str("x"), str("x")},
		{"real drift to empty is shown", str("x"), str(""), str("")},
		{"real drift to null is shown", str("x"), nullOf(tftypes.String), nullOf(tftypes.String)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepEmptyForm(tc.prior, tc.next, true); !got.Equal(tc.want) {
				t.Errorf("keepEmptyForm(%v, %v) = %v, want %v", tc.prior, tc.next, got, tc.want)
			}
		})
	}
}

// An object the user left unset, echoed back with only empty children, stays
// null; a configured object keeps the user's null child when the API returns "".
func TestKeepEmptyForm_Objects(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"a": tftypes.String, "b": tftypes.String}}
	allEmpty := tftypes.NewValue(objType, map[string]tftypes.Value{"a": str(""), "b": nullOf(tftypes.String)})
	if got := keepEmptyForm(nullOf(objType), allEmpty, true); !got.IsNull() {
		t.Errorf("unset object echoed as empty should stay null, got %v", got)
	}

	prior := tftypes.NewValue(objType, map[string]tftypes.Value{"a": str("x"), "b": nullOf(tftypes.String)})
	next := tftypes.NewValue(objType, map[string]tftypes.Value{"a": str("x"), "b": str("")})
	if got := keepEmptyForm(prior, next, true); !got.Equal(prior) {
		t.Errorf("nested null child should be kept, got %v want %v", got, prior)
	}
}

// End to end through the Read path: an optional field the user never set, which
// the API returns as "", must stay null in state so the next plan is empty. A
// computed-only output keeps exactly what the API said.
func TestBuildStateRaw_ReadKeepsNullForEmptyResponse(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":          tftypes.String,
		"description": tftypes.String,
		"aliases":     tftypes.List{ElementType: tftypes.String},
		"status":      tftypes.String,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":          str("obj-1"),
		"description": nullOf(tftypes.String),
		"aliases":     nullOf(tftypes.List{ElementType: tftypes.String}),
		"status":      nullOf(tftypes.String),
	})
	attrs := []AttributeSpec{
		{Name: "description", Type: "string", Optional: true, APIPath: "description"},
		{Name: "aliases", Type: "list(string)", Optional: true, APIPath: "aliases"},
		{Name: "status", Type: "string", Computed: true, APIPath: "status"},
	}

	var diags diag.Diagnostics
	out := buildStateRaw(attrs, base,
		map[string]any{"description": "", "aliases": []any{}, "status": ""},
		map[string]string{}, "obj-1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	if !m["description"].IsNull() {
		t.Errorf("description = %v, want null (unset, API echoed \"\")", m["description"])
	}
	if !m["aliases"].IsNull() {
		t.Errorf("aliases = %v, want null (unset, API echoed [])", m["aliases"])
	}
	if !m["status"].Equal(str("")) {
		t.Errorf("status = %v, want \"\" (computed-only keeps the API value)", m["status"])
	}
}

// After an import every value starts null. A computed attribute with a default
// of "" (provisioner_version, forceNew) must take the API's "" rather than keep
// that null: the plan's default is "", and null -> "" on a forceNew attribute
// replaces the imported resource. A user-only attribute still keeps its null.
func TestBuildStateRaw_ImportTakesEmptyValueForComputed(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":                  tftypes.String,
		"provisioner_version": tftypes.String,
		"description":         tftypes.String,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":                  str("ws/obj-1"),
		"provisioner_version": nullOf(tftypes.String),
		"description":         nullOf(tftypes.String),
	})
	attrs := []AttributeSpec{
		{Name: "provisioner_version", Type: "string", Optional: true, Computed: true, ForceNew: true, APIPath: "spec.provisioner.version"},
		{Name: "description", Type: "string", Optional: true, APIPath: "description"},
	}

	var diags diag.Diagnostics
	out := buildStateRaw(attrs, base,
		map[string]any{"spec": map[string]any{"provisioner": map[string]any{"version": ""}}, "description": ""},
		map[string]string{}, "ws/obj-1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	if !m["provisioner_version"].Equal(str("")) {
		t.Errorf("provisioner_version = %v, want \"\" (computed: take the API's empty value)", m["provisioner_version"])
	}
	if !m["description"].IsNull() {
		t.Errorf("description = %v, want null (user-only: keep the user's null)", m["description"])
	}
}

// A computed attribute whose prior is "" keeps it when the API omits the value.
func TestKeepEmptyForm_ComputedKeepsPriorEmpty(t *testing.T) {
	if got := keepEmptyForm(str(""), nullOf(tftypes.String), false); !got.Equal(str("")) {
		t.Errorf("got %v, want \"\"", got)
	}
	if got := keepEmptyForm(nullOf(tftypes.String), str(""), false); !got.Equal(str("")) {
		t.Errorf("got %v, want \"\" (null prior must not win for a computed attribute)", got)
	}
}

// An imported resource has null state for a value the API never echoes back.
// With an empty-string default the plan must keep that null, or null -> "" on a
// forceNew attribute replaces the resource just imported.
func TestNullStateMatchesEmptyDefault(t *testing.T) {
	existing := tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})}
	created := tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, nil)}
	cases := []struct {
		name               string
		state              tfsdk.State
		config, stateV, pl types.String
		want               types.String
	}{
		{"import: null state, unset, default \"\" -> keep null", existing, types.StringNull(), types.StringNull(), types.StringValue(""), types.StringNull()},
		{"create: no state -> default stays", created, types.StringNull(), types.StringNull(), types.StringValue(""), types.StringValue("")},
		{"user set a value -> untouched", existing, types.StringValue("v2"), types.StringNull(), types.StringValue("v2"), types.StringValue("v2")},
		{"real prior value -> untouched", existing, types.StringNull(), types.StringValue("v1"), types.StringValue(""), types.StringValue("")},
		{"non-empty plan -> untouched", existing, types.StringNull(), types.StringNull(), types.StringValue("x"), types.StringValue("x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.StringRequest{State: tc.state, ConfigValue: tc.config, StateValue: tc.stateV, PlanValue: tc.pl}
			resp := &planmodifier.StringResponse{PlanValue: tc.pl}
			nullStateMatchesEmptyDefault{}.PlanModifyString(context.Background(), req, resp)
			if !resp.PlanValue.Equal(tc.want) {
				t.Errorf("plan = %v, want %v", resp.PlanValue, tc.want)
			}
		})
	}

	// Wired before RequiresReplace on a computed, forceNew attribute with default "".
	def := json.RawMessage(`""`)
	a := AttributeSpec{Type: "string", Optional: true, Computed: true, ForceNew: true, Default: &def}
	mods := attrSchema(a).(schema.StringAttribute).PlanModifiers
	var idxNull, idxReplace = -1, -1
	for i, m := range mods {
		switch m.(type) {
		case nullStateMatchesEmptyDefault:
			idxNull = i
		default:
			if strings.Contains(m.Description(context.Background()), "destroy and recreate") || strings.Contains(fmt.Sprintf("%T", m), "requiresReplace") {
				idxReplace = i
			}
		}
	}
	if idxNull < 0 || idxReplace < 0 || idxNull > idxReplace {
		t.Errorf("want nullStateMatchesEmptyDefault before RequiresReplace, got order %v", mods)
	}
}

// importAdoptsConfig: a forceNew value the API never returns is null after
// import. The first plan must adopt the config without replacing; a change
// from a known prior value still replaces.
func TestImportAdoptsConfig(t *testing.T) {
	obj := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	run := func(a AttributeSpec, prior, planned types.String) bool {
		mods := attrSchema(a).(schema.StringAttribute).PlanModifiers
		req := planmodifier.StringRequest{
			State: tfsdk.State{Raw: obj}, Plan: tfsdk.Plan{Raw: obj},
			StateValue: prior, PlanValue: planned, ConfigValue: planned,
		}
		resp := &planmodifier.StringResponse{PlanValue: planned}
		for _, m := range mods {
			m.PlanModifyString(context.Background(), req, resp)
		}
		return resp.RequiresReplace
	}
	adopt := AttributeSpec{Type: "string", Optional: true, ForceNew: true, ImportAdoptsConfig: true}
	plain := AttributeSpec{Type: "string", Optional: true, ForceNew: true}

	// No import mark in private state (a request without one): a null prior
	// means the value was unset at creation, so setting it replaces, as it did
	// before importAdoptsConfig. The imported case is covered by TestJustImported.
	if !run(adopt, types.StringNull(), types.StringValue("ZWNobw==")) {
		t.Error("importAdoptsConfig: null prior without an import mark must replace")
	}
	if !run(adopt, types.StringValue("b2xk"), types.StringValue("ZWNobw==")) {
		t.Error("importAdoptsConfig: a change from a known value must still replace")
	}
	if !run(plain, types.StringNull(), types.StringValue("ZWNobw==")) {
		t.Error("plain forceNew: null prior still replaces (unchanged behavior)")
	}
	for _, typ := range []string{"list(string)", "int", "bool"} {
		ok := AttributeSpec{Name: "x", Type: typ, Optional: true, ForceNew: true, ImportAdoptsConfig: true}
		if _, err := validateAttributes([]AttributeSpec{ok}); err != nil {
			t.Errorf("forceNew %s with importAdoptsConfig should validate, got %v", typ, err)
		}
	}

	// Spec validation: only on forceNew string/int/bool/list attributes.
	for _, bad := range []AttributeSpec{
		{Name: "x", Type: "string", Optional: true, ImportAdoptsConfig: true},
		{Name: "x", Type: "number", Optional: true, ForceNew: true, ImportAdoptsConfig: true},
		{Name: "x", Type: "map(string)", Optional: true, ForceNew: true, ImportAdoptsConfig: true},
	} {
		if _, err := validateAttributes([]AttributeSpec{bad}); err == nil || !strings.Contains(err.Error(), "importAdoptsConfig") {
			t.Errorf("validate(%+v) = %v, want an importAdoptsConfig error", bad, err)
		}
	}
}

// importAdoptsConfig on int and bool (msk_kafka.configuration_revision,
// native_host.ebs_optimized): without an import mark a null prior still
// replaces, and a known prior that changes always replaces.
func TestImportAdoptsConfigIntBool(t *testing.T) {
	obj := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	runInt := func(prior, planned types.Int64) bool {
		a := AttributeSpec{Type: "int", Optional: true, ForceNew: true, ImportAdoptsConfig: true}
		req := planmodifier.Int64Request{State: tfsdk.State{Raw: obj}, Plan: tfsdk.Plan{Raw: obj}, StateValue: prior, PlanValue: planned, ConfigValue: planned}
		resp := &planmodifier.Int64Response{PlanValue: planned}
		for _, m := range attrSchema(a).(schema.Int64Attribute).PlanModifiers {
			m.PlanModifyInt64(context.Background(), req, resp)
		}
		return resp.RequiresReplace
	}
	runBool := func(prior, planned types.Bool) bool {
		a := AttributeSpec{Type: "bool", Optional: true, Computed: true, ForceNew: true, ImportAdoptsConfig: true}
		req := planmodifier.BoolRequest{State: tfsdk.State{Raw: obj}, Plan: tfsdk.Plan{Raw: obj}, StateValue: prior, PlanValue: planned, ConfigValue: planned}
		resp := &planmodifier.BoolResponse{PlanValue: planned}
		for _, m := range attrSchema(a).(schema.BoolAttribute).PlanModifiers {
			m.PlanModifyBool(context.Background(), req, resp)
		}
		return resp.RequiresReplace
	}
	if !runInt(types.Int64Null(), types.Int64Value(3)) {
		t.Error("int: null prior without an import mark must replace")
	}
	if !runInt(types.Int64Value(2), types.Int64Value(3)) {
		t.Error("int: a change from a known value must still replace")
	}
	if !runBool(types.BoolNull(), types.BoolValue(true)) {
		t.Error("bool: null prior without an import mark must replace")
	}
	if !runBool(types.BoolValue(false), types.BoolValue(true)) {
		t.Error("bool: a change from a known value must still replace")
	}
}

// updateOnlyIfChanged: deleting an unchanged field prunes the parent objects it
// leaves empty (EFS: no spec.updateRequest -> no UpdateFileSystem call), but
// keeps a parent that still holds a changed sibling.
func TestDeletePathPruning(t *testing.T) {
	body := map[string]any{
		"description": "d",
		"spec": map[string]any{
			"backup":        true,
			"updateRequest": map[string]any{"throughputMode": "bursting"},
		},
	}
	deletePathPruning(body, []string{"spec", "updateRequest", "throughputMode"})
	spec := body["spec"].(map[string]any)
	if _, ok := spec["updateRequest"]; ok {
		t.Errorf("empty spec.updateRequest should be pruned, got %v", spec)
	}
	if spec["backup"] != true {
		t.Errorf("sibling spec.backup must stay, got %v", spec)
	}

	body2 := map[string]any{"spec": map[string]any{"updateRequest": map[string]any{
		"throughputMode": "provisioned", "provisionedThroughputInMibps": 50,
	}}}
	deletePathPruning(body2, []string{"spec", "updateRequest", "throughputMode"})
	ur := body2["spec"].(map[string]any)["updateRequest"].(map[string]any)
	if _, ok := ur["throughputMode"]; ok || ur["provisionedThroughputInMibps"] != 50 {
		t.Errorf("only throughputMode should be removed, got %v", ur)
	}

	deletePathPruning(body2, []string{"spec", "missing", "x"}) // absent path: no-op, no panic
}

// appliesWhen: an AKS-only output on an AWS cluster is nulled (plan and state),
// an applicable one is kept, and an unknown condition value never nulls.
func TestNullInapplicable(t *testing.T) {
	cloudDefault := json.RawMessage(`"Aws"`)
	attrs := []AttributeSpec{
		{Name: "cloud", Type: "string", Optional: true, Computed: true, Default: &cloudDefault},
		{Name: "azure_fqdn", Type: "string", Computed: true, AppliesWhen: []RequiredIfCondition{{Attribute: "cloud", Equals: "Azure"}}},
		{Name: "cluster_arn", Type: "string", Computed: true, AppliesWhen: []RequiredIfCondition{{Attribute: "cloud", Equals: "Aws"}}},
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"cloud": tftypes.String, "azure_fqdn": tftypes.String, "cluster_arn": tftypes.String,
	}}
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	get := func(v tftypes.Value, k string) tftypes.Value {
		var m map[string]tftypes.Value
		_ = v.As(&m)
		return m[k]
	}

	aws := tftypes.NewValue(objType, map[string]tftypes.Value{"cloud": str("Aws"), "azure_fqdn": unknown, "cluster_arn": unknown})
	out := nullInapplicable(attrs, aws)
	if !get(out, "azure_fqdn").IsNull() {
		t.Errorf("azure_fqdn on an AWS cluster should be null, got %v", get(out, "azure_fqdn"))
	}
	if get(out, "cluster_arn").IsKnown() {
		t.Errorf("cluster_arn applies on AWS and must stay as planned (unknown), got %v", get(out, "cluster_arn"))
	}

	// cloud unset -> default "Aws" stands in.
	def := tftypes.NewValue(objType, map[string]tftypes.Value{"cloud": nullOf(tftypes.String), "azure_fqdn": unknown, "cluster_arn": unknown})
	if !get(nullInapplicable(attrs, def), "azure_fqdn").IsNull() {
		t.Error("with cloud defaulted to Aws, azure_fqdn should be null")
	}

	// cloud unknown -> nothing is nulled.
	unk := tftypes.NewValue(objType, map[string]tftypes.Value{"cloud": unknown, "azure_fqdn": unknown, "cluster_arn": unknown})
	if !get(nullInapplicable(attrs, unk), "azure_fqdn").Equal(unknown) {
		t.Error("an unknown condition value must not null the attribute")
	}

	// Validation: only computed-only attributes.
	bad := AttributeSpec{Name: "x", Type: "string", Optional: true, Computed: true, AppliesWhen: []RequiredIfCondition{{Attribute: "cloud", Equals: "Aws"}}}
	if _, err := validateAttributes([]AttributeSpec{bad}); err == nil || !strings.Contains(err.Error(), "appliesWhen") {
		t.Errorf("appliesWhen on an optional attribute should fail validation, got %v", err)
	}
}

// caseInsensitive: a case-only change to a forceNew ARM id never replaces; a
// real change still does; a read that re-cases the id keeps the stored value.
func TestCaseInsensitive(t *testing.T) {
	obj := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	a := AttributeSpec{Type: "string", Required: true, ForceNew: true, CaseInsensitive: true}
	replaces := func(prior, planned string) bool {
		req := planmodifier.StringRequest{
			State: tfsdk.State{Raw: obj}, Plan: tfsdk.Plan{Raw: obj},
			StateValue: types.StringValue(prior), PlanValue: types.StringValue(planned), ConfigValue: types.StringValue(planned),
		}
		resp := &planmodifier.StringResponse{PlanValue: types.StringValue(planned)}
		for _, m := range attrSchema(a).(schema.StringAttribute).PlanModifiers {
			m.PlanModifyString(context.Background(), req, resp)
		}
		return resp.RequiresReplace
	}
	lower := "/subscriptions/s/resourcegroups/rg/providers/Microsoft.Storage/storageAccounts/sa"
	canon := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa"
	if replaces(lower, canon) {
		t.Error("a case-only change must not replace")
	}
	if !replaces(lower, "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/other") {
		t.Error("a real change must still replace")
	}

	if got := keepPriorCasing(str(canon), str(lower)); !got.Equal(str(canon)) {
		t.Errorf("read with other casing should keep the stored value, got %v", got)
	}
	if got := keepPriorCasing(str(canon), str("/other")); !got.Equal(str("/other")) {
		t.Errorf("a real change on read must show, got %v", got)
	}

	if _, err := validateAttributes([]AttributeSpec{{Name: "x", Type: "int", Optional: true, CaseInsensitive: true}}); err == nil || !strings.Contains(err.Error(), "caseInsensitive") {
		t.Errorf("caseInsensitive on an int should fail validation, got %v", err)
	}
}

// Kubernetes labels with filterResponseKeys ["*"]: the platform stamps
// duplocloud.ai/* labels and the resource group's tags (arbitrary keys) onto the
// object. Only labels the user declared are tracked; a changed value on one of
// them still shows as drift.
func TestFilterAllUndeclaredLabels(t *testing.T) {
	mapT := tftypes.Map{ElementType: tftypes.String}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"id": tftypes.String, "labels": mapT}}
	attrs := []AttributeSpec{{
		Name: "labels", Type: "map(string)", Optional: true, Computed: true,
		ResponsePath: "result.k8sResource.metadata.labels", FilterResponseKeys: []string{"*"},
	}}
	read := func(prior map[string]tftypes.Value, server map[string]any) map[string]tftypes.Value {
		base := tftypes.NewValue(objType, map[string]tftypes.Value{"id": str("ws/x"), "labels": tftypes.NewValue(mapT, prior)})
		var diags diag.Diagnostics
		out := buildStateRaw(attrs, base, map[string]any{"result": map[string]any{"k8sResource": map[string]any{"metadata": map[string]any{"labels": server}}}},
			map[string]string{}, "ws/x", true, true, &diags)
		if diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		var m map[string]tftypes.Value
		_ = out.As(&m)
		var labels map[string]tftypes.Value
		_ = m["labels"].As(&labels)
		return labels
	}
	server := map[string]any{"app": "web", "duplocloud.ai/workspace": "ws", "duplocloud.ai/version": "abc", "team": "platform-rg-tag"}

	got := read(map[string]tftypes.Value{"app": str("web")}, server)
	if len(got) != 1 || !got["app"].Equal(str("web")) {
		t.Errorf("only the declared label should be kept, got %v", got)
	}

	server["app"] = "changed-outside"
	if got := read(map[string]tftypes.Value{"app": str("web")}, server); !got["app"].Equal(str("changed-outside")) {
		t.Errorf("drift on a declared label must still show, got %v", got)
	}
}

// TestAppliesWhenUnknownAttribute: a condition naming an attribute the spec
// doesn't declare must fail at load. rawConditionsHold skips such a condition,
// so a typo would otherwise silently disable the rule.
func TestAppliesWhenUnknownAttribute(t *testing.T) {
	cases := []struct{ name, cond, wantErr string }{
		{"declared attribute loads", `"cloud"`, ""},
		{"unknown attribute is rejected", `"clould"`, `appliesWhen references unknown attribute "clould"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"name":"probe","description":"probe","idPath":"id",
			  "endpoint":{"uriBase":"/v3/admin/things"},"attributes":[
			  {"name":"cloud","type":"string","optional":true,"apiPath":"cloud"},
			  {"name":"azure_fqdn","type":"string","computed":true,"apiPath":"fqdn",
			   "appliesWhen":[{"attribute":` + tc.cond + `,"equals":"Azure"}]}]}`)
			var spec ResourceSpec
			if err := json.Unmarshal(raw, &spec); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := spec.validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("expected the spec to load, got %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// A stable output is planned as its prior value, so a write response that
// leaves it out must not null it (inconsistent result after apply). A read
// still reflects the API, and a non-stable output still takes the response.
func TestBuildStateRaw_StableKeptWhenWriteResponseOmitsIt(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":         tftypes.String,
		"created_at": tftypes.String,
		"status":     tftypes.String,
	}}
	base := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":         str("obj-1"),
		"created_at": str("2026-10-14T10:00:00Z"),
		"status":     str("Complete"),
	})
	attrs := []AttributeSpec{
		{Name: "created_at", Type: "string", Computed: true, Stable: true, APIPath: "createdAt"},
		{Name: "status", Type: "string", Computed: true, APIPath: "status"},
	}
	build := func(refresh bool) map[string]tftypes.Value {
		var diags diag.Diagnostics
		out := buildStateRaw(attrs, base, map[string]any{"status": "Updating"},
			map[string]string{}, "obj-1", refresh, true, &diags)
		if diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		m := map[string]tftypes.Value{}
		if err := out.As(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	w := build(false)
	if !w["created_at"].Equal(str("2026-10-14T10:00:00Z")) {
		t.Errorf("write: stable created_at missing from the response must keep the planned value, got %v", w["created_at"])
	}
	if !w["status"].Equal(str("Updating")) {
		t.Errorf("write: non-stable status must take the response, got %v", w["status"])
	}
	if r := build(true); !r["created_at"].IsNull() {
		t.Errorf("read: a stable output the API omits must read as null, got %v", r["created_at"])
	}
}

type fakePrivate map[string][]byte

func (f fakePrivate) GetKey(_ context.Context, key string) ([]byte, diag.Diagnostics) {
	return f[key], nil
}

// TestJustImported: only the mark ImportState writes counts as "imported".
func TestJustImported(t *testing.T) {
	ctx := context.Background()
	if !justImported(ctx, fakePrivate{importedPrivateKey: []byte("true")}) {
		t.Error("the import mark must read as imported")
	}
	if justImported(ctx, fakePrivate{}) {
		t.Error("no mark must read as not imported")
	}
	if justImported(ctx, fakePrivate{importedPrivateKey: nil}) {
		t.Error("a cleared mark must read as not imported")
	}
	if justImported(ctx, nil) {
		t.Error("no private state must read as not imported")
	}
}
