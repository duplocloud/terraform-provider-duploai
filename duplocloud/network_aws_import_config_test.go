package duplocloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// network_baseline.aws_import_config (spec.awsImportConfig) was added after networks were already in
// production. These tests pin what keeps it backward compatible for existing configurations and state:
//
//   - optional + computed with a keep-null UseStateForUnknown, so a config without the block plans the
//     stored value (null for every pre-existing network, or whatever the UI saved) — never a diff;
//   - no default and no forceNew, so adding the attribute can never plan a replacement;
//   - a config without the block sends the same request body as before (no awsImportConfig key);
//   - none of the new plan-time rules fire for a config that does not set the block.

func TestNetworkAwsImportConfigIsBackwardCompatible(t *testing.T) {
	a := customerManagedSpecs(t)["network_baseline"].attr("aws_import_config")
	if a == nil {
		t.Fatal("network_baseline has no aws_import_config attribute")
	}
	if !a.Optional || !a.Computed || a.Required {
		t.Errorf("optional=%v computed=%v required=%v, want optional + computed", a.Optional, a.Computed, a.Required)
	}
	if a.ForceNew || a.Default != nil {
		t.Errorf("forceNew=%v default=%v, want neither: either would plan a change for existing networks", a.ForceNew, a.Default)
	}
	if a.APIPath != "spec.awsImportConfig" {
		t.Errorf("apiPath=%q, want spec.awsImportConfig", a.APIPath)
	}
	if !useStateForUnknown(*a) || preserveNonNullOnly(*a) {
		t.Errorf("useStateForUnknown=%v preserveNonNullOnly=%v, want a modifier that keeps a null from state",
			useStateForUnknown(*a), preserveNonNullOnly(*a))
	}
	mods := attrSchema(*a).(schema.SingleNestedAttribute).PlanModifiers
	if len(mods) != 1 || reflect.TypeOf(mods[0]) != reflect.TypeOf(objectplanmodifier.UseStateForUnknown()) {
		t.Errorf("plan modifiers = %v, want [UseStateForUnknown]", mods)
	}
	for _, na := range a.Attributes {
		if na.Required || na.ForceNew || na.Default != nil {
			t.Errorf("%s: required=%v forceNew=%v default=%v, want an optional leaf with no default",
				na.Name, na.Required, na.ForceNew, na.Default)
		}
	}
}

// A config without the block — every configuration written before this attribute existed — must not
// put awsImportConfig in the create or update body.
func TestNetworkAwsImportConfigOmittedFromBodyWhenUnset(t *testing.T) {
	spec := customerManagedSpecs(t)["network_baseline"]
	ctx := context.Background()
	r := &dynamicResource{spec: *spec}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)

	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(typ, nil)
	}
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws")
	vals["name"] = tftypes.NewValue(tftypes.String, "net")
	vals["mode"] = tftypes.NewValue(tftypes.String, "Import")
	vals["vpc_id"] = tftypes.NewValue(tftypes.String, "vpc-0a1b2c3d")
	raw := tftypes.NewValue(objType, vals)

	for _, verb := range []string{"create", "update"} {
		var diags diag.Diagnostics
		body := r.bodyFromRaw(raw, verb, &diags)
		if diags.HasError() {
			t.Fatalf("%s: %v", verb, diags)
		}
		spec, _ := body["spec"].(map[string]any)
		if _, present := spec["awsImportConfig"]; present {
			t.Errorf("%s body carries awsImportConfig for a config that does not set it: %v", verb, spec["awsImportConfig"])
		}
		if spec["importVpcId"] != "vpc-0a1b2c3d" {
			t.Errorf("%s body importVpcId = %v, want vpc-0a1b2c3d", verb, spec["importVpcId"])
		}
	}
}

// Configurations that predate aws_import_config must still validate.
func TestNetworkAwsImportConfigRulesIgnoreExistingConfigs(t *testing.T) {
	for _, set := range []map[string]string{
		{},
		{"mode": "Import"},
		{"mode": "Import", "vpc_id": "vpc-0a1b2c3d"},
		{"cloud": "Azure"},
		{"cloud": "Azure", "mode": "Import"},
	} {
		if invalidWhenFires(t, "network_baseline", set) {
			t.Errorf("%v: invalidWhen fired for a config without aws_import_config", set)
		}
	}
}

