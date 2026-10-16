package duplocloud

import "testing"

// Every new Key Vault secret version (any value change, and with `value` every
// update) gets a new secret_id and created_on, so neither may be held at its
// prior value: `stable` there failed applies with "Provider produced
// inconsistent result after apply".
func TestKeyVaultSecret_VersionOutputsNotStable(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Name != "azure_key_vault_secret" {
			continue
		}
		for _, n := range []string{"secret_id", "created_on"} {
			if a := s.attr(n); a == nil || a.Stable || len(a.StableUnlessChanged) > 0 {
				t.Errorf("%s must exist and change with each secret version (not stable)", n)
			}
		}
		return
	}
	t.Fatal("azure_key_vault_secret spec not found")
}
