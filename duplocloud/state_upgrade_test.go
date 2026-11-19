package duplocloud

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// sampleJSON returns a fully populated value of typ as plain JSON-ready Go data
// (every attribute set, every collection non-empty), so the round trip below
// exercises every attribute of every schema, nested ones included.
func sampleJSON(typ tftypes.Type) any {
	switch {
	case typ.Is(tftypes.String):
		return "x"
	case typ.Is(tftypes.Number):
		return 7
	case typ.Is(tftypes.Bool):
		return true
	case typ.Is(tftypes.List{}):
		return []any{sampleJSON(typ.(tftypes.List).ElementType)}
	case typ.Is(tftypes.Set{}):
		return []any{sampleJSON(typ.(tftypes.Set).ElementType)}
	case typ.Is(tftypes.Map{}):
		return map[string]any{"k": sampleJSON(typ.(tftypes.Map).ElementType)}
	case typ.Is(tftypes.Object{}):
		out := map[string]any{}
		for n, at := range typ.(tftypes.Object).AttributeTypes {
			out[n] = sampleJSON(at)
		}
		return out
	}
	return nil
}

// The no-drift guarantee for existing state: every resource is at schema
// version 0, so the provider's UpgradeResourceState must hand back exactly the
// state it was given — for every spec, with every attribute populated.
func TestUpgradeResourceState_Version0IsIdentityForEverySpec(t *testing.T) {
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil || len(schemas.ResourceSchemas) < 50 {
		t.Fatalf("GetProviderSchema: %v (%d schemas)", err, len(schemas.ResourceSchemas))
	}
	for name, sch := range schemas.ResourceSchemas {
		if sch.Version != 0 {
			t.Errorf("%s: schema version %d; existing state is version 0", name, sch.Version)
			continue
		}
		typ := sch.ValueType()
		raw, _ := json.Marshal(sampleJSON(typ))
		resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
			TypeName: name, Version: 0, RawState: &tfprotov6.RawState{JSON: raw},
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, d := range resp.Diagnostics {
			t.Errorf("%s: %s: %s", name, d.Summary, d.Detail)
		}
		if resp.UpgradedState == nil {
			continue
		}
		got, err := resp.UpgradedState.Unmarshal(typ)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want, err := (&tfprotov6.RawState{JSON: raw}).UnmarshalWithOpts(typ, tfprotov6.UnmarshalOpts{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !got.Equal(want) {
			t.Errorf("%s: version-0 state changed on upgrade:\n got  %v\n want %v", name, got, want)
		}
	}
}

// Declared steps chain from the state's version up to the current one.
func TestUpgradeState_ChainsDeclaredSteps(t *testing.T) {
	r := &dynamicResource{spec: ResourceSpec{
		Name: "thing", SchemaVersion: 2,
		StateUpgrades: []StateUpgradeStep{
			{FromVersion: 0, Renames: map[string]string{"eks_version": "version"}},
			{FromVersion: 1, ToNumber: []string{"limit"}, Drop: []string{"legacy"}},
		},
	}}
	ups := r.UpgradeState(context.Background())
	if len(ups) != 2 {
		t.Fatalf("want upgraders for versions 0 and 1, got %d", len(ups))
	}
	run := func(from int64, in string) map[string]any {
		var resp resource.UpgradeStateResponse
		ups[from].StateUpgrader(context.Background(), resource.UpgradeStateRequest{RawState: &tfprotov6.RawState{JSON: []byte(in)}}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("from %d: %v", from, resp.Diagnostics)
		}
		var m map[string]any
		_ = json.Unmarshal(resp.DynamicValue.JSON, &m)
		return m
	}
	// From version 0: both steps.
	m := run(0, `{"id":"ws/1","eks_version":"1.30","limit":"25","legacy":"x"}`)
	if m["version"] != "1.30" || m["eks_version"] != nil || m["limit"] != float64(25) || m["legacy"] != nil {
		t.Errorf("from 0: want rename + number + drop, got %v", m)
	}
	// From version 1: only the second step (the rename already happened).
	m = run(1, `{"id":"ws/1","version":"1.31","limit":"5","legacy":"x"}`)
	if m["version"] != "1.31" || m["limit"] != float64(5) || m["legacy"] != nil {
		t.Errorf("from 1: want number + drop only, got %v", m)
	}
	// No version bump, no upgraders.
	if n := len((&dynamicResource{spec: ResourceSpec{Name: "plain"}}).UpgradeState(context.Background())); n != 0 {
		t.Errorf("schemaVersion 0 must register no upgraders, got %d", n)
	}
}

func TestStateUpgradesValidation(t *testing.T) {
	cases := []struct{ name, extra, wantErr string }{
		{"valid", `"schemaVersion":1,"stateUpgrades":[{"fromVersion":0,"renames":{"old":"name"}}]`, ""},
		{"upgrades without a version", `"stateUpgrades":[{"fromVersion":0,"renames":{"old":"name"}}]`, "fromVersion 0 must be in"},
		{"fromVersion out of range", `"schemaVersion":1,"stateUpgrades":[{"fromVersion":1}]`, "fromVersion 1 must be in"},
		{"duplicate fromVersion", `"schemaVersion":2,"stateUpgrades":[{"fromVersion":0},{"fromVersion":0}]`, "listed twice"},
		{"rename target missing", `"schemaVersion":1,"stateUpgrades":[{"fromVersion":0,"renames":{"a":"nope"}}]`, "is not an attribute"},
		{"drop of a live attribute", `"schemaVersion":1,"stateUpgrades":[{"fromVersion":0,"drop":["name"]}]`, "still an attribute"},
		{"negative version", `"schemaVersion":-1`, "must not be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"name":"probe","description":"probe","idPath":"id","endpoint":{"uriBase":"/v3/admin/things"},` + tc.extra + `,
			  "attributes":[{"name":"name","type":"string","required":true,"apiPath":"name"}]}`)
			var spec ResourceSpec
			if err := json.Unmarshal(raw, &spec); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := spec.validate()
			if tc.wantErr == "" && err != nil {
				t.Fatalf("expected load, got %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
