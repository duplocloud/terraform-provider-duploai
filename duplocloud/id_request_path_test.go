package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// idRequestPath puts the backend object id in the UPDATE body. The DuploAI admin
// entity endpoints validate a full-document update against the id in the body,
// not the route: Entity.Id self-generates when the body omits it, so a uniqueness
// check that self-excludes by that id excludes nothing and the record collides
// with itself ("ShortName 'AZURE' is already used by workspace 'azure-workspace'"
// naming the very workspace being updated). Create must NOT carry an id — the
// backend assigns it.
func TestUpdateBody_IDRequestPath(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"name":       tftypes.String,
		"short_name": tftypes.String,
	}}
	spec := ResourceSpec{
		Name:          "admin_workspace",
		IDRequestPath: "id",
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
			{Name: "short_name", Type: "string", Optional: true, Computed: true, APIPath: "shortName"},
		},
	}
	r := &dynamicResource{spec: spec}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"name":       tftypes.NewValue(tftypes.String, "azure-workspace"),
		"short_name": tftypes.NewValue(tftypes.String, "azure"),
	})
	const objID = "6a62f7fa433d667a65097b84"

	var diags diag.Diagnostics
	upd := r.updateBodyFromRaw(raw, objID, &diags)
	if diags.HasError() {
		t.Fatalf("update diags: %v", diags)
	}
	if got := upd["id"]; got != objID {
		t.Errorf("update body id = %#v, want %q", got, objID)
	}
	if got := upd["name"]; got != "azure-workspace" {
		t.Errorf("update body name = %#v", got)
	}

	create := r.bodyFromRaw(raw, "create", &diags)
	if diags.HasError() {
		t.Fatalf("create diags: %v", diags)
	}
	if _, present := create["id"]; present {
		t.Errorf("create body must not carry an id, got %#v", create["id"])
	}
}

// A spec that does not set idRequestPath keeps the old body exactly — this flag
// is opt-in and must not leak an id into the other 40-odd resources.
func TestUpdateBody_NoIDRequestPathIsUnchanged(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"name": tftypes.String}}
	spec := ResourceSpec{Attributes: []AttributeSpec{
		{Name: "name", Type: "string", Required: true, APIPath: "name"},
	}}
	r := &dynamicResource{spec: spec}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "thing"),
	})

	var diags diag.Diagnostics
	upd := r.updateBodyFromRaw(raw, "someid", &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if _, present := upd["id"]; present {
		t.Errorf("id must not be injected without idRequestPath, got %#v", upd["id"])
	}
	if len(upd) != 1 {
		t.Errorf("body = %#v, want only the mapped attribute", upd)
	}
}

// The plan and prior-state bodies must stay comparable: apiBodyEqual drives the
// "nothing the API cares about changed" skip in Update, so if the id landed in
// only one of them every apply would issue a pointless PUT.
func TestUpdateBody_IDDoesNotDefeatNoOpSkip(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"name": tftypes.String}}
	spec := ResourceSpec{
		IDRequestPath: "id",
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
		},
	}
	r := &dynamicResource{spec: spec}
	mk := func(name string) tftypes.Value {
		return tftypes.NewValue(objType, map[string]tftypes.Value{
			"name": tftypes.NewValue(tftypes.String, name),
		})
	}

	var diags diag.Diagnostics
	planBody := r.updateBodyFromRaw(mk("same"), "objid-1", &diags)
	stateBody := r.updateBodyFromRaw(mk("same"), "objid-1", &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !apiBodyEqual(planBody, stateBody) {
		t.Errorf("unchanged plan/state bodies must compare equal:\n plan=%#v\nstate=%#v", planBody, stateBody)
	}

	// A real change must still be detected.
	changed := r.updateBodyFromRaw(mk("different"), "objid-1", &diags)
	if apiBodyEqual(planBody, changed) {
		t.Error("a changed attribute must not compare equal")
	}
}