// The tests above check the flags and the body builder in isolation, which cannot tell whether
// `computed` is set: what it changes is planning. This one drives the real framework plan and apply
// through providerserver, as Terraform does, for a network whose prior state holds an
// aws_import_config (saved via the UI, or returned as {}) while the config omits the block and makes
// an unrelated change. Without `computed` the plan would take the config's null, and the update
// would clear the stored value; with it the stored value is planned and sent back unchanged.
func TestNetworkAwsImportConfigStoredValueSurvivesUnrelatedUpdate(t *testing.T) {
	const typeName = "duploai_network_baseline"
	cases := []struct {
		name   string
		stored map[string]tftypes.Value // nil = attribute null in state
		want   any                      // spec.awsImportConfig in the PUT body; nil = key absent
	}{
		{
			name: "saved via UI",
			stored: map[string]tftypes.Value{
				"private_subnet_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String},
					[]tftypes.Value{tftypes.NewValue(tftypes.String, "subnet-0a1b2c3d")}),
				"public_subnet_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String},
					[]tftypes.Value{tftypes.NewValue(tftypes.String, "subnet-0a1b2c3e")}),
			},
			want: map[string]any{"privateSubnetIds": []any{"subnet-0a1b2c3d"}, "publicSubnetIds": []any{"subnet-0a1b2c3e"}},
		},
		{name: "empty object", stored: map[string]tftypes.Value{}, want: map[string]any{}},
		{name: "never set", stored: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var mu sync.Mutex
			var putSpec map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body map[string]any
				if b, _ := io.ReadAll(req.Body); len(b) > 0 {
					_ = json.Unmarshal(b, &body)
				}
				if req.Method == http.MethodPut {
					mu.Lock()
					putSpec, _ = body["spec"].(map[string]any)
					mu.Unlock()
				}
				mu.Lock()
				spec := putSpec
				mu.Unlock()
				resp, _ := json.Marshal(map[string]any{"data": map[string]any{
					"id": "net1", "name": "net", "status": "Complete", "spec": spec,
					"result": map[string]any{"id": "vpc-0a1b2c3d"},
				}})
				_, _ = w.Write(resp)
			}))
			t.Cleanup(srv.Close)

			server, err := providerserver.NewProtocol6WithError(New())()
			if err != nil {
				t.Fatalf("provider server: %v", err)
			}
			schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
			if err != nil || len(schemas.Diagnostics) > 0 {
				t.Fatalf("GetProviderSchema: %v %v", err, schemas.Diagnostics)
			}
			providerType := schemas.Provider.ValueType().(tftypes.Object)
			rs := schemas.ResourceSchemas[typeName]
			resType := rs.ValueType().(tftypes.Object)

			cfg := protoDynamic(t, providerType, objectWith(providerType, map[string]tftypes.Value{
				"duplo_host":  tftypes.NewValue(tftypes.String, srv.URL),
				"duplo_token": tftypes.NewValue(tftypes.String, "tok"),
			}))
			conf, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &cfg})
			if err != nil || len(conf.Diagnostics) > 0 {
				t.Fatalf("ConfigureProvider: %v %v", err, conf.Diagnostics)
			}

			// The customer's config: an imported network, no aws_import_config block.
			userSet := map[string]tftypes.Value{
				"workspace_id":  tftypes.NewValue(tftypes.String, "ws"),
				"name":          tftypes.NewValue(tftypes.String, "net"),
				"mode":          tftypes.NewValue(tftypes.String, "Import"),
				"region":        tftypes.NewValue(tftypes.String, "us-east-1"),
				"vpc_id":        tftypes.NewValue(tftypes.String, "vpc-0a1b2c3d"),
				"az_count":      tftypes.NewValue(tftypes.Number, 2),
				"subnet_prefix": tftypes.NewValue(tftypes.Number, 24),
				"scope_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String},
					[]tftypes.Value{tftypes.NewValue(tftypes.String, "s")}),
			}
			importType := resType.AttributeTypes["aws_import_config"]
			storedImport := tftypes.NewValue(importType, nil)
			if tc.stored != nil {
				storedImport = objectWith(importType.(tftypes.Object), tc.stored)
			}
			prior := map[string]tftypes.Value{}
			// Real state holds every defaulted attribute (cloud = "Aws", provisioner_type = "Cli", ...).
			for _, a := range customerManagedSpecs(t)["network_baseline"].Attributes {
				if a.Default != nil {
					var d any
					if err := json.Unmarshal(*a.Default, &d); err != nil {
						t.Fatalf("default of %s: %v", a.Name, err)
					}
					prior[a.Name] = goToTftypesValue(resType.AttributeTypes[a.Name], d)
				}
			}
			prior["id"] = tftypes.NewValue(tftypes.String, "ws/net1")
			prior["network_id"] = tftypes.NewValue(tftypes.String, "net1")
			prior["status"] = tftypes.NewValue(tftypes.String, "Complete")
			prior["description"] = tftypes.NewValue(tftypes.String, "before")
			prior["aws_import_config"] = storedImport
			for k, v := range userSet {
				prior[k] = v
			}
			priorVal := objectWith(resType, prior)

			config := map[string]tftypes.Value{"description": tftypes.NewValue(tftypes.String, "after")}
			for k, v := range userSet {
				config[k] = v
			}
			configVal := objectWith(resType, config)

			priorDV := protoDynamic(t, resType, priorVal)
			configDV := protoDynamic(t, resType, configVal)
			proposedDV := protoDynamic(t, resType, proposedNewState(rs, resType, priorVal, configVal))
			plan, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName: typeName, PriorState: &priorDV, ProposedNewState: &proposedDV, Config: &configDV,
			})
			if err != nil || len(plan.Diagnostics) > 0 {
				t.Fatalf("PlanResourceChange: %v %v", err, plan.Diagnostics)
			}
			if len(plan.RequiresReplace) > 0 {
				t.Fatalf("plan requires replacement: %v", plan.RequiresReplace)
			}
			planned := dynamicAttrOf(t, plan.PlannedState, resType, "aws_import_config")
			if !planned.Equal(storedImport) {
				t.Fatalf("planned aws_import_config = %v, want the stored %v", planned, storedImport)
			}

			apply, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
				TypeName: typeName, PriorState: &priorDV, PlannedState: plan.PlannedState, Config: &configDV,
			})
			if err != nil || len(apply.Diagnostics) > 0 {
				t.Fatalf("ApplyResourceChange: %v %v", err, apply.Diagnostics)
			}
			mu.Lock()
			got, present := putSpec["awsImportConfig"]
			mu.Unlock()
			if putSpec == nil {
				t.Fatal("no PUT was sent")
			}
			if tc.want == nil {
				if present {
					t.Errorf("PUT carries awsImportConfig = %v, want the key absent as before", got)
				}
			} else if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("PUT awsImportConfig = %v, want the stored %v", got, tc.want)
			}
		})
	}
}

