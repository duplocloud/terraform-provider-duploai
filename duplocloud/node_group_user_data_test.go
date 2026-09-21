package duplocloud

import (
	"strings"
	"testing"
)

// Backend PR #573 added spec.userData and spec.userDataMode to AWSEksNodeGroupSpec:
// user data for the launch template, and how it combines with the bootstrap payload
// the platform generates for a CUSTOM AMI. These pin the provider's side of that
// contract — the constraints the API enforces, so a bad value fails at plan rather
// than mid-apply.
func TestNodeGroupUserDataContract(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	var ng *ResourceSpec
	for i := range specs {
		if specs[i].Name == "node_group" {
			ng = &specs[i]
			break
		}
	}
	if ng == nil {
		t.Fatal("node_group spec not found")
	}

	t.Run("user_data", func(t *testing.T) {
		a := ng.attr("user_data")
		if a == nil {
			t.Fatal("node_group has no user_data attribute — spec.userData is unreachable from Terraform")
		}
		if a.Type != "string" {
			t.Errorf("type = %q, want string", a.Type)
		}
		if !a.Optional {
			t.Error("user_data must be optional — a node group needs no user data")
		}
		// Bootstrap scripts routinely carry registry credentials, join tokens or
		// licence keys; the backend marks the field [SecretValue] for the same
		// reason. Without this the value lands in plan output and CI logs.
		if !a.Sensitive {
			t.Error("user_data must be sensitive: bootstrap scripts carry credentials")
		}
		// The backend caps at 1024 BYTES (MaxUserDataBytes). maxLength counts
		// characters, which matches for the ASCII scripts this holds; a multibyte
		// script could still be rejected by the API, which is the safe direction.
		if a.MaxLength != 1024 {
			t.Errorf("maxLength = %d, want 1024 to match the API cap", a.MaxLength)
		}
		if a.APIPath != "spec.userData" {
			t.Errorf("apiPath = %q, want spec.userData", a.APIPath)
		}
		// The API rejects a pre-encoded value: it base64-encodes the plain text
		// itself. That is the inverse of native_host, so the description has to
		// say it or the mistake is inevitable.
		if !strings.Contains(a.Description, "PLAIN TEXT") || !strings.Contains(a.Description, "base64") {
			t.Error("user_data description must state that the value is plain text and " +
				"must not be pre-encoded — the opposite of native_host.base64_user_data")
		}
	})

	t.Run("user_data_mode", func(t *testing.T) {
		a := ng.attr("user_data_mode")
		if a == nil {
			t.Fatal("node_group has no user_data_mode attribute")
		}
		// Server-defaulted, so optional+computed with the same default the API uses;
		// optional-only would drift on every refresh.
		if !a.Optional || !a.Computed {
			t.Errorf("user_data_mode must be optional+computed (got optional=%v computed=%v)", a.Optional, a.Computed)
		}
		// No TF-side default on purpose: the API always returns userDataMode —
		// EksUserDataMode is a non-nullable enum defaulting to Override, with no
		// conditional ignore, so even a node group created before the field
		// existed reads back "Override". That is the condition optional+computed
		// needs to be safe without a default, and it keeps the server the single
		// owner of the value rather than duplicating it here.
		if a.Default != nil {
			t.Errorf("default = %s, want none — the server always supplies userDataMode", string(*a.Default))
		}
		want := map[string]bool{"Override": true, "Append": true}
		if len(a.OneOf) != len(want) {
			t.Errorf("oneOf = %v, want exactly Override and Append", a.OneOf)
		}
		for _, v := range a.OneOf {
			if !want[v] {
				t.Errorf("oneOf contains %q, which EksUserDataMode does not define", v)
			}
		}
		if a.APIPath != "spec.userDataMode" {
			t.Errorf("apiPath = %q, want spec.userDataMode", a.APIPath)
		}
	})

	// Neither field is forceNew: the backend reconciles any spec change through the
	// provisioning skill (GetSpecChangeMessage is deliberately field-agnostic, and
	// CloudFormation no-ops on unchanged parameters), so changing a bootstrap script
	// must not destroy the node group.
	t.Run("mutable", func(t *testing.T) {
		for _, n := range []string{"user_data", "user_data_mode"} {
			if a := ng.attr(n); a != nil && a.ForceNew {
				t.Errorf("%s is forceNew: changing a bootstrap script would replace the whole node group", n)
			}
		}
	})

	// The API rejects /dev/xvda and /dev/sda1 on an additional volume — both, because
	// Amazon Linux and Ubuntu name the root device differently. It cannot be a
	// plan-time rule: leafAt refuses to traverse a list of objects, so invalidWhen
	// cannot reach volumes[].device_name. The description carries it instead.
	t.Run("device_name documents the root-device rejection", func(t *testing.T) {
		vols := ng.attr("volumes")
		if vols == nil {
			t.Fatal("node_group has no volumes attribute")
		}
		var dev *AttributeSpec
		for i := range vols.Attributes {
			if vols.Attributes[i].Name == "device_name" {
				dev = &vols.Attributes[i]
				break
			}
		}
		if dev == nil {
			t.Fatal("volumes has no device_name attribute")
		}
		for _, root := range []string{"/dev/xvda", "/dev/sda1"} {
			if !strings.Contains(dev.Description, root) {
				t.Errorf("device_name description does not mention %s, which the API rejects", root)
			}
		}
	})
}
