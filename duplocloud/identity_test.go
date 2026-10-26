package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// The framework rejects an identity that changes after create ("Unexpected
// Identity Change"). Identity = the composite id's parts: the endpoint path
// parameters and the object id. The object id is assigned once; a path
// parameter could only change in place if its attribute were not forceNew —
// this guards every spec against that, now and for future specs.
func TestIdentity_ImmutableForEverySpec(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, s := range specs {
		if s.DataSourceOnly {
			continue
		}
		ep, err := s.BuildEndpoint()
		if err != nil {
			t.Fatal(err)
		}
		r := &dynamicResource{spec: s, endpoint: ep}
		var resp resource.IdentitySchemaResponse
		r.IdentitySchema(context.Background(), resource.IdentitySchemaRequest{}, &resp)
		names := r.identityAttrs()
		if len(resp.IdentitySchema.Attributes) != len(names) || len(names) == 0 {
			t.Errorf("%s: identity schema has %d attributes, want %v", s.Name, len(resp.IdentitySchema.Attributes), names)
		}
		for _, n := range ep.PathParams() {
			a := s.attr(n)
			if a == nil || !a.ForceNew {
				t.Errorf("%s: path parameter %q is part of the identity but not forceNew, so an in-place change would alter the identity", s.Name, n)
			}
		}
		checked++
	}
	if checked < 50 {
		t.Fatalf("only %d resource specs checked", checked)
	}
}

// Identity round-trips through the composite id in both directions, for a
// normal resource and an association (path parameters only, no object id).
func TestIdentity_ComposedFromCompositeID(t *testing.T) {
	specs, _ := loadResourceSpecs()
	var plain, assoc *dynamicResource
	for _, s := range specs {
		ep, _ := s.BuildEndpoint()
		switch {
		case s.Name == "resource_group":
			plain = &dynamicResource{spec: s, endpoint: ep}
		case s.Association != nil && assoc == nil:
			assoc = &dynamicResource{spec: s, endpoint: ep}
		}
	}
	if plain == nil {
		t.Fatal("resource_group spec not found")
	}
	if got := plain.identityAttrs(); len(got) != 2 || got[0] != "workspace_id" || got[1] != "id" {
		t.Errorf("resource_group identity = %v, want [workspace_id id]", got)
	}
	if assoc != nil {
		for _, n := range assoc.identityAttrs() {
			if n == "id" {
				t.Errorf("%s: an association has no object id; identity must be its path parameters only", assoc.spec.Name)
			}
		}
	}
}