// objectWith builds a value of t with the given attributes set and every other attribute null.
func objectWith(t tftypes.Object, set map[string]tftypes.Value) tftypes.Value {
	vals := make(map[string]tftypes.Value, len(t.AttributeTypes))
	for name, at := range t.AttributeTypes {
		if v, ok := set[name]; ok {
			vals[name] = v
		} else {
			vals[name] = tftypes.NewValue(at, nil)
		}
	}
	return tftypes.NewValue(t, vals)
}

// proposedNewState mirrors Terraform core for top-level attributes: a computed attribute left null
// in config proposes its prior value; everything else proposes the config value.
func proposedNewState(s *tfprotov6.Schema, t tftypes.Object, prior, config tftypes.Value) tftypes.Value {
	var p, c map[string]tftypes.Value
	_ = prior.As(&p)
	_ = config.As(&c)
	out := make(map[string]tftypes.Value, len(c))
	for k, v := range c {
		out[k] = v
	}
	for _, a := range s.Block.Attributes {
		if a.Computed && c[a.Name].IsNull() {
			out[a.Name] = p[a.Name]
		}
	}
	return tftypes.NewValue(t, out)
}

func protoDynamic(t *testing.T, typ tftypes.Type, v tftypes.Value) tfprotov6.DynamicValue {
	t.Helper()
	dv, err := tfprotov6.NewDynamicValue(typ, v)
	if err != nil {
		t.Fatalf("NewDynamicValue: %v", err)
	}
	return dv
}

func dynamicAttrOf(t *testing.T, dv *tfprotov6.DynamicValue, typ tftypes.Object, name string) tftypes.Value {
	t.Helper()
	v, err := dv.Unmarshal(typ)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var m map[string]tftypes.Value
	if err := v.As(&m); err != nil {
		t.Fatalf("as map: %v", err)
	}
	if _, ok := m[name]; !ok {
		t.Fatalf("no attribute %q in %s", name, strings.Join(keysOf(m), ", "))
	}
	return m[name]
}

func keysOf(m map[string]tftypes.Value) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
