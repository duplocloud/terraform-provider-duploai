package duplocloud

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// oneOf on a string collection constrains every element. It used to be wired
// only for a plain string, so on list(string) it was accepted and silently
// ignored — admin_user.roles took any value, including "user", which the
// backend's case-sensitive role checks then never matched.

var roleAttr = AttributeSpec{
	Name: "roles", Type: "list(string)", Optional: true, Computed: true,
	APIPath: "roles", OneOf: []string{"Administrator", "User"}, MaxItems: 1,
}

func stringValues(vs ...string) []attr.Value {
	out := make([]attr.Value, len(vs))
	for i, v := range vs {
		out[i] = types.StringValue(v)
	}
	return out
}

func TestCollectionOneOf_ListChecksEveryElement(t *testing.T) {
	sa, ok := attrSchema(roleAttr).(schema.ListAttribute)
	if !ok {
		t.Fatalf("roles schema is %T, want schema.ListAttribute", attrSchema(roleAttr))
	}
	cases := []struct {
		name    string
		values  []string
		wantErr bool
	}{
		{"Administrator", []string{"Administrator"}, false},
		{"User", []string{"User"}, false},
		{"both is rejected (one role per user, as in the console)", []string{"Administrator", "User"}, true},
		{"empty list", []string{}, false},
		{"lowercase is rejected (backend compares case-sensitively)", []string{"user"}, true},
		{"unknown role", []string{"SuperUser"}, true},
		{"one bad element in an otherwise valid list", []string{"User", "Admin"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := types.ListValueMust(types.StringType, stringValues(tc.values...))
			var errs int
			for _, v := range sa.Validators {
				resp := &validator.ListResponse{}
				v.ValidateList(context.Background(), validator.ListRequest{ConfigValue: list}, resp)
				errs += resp.Diagnostics.ErrorsCount()
			}
			if got := errs > 0; got != tc.wantErr {
				t.Errorf("values %q: error = %v, want %v", tc.values, got, tc.wantErr)
			}
		})
	}
}

func TestCollectionOneOf_SetAndMap(t *testing.T) {
	setAttr := roleAttr
	setAttr.Type = "set(string)"
	ss, ok := attrSchema(setAttr).(schema.SetAttribute)
	if !ok {
		t.Fatalf("set schema is %T", attrSchema(setAttr))
	}
	bad := types.SetValueMust(types.StringType, stringValues("User", "nope"))
	resp := &validator.SetResponse{}
	for _, v := range ss.Validators {
		v.ValidateSet(context.Background(), validator.SetRequest{ConfigValue: bad}, resp)
	}
	if !resp.Diagnostics.HasError() {
		t.Error("set(string) with oneOf accepted an element outside the set")
	}

	mapAttr := roleAttr
	mapAttr.Type = "map(string)"
	ms, ok := attrSchema(mapAttr).(schema.MapAttribute)
	if !ok {
		t.Fatalf("map schema is %T", attrSchema(mapAttr))
	}
	badMap := types.MapValueMust(types.StringType, map[string]attr.Value{"primary": types.StringValue("nope")})
	mresp := &validator.MapResponse{}
	for _, v := range ms.Validators {
		v.ValidateMap(context.Background(), validator.MapRequest{ConfigValue: badMap}, mresp)
	}
	if !mresp.Diagnostics.HasError() {
		t.Error("map(string) with oneOf accepted a value outside the set")
	}
}

// A non-string collection gets no element validators, so nothing changes for it.
func TestCollectionOneOf_NoValidatorsWithoutConstraints(t *testing.T) {
	plain := AttributeSpec{Name: "ids", Type: "list(string)", Optional: true, APIPath: "ids"}
	if sa := attrSchema(plain).(schema.ListAttribute); len(sa.Validators) != 0 {
		t.Errorf("unconstrained list(string) got %d validators, want 0", len(sa.Validators))
	}
}

// oneOf and the string constraints load on a string collection, and are
// rejected on a type that cannot use them rather than being silently ignored.
func TestCollectionOneOf_SpecValidation(t *testing.T) {
	cases := []struct {
		name    string
		attr    string
		wantErr string
	}{
		{"oneOf on list(string) loads", `{"name":"roles","type":"list(string)","optional":true,"apiPath":"roles","oneOf":["A","B"]}`, ""},
		{"pattern on set(string) loads", `{"name":"tags","type":"set(string)","optional":true,"apiPath":"tags","pattern":"^[a-z]+$"}`, ""},
		{"oneOf on int is rejected", `{"name":"count","type":"int","optional":true,"apiPath":"count","oneOf":["1","2"]}`, "oneOf is only valid on a string"},
		{"oneOf on list(int) is rejected", `{"name":"ports","type":"list(int)","optional":true,"apiPath":"ports","oneOf":["80"]}`, "oneOf is only valid on a string"},
		{"pattern on list(int) is rejected", `{"name":"ports","type":"list(int)","optional":true,"apiPath":"ports","pattern":"^8"}`, "only valid on a string"},
		{"maxItems on list loads", `{"name":"roles","type":"list(string)","optional":true,"apiPath":"roles","maxItems":1}`, ""},
		{"maxItems on string is rejected", `{"name":"role","type":"string","optional":true,"apiPath":"role","maxItems":1}`, "maxItems is only valid on a list or set"},
		{"negative maxItems is rejected", `{"name":"roles","type":"list(string)","optional":true,"apiPath":"roles","maxItems":-1}`, "maxItems must be positive"},
		{"minItems above maxItems is rejected", `{"name":"roles","type":"list(string)","optional":true,"apiPath":"roles","minItems":2,"maxItems":1}`, "exceeds maxItems"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"name":"probe","description":"probe","idPath":"id",
			  "endpoint":{"uriBase":"/v3/admin/things"},"attributes":[` + tc.attr + `]}`)
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
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// The shipped admin_user spec restricts roles to one of the backend's two role names.
func TestAdminUserRolesOneOf(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, spec := range specs {
		if spec.Name != "admin_user" {
			continue
		}
		for _, a := range spec.Attributes {
			if a.Name != "roles" {
				continue
			}
			want := []string{"Administrator", "User"}
			if strings.Join(a.OneOf, ",") != strings.Join(want, ",") {
				t.Errorf("admin_user.roles oneOf = %v, want %v (backend UserRoles constants)", a.OneOf, want)
			}
			if a.MaxItems != 1 {
				t.Errorf("admin_user.roles maxItems = %d, want 1 (a user has one role; the console's role picker is single-select)", a.MaxItems)
			}
			return
		}
		t.Fatal("admin_user spec has no roles attribute")
	}
	t.Fatal("admin_user spec not found")
}

// The data source schema carries the same per-element constraints as the resource.
func TestCollectionOneOf_DataSourceSchema(t *testing.T) {
	ds, ok := dsPrimitiveCollectionSchema(roleAttr, typeInfo{coll: "list", elem: "string"}).(dsschema.ListAttribute)
	if !ok {
		t.Fatal("data source roles schema is not a dsschema.ListAttribute")
	}
	if len(ds.Validators) != 1 {
		t.Errorf("data source list(string) with oneOf has %d validators, want 1", len(ds.Validators))
	}
}
