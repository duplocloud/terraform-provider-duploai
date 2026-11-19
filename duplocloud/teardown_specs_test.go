package duplocloud

import (
	"slices"
	"testing"
)

func specByName(t *testing.T, name string) *ResourceSpec {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i]
		}
	}
	t.Fatalf("%s spec not found", name)
	return nil
}

// F-15: the backend refuses to delete an AzurePrivateEndpoint whose ARM id is set
// unless it is Failed or DeProvisioned (origin/dev AzurePrivateEndpointHooks.
// GetDeletableStatuses), so `terraform destroy` must deprovision first and wait for
// DeProvisioned — the same shape as storage_account and azure_key_vault.
func TestAzurePrivateEndpoint_DeprovisionsBeforeDelete(t *testing.T) {
	s := specByName(t, "azure_private_endpoint")
	if s.Endpoint.Deprovision == nil {
		t.Fatal("azure_private_endpoint has no endpoint.deprovision: destroy fails with " +
			"\"must be deprovisioned first\" on every Ready endpoint")
	}
	if s.Waiter == nil || s.Waiter.DeprovisionedState != "DeProvisioned" {
		t.Fatalf("azure_private_endpoint waiter.deprovisionedState must be \"DeProvisioned\" "+
			"so delete waits for the teardown; got waiter %+v", s.Waiter)
	}
	ep, err := s.BuildEndpoint()
	if err != nil {
		t.Fatalf("BuildEndpoint: %v", err)
	}
	if !ep.HasDeprovision() {
		t.Error("built endpoint has no deprovision operation")
	}
}

// F-12: the backend turns metaData.delete_protection on for every new workspace
// (origin/dev WorkspaceService, key "delete_protection") and refuses to delete a
// protected one, so destroy failed with no attribute to turn it off. The fix follows
// resource_group / k8s_namespace: a typed delete_protection bool on
// metaData.delete_protection, with meta_data hiding that key unless declared.
func TestAdminWorkspace_DeleteProtectionAttribute(t *testing.T) {
	s := specByName(t, "admin_workspace")
	dp := s.attr("delete_protection")
	if dp == nil {
		t.Fatal("admin_workspace has no delete_protection attribute: destroy fails by default")
	}
	if dp.Type != "bool" || !dp.Optional || !dp.Computed || dp.Required || !dp.StringBool ||
		dp.APIPath != "metaData.delete_protection" || dp.ForceNew {
		t.Errorf("delete_protection = %+v; want optional+computed stringBool bool at metaData.delete_protection, not forceNew", *dp)
	}
	md := s.attr("meta_data")
	if md == nil {
		t.Fatal("meta_data attribute removed: existing configs would break")
	}
	if md.Type != "map(string)" || !md.Optional || !md.Computed || md.APIPath != "metaData" {
		t.Errorf("meta_data changed shape (%+v): existing configs and state must keep working", *md)
	}
	if !slices.Contains(md.FilterResponseKeys, "delete_protection") {
		t.Error("meta_data must filter delete_protection, or a config listing other keys drifts on the server-stamped one")
	}
	// The typed flag must be written after the map so it wins (see metadata_map_test.go).
	mdIdx, dpIdx := -1, -1
	for i, a := range s.Attributes {
		switch a.Name {
		case "meta_data":
			mdIdx = i
		case "delete_protection":
			dpIdx = i
		}
	}
	if dpIdx < mdIdx {
		t.Errorf("delete_protection (index %d) must come after meta_data (index %d), or the map overwrites the flag", dpIdx, mdIdx)
	}
}

// Backward compatibility for F-12: state written before delete_protection existed
// carries the server-stamped key inside meta_data. The refresh keep-set is the prior
// state's keys, so that key must survive the new filter — otherwise every existing
// workspace (and any config using the meta_data = { delete_protection = "false" }
// workaround) would show a diff after upgrading.
func TestAdminWorkspace_LegacyMetaDataKeyKept(t *testing.T) {
	s := specByName(t, "admin_workspace")
	md := s.attr("meta_data")
	resp := map[string]any{"delete_protection": "false", "owner": "team-a"}

	got, _ := filterMapKeys(resp, md.FilterResponseKeys, map[string]bool{"delete_protection": true, "owner": true}).(map[string]any)
	if got["delete_protection"] != "false" || got["owner"] != "team-a" {
		t.Errorf("legacy/declared key dropped on refresh: %#v", got)
	}
	got, _ = filterMapKeys(resp, md.FilterResponseKeys, map[string]bool{"owner": true}).(map[string]any)
	if _, present := got["delete_protection"]; present {
		t.Errorf("undeclared delete_protection leaked into meta_data: %#v", got)
	}
}

// F-11: for a missing secret the Key Vault API answers 400 "Secret '<n>' was not
// found in Key Vault '<v>'" (origin/dev AzureKeyVaultSecretService throws
// KeyNotFoundException, which the controller maps to 400). The spec declares that
// one shape as not-found so refresh drops the secret and destroy succeeds after an
// out-of-band delete; a validation 400 from the same API must still fail.
func TestAzureKeyVaultSecret_NotFoundResponse(t *testing.T) {
	s := specByName(t, "azure_key_vault_secret")
	ep, err := s.BuildEndpoint()
	if err != nil {
		t.Fatalf("BuildEndpoint: %v", err)
	}
	if len(ep.NotFoundResponses) != 1 {
		t.Fatalf("want exactly one notFoundResponses rule, got %d", len(ep.NotFoundResponses))
	}
	rule := ep.NotFoundResponses[0]
	if rule.Status != 400 {
		t.Errorf("rule status = %d, want 400", rule.Status)
	}
	for _, msg := range []string{
		"Secret 'qa-live-kvs-oob-10271445' was not found in Key Vault 'tf-kv-full-test1'.",
	} {
		if !rule.Message.MatchString(msg) {
			t.Errorf("rule does not match the backend's not-found message %q", msg)
		}
	}
	for _, msg := range []string{
		"Value cannot be null. (Parameter 'Version')",
		"Key Vault 'tf-kv-full-test1' was not found in Azure.",
		"Secret name 'bad name' is invalid; it was not found in the allowed character set.",
	} {
		if rule.Message.MatchString(msg) {
			t.Errorf("rule wrongly treats %q as not-found", msg)
		}
	}
}

// Only the key vault secret declares a not-found rule: every other resource keeps
// the plain 404 contract.
func TestNotFoundResponses_OnlyWhereDeclared(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for i := range specs {
		if len(specs[i].Endpoint.NotFoundResponses) > 0 && specs[i].Name != "azure_key_vault_secret" {
			t.Errorf("%s declares notFoundResponses; add it here deliberately, with backend evidence", specs[i].Name)
		}
	}
}

func TestNotFoundResponses_Validation(t *testing.T) {
	cases := map[string]NotFoundResponseSpec{
		"404 is already not-found":  {Status: 404, MessagePattern: "x"},
		"2xx is not an error":       {Status: 200, MessagePattern: "x"},
		"5xx is not a client error": {Status: 500, MessagePattern: "x"},
		"empty pattern":             {Status: 400},
		"bad regexp":                {Status: 400, MessagePattern: "("},
	}
	for name, rule := range cases {
		t.Run(name, func(t *testing.T) {
			s := ResourceSpec{Name: "x", IDPath: "id", Endpoint: EndpointSpec{
				UriBase: "/x", NotFoundResponses: []NotFoundResponseSpec{rule}}}
			if _, err := s.BuildEndpoint(); err == nil {
				t.Errorf("rule %+v accepted", rule)
			}
		})
	}
}
