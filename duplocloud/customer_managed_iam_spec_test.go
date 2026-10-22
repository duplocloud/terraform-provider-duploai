package duplocloud

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// existing_iam_role_arn / existing_kms_key_arn let a customer whose AWS account forbids IAM creation
// supply their own role and key; the API then creates no IAM or KMS for that baseline. The API treats
// both as immutable once set and keeps the stored value when an update omits them.
//
// optional + computed + forceNew is what matches that contract: left unset, UseStateForUnknown carries
// the stored value into the plan (so an imported or UI-created resource reads it back without a
// diff), the full-document PUT resends it unchanged, and changing it plans a replacement instead of
// an update the API would reject. These tests pin those flags, the plan modifiers they build, the
// plan-time rules, and the patterns, so a bad ARN fails at plan rather than at apply.

func customerManagedSpecs(t *testing.T) map[string]*ResourceSpec {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	byName := map[string]*ResourceSpec{}
	for i := range specs {
		byName[specs[i].Name] = &specs[i]
	}
	return byName
}

func TestCustomerManagedInputsAreOptionalComputedForceNew(t *testing.T) {
	specs := customerManagedSpecs(t)
	cases := []struct{ resource, attr, apiPath string }{
		{"network_baseline", "existing_iam_role_arn", "spec.existingIamRoleArn"},
		{"resource_group", "existing_iam_role_arn", "spec.existingIamRoleArn"},
		{"resource_group", "existing_kms_key_arn", "spec.existingKmsKeyArn"},
		{"cluster_baseline", "existing_iam_role_arn", "spec.existingIamRoleArn"},
		{"cluster_baseline", "existing_kms_key_arn", "spec.existingKmsKeyArn"},
	}
	for _, c := range cases {
		t.Run(c.resource+"."+c.attr, func(t *testing.T) {
			s := specs[c.resource]
			if s == nil {
				t.Fatalf("no embedded spec %q", c.resource)
			}
			a := s.attr(c.attr)
			if a == nil {
				t.Fatalf("%s has no %s attribute", c.resource, c.attr)
			}
			if !a.Optional || !a.Computed || !a.ForceNew {
				t.Errorf("optional=%v computed=%v forceNew=%v, want all true", a.Optional, a.Computed, a.ForceNew)
			}
			if a.Required || a.NoSend || a.CreateOnly {
				t.Errorf("required=%v noSend=%v createOnly=%v, want all false: the value must be resent on every PUT",
					a.Required, a.NoSend, a.CreateOnly)
			}
			if a.APIPath != c.apiPath {
				t.Errorf("apiPath=%q, want %q", a.APIPath, c.apiPath)
			}
			if a.Pattern == "" || a.PatternDescription == "" {
				t.Errorf("missing pattern/patternDescription: a bad ARN would only fail at apply")
			}
			if !hasAwsOnlyRule(s, c.attr) {
				t.Errorf("no invalidWhen rule rejecting %s when cloud is not Aws", c.attr)
			}
			// The built schema, not just the flags: keep-null UseStateForUnknown first (so a resource
			// created without the field, or an upgraded state, plans null against null), then
			// RequiresReplace.
			mods := attrSchema(*a).(schema.StringAttribute).PlanModifiers
			if len(mods) != 2 || reflect.TypeOf(mods[0]) != reflect.TypeOf(stringplanmodifier.UseStateForUnknown()) ||
				reflect.TypeOf(mods[1]) != reflect.TypeOf(stringplanmodifier.RequiresReplace()) {
				t.Errorf("plan modifiers = %v, want [UseStateForUnknown RequiresReplace]", mods)
			}
		})
	}
}

func hasAwsOnlyRule(s *ResourceSpec, attr string) bool {
	for _, r := range s.InvalidWhen {
		var notAws, isSet bool
		for _, w := range r.When {
			if w.Attribute == "cloud" && w.NotEquals == "Aws" {
				notAws = true
			}
			if w.Attribute == attr && w.IsNotEmpty {
				isSet = true
			}
		}
		if notAws && isSet {
			return true
		}
	}
	return false
}

// ClusterAttributes' role is stamped by the server from the parent cluster; it must never be sent.
// It is null for most clusters, so it must keep a null from state: a stable-only field gets
// UseNonNullStateForUnknown and would show "known after apply" on every real update.
func TestClusterAttributesExistingRoleIsServerOwned(t *testing.T) {
	a := customerManagedSpecs(t)["cluster_attributes"].attr("existing_iam_role_arn")
	if a == nil {
		t.Fatal("cluster_attributes has no existing_iam_role_arn attribute")
	}
	if !a.Computed || a.Optional || a.Required || a.SendFromState {
		t.Errorf("computed=%v optional=%v required=%v sendFromState=%v, want computed-only and never sent",
			a.Computed, a.Optional, a.Required, a.SendFromState)
	}
	if !useStateForUnknown(*a) || preserveNonNullOnly(*a) {
		t.Errorf("useStateForUnknown=%v preserveNonNullOnly=%v, want a modifier that keeps a null from state",
			useStateForUnknown(*a), preserveNonNullOnly(*a))
	}
	mods := attrSchema(*a).(schema.StringAttribute).PlanModifiers
	if len(mods) == 0 || reflect.TypeOf(mods[0]) != reflect.TypeOf(stringplanmodifier.UseStateForUnknown()) {
		t.Errorf("first plan modifier = %v, want UseStateForUnknown", mods)
	}
	if a.APIPath != "spec.existingIamRoleArn" {
		t.Errorf("apiPath=%q, want spec.existingIamRoleArn", a.APIPath)
	}
}

func specPattern(t *testing.T, resource, attr string) *regexp.Regexp {
	t.Helper()
	a := customerManagedSpecs(t)[resource].attr(attr)
	if a == nil {
		t.Fatalf("%s has no %s attribute", resource, attr)
	}
	return regexp.MustCompile(a.Pattern)
}

// Accept/reject cases mirroring the API's own role-ARN validation, so the plan-time check never accepts
// a value the API would reject. Where the two differ the pattern is the stricter one.
func TestExistingIamRoleArnPattern(t *testing.T) {
	for _, resource := range []string{"network_baseline", "resource_group", "cluster_baseline"} {
		re := specPattern(t, resource, "existing_iam_role_arn")
		for _, ok := range []string{
			"arn:aws:iam::123456789012:role/platform-shared",
			"arn:aws:iam::123456789012:role/path/to/platform-shared",
			"arn:aws-us-gov:iam::123456789012:role/platform-shared",
			"arn:aws-cn:iam::123456789012:role/platform-shared",
			"arn:aws:iam::123456789012:role/svc+role=a,b.c@d-e_f",
		} {
			if !re.MatchString(ok) {
				t.Errorf("%s: rejected valid role ARN %q", resource, ok)
			}
		}
		for _, bad := range []string{
			"platform-shared",
			"arn:aws:iam::123456789012:user/alice",
			"arn:aws:iam::123456789012:instance-profile/platform",
			"arn:aws:iam::12345:role/platform-shared",
			"arn:aws:iam::123456789012:role/",
			"arn:aws:iam:us-east-1:123456789012:role/platform-shared", // IAM ARNs carry no region
			`arn:aws:iam::123456789012:role/a"b`,
			"arn:aws:iam::123456789012:role/$(id)",
			"arn:aws:iam::123456789012:role/a b",
			"arn:aws:iam::123456789012:role/café",
			"arn:aws$(id):iam::123456789012:role/platform-shared",
			"arn:aws:iam::123456789012:role/" + strings.Repeat("a", 65), // IAM caps a role name at 64
		} {
			if re.MatchString(bad) {
				t.Errorf("%s: accepted invalid role ARN %q", resource, bad)
			}
		}
	}
}

// Accept/reject cases mirroring the API's own KMS-key-ARN validation.
func TestExistingKmsKeyArnPattern(t *testing.T) {
	for _, resource := range []string{"resource_group", "cluster_baseline"} {
		re := specPattern(t, resource, "existing_kms_key_arn")
		for _, ok := range []string{
			"arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
			"arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			"arn:aws-cn:kms:cn-north-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
			"arn:aws-iso:kms:us-iso-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
			"arn:aws-eusc:kms:eusc-de-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
		} {
			if !re.MatchString(ok) {
				t.Errorf("%s: rejected valid key ARN %q", resource, ok)
			}
		}
		for _, bad := range []string{
			"1234abcd-12ab-34cd-56ef-1234567890ab",
			"alias/my-key",
			"arn:aws:kms:us-east-1:123456789012:alias/my-key",
			"arn:aws:iam::123456789012:role/platform-shared",
			"arn:aws:kms:us-east-1:12345:key/1234abcd-12ab-34cd-56ef-1234567890ab",
			"arn:aws:kms::123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
			"arn:aws:kms:us-east-1:123456789012:key/",
			"arn:aws:kms:us-east-1:123456789012:key/$(id)",
			"arn:aws:kms:us-east-1:123456789012:key/not-a-key-id",
			"arn:aws:kms:us-east-1:123456789012:key/mrk-1234",
			"arn:aws:kms:us-east-1:123456789012:key/1234ABCD-12AB-34CD-56EF-1234567890AB", // key ids are lowercase
			"arn:aws:kms:us-east-1$(id):123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
		} {
			if re.MatchString(bad) {
				t.Errorf("%s: accepted invalid key ARN %q", resource, bad)
			}
		}
	}
}

// invalidWhenFires runs the shipped spec's invalidWhen rules over a config holding only the given
// attributes; every other attribute is null, so defaults (cloud = "Aws", mode = "Create",
// enable_flow_logs = false) apply exactly as they would for a user who leaves them out.
func invalidWhenFires(t *testing.T, resourceName string, set map[string]string) bool {
	t.Helper()
	spec := customerManagedSpecs(t)[resourceName]
	if spec == nil {
		t.Fatalf("no embedded spec %q", resourceName)
	}
	ctx := context.Background()
	r := &dynamicResource{spec: *spec}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType, ok := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("%s schema is not an object", resourceName)
	}
	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(typ, nil)
	}
	for name, v := range set {
		typ, ok := objType.AttributeTypes[name]
		if !ok {
			t.Fatalf("%s has no attribute %q", resourceName, name)
		}
		if typ.Is(tftypes.Bool) {
			vals[name] = tftypes.NewValue(tftypes.Bool, v == "true")
		} else {
			vals[name] = tftypes.NewValue(tftypes.String, v)
		}
	}
	req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(objType, vals)}}
	resp := resource.ValidateConfigResponse{}
	invalidWhenValidator{spec: *spec}.ValidateResource(ctx, req, &resp)
	return resp.Diagnostics.HasError()
}

func TestCustomerManagedInvalidWhenRules(t *testing.T) {
	const role = "arn:aws:iam::123456789012:role/platform-shared"
	const key = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	cases := []struct {
		resource string
		set      map[string]string
		want     bool
	}{
		// network: AWS only, needs flow logs, not in Import mode
		{"network_baseline", map[string]string{"existing_iam_role_arn": role, "enable_flow_logs": "true"}, false},
		{"network_baseline", map[string]string{"existing_iam_role_arn": role}, true}, // flow logs default off
		{"network_baseline", map[string]string{"existing_iam_role_arn": role, "enable_flow_logs": "true", "cloud": "Azure"}, true},
		{"network_baseline", map[string]string{"existing_iam_role_arn": role, "enable_flow_logs": "true", "mode": "Import"}, true},
		{"network_baseline", map[string]string{"cloud": "Azure"}, false},
		// resource group: AWS only
		{"resource_group", map[string]string{"existing_iam_role_arn": role, "existing_kms_key_arn": key}, false},
		{"resource_group", map[string]string{"existing_iam_role_arn": role, "cloud": "Azure"}, true},
		{"resource_group", map[string]string{"existing_kms_key_arn": key, "cloud": "K8S_ONLY"}, true},
		{"resource_group", map[string]string{"cloud": "Azure"}, false},
		// cluster: AWS only, not in Import mode
		{"cluster_baseline", map[string]string{"existing_iam_role_arn": role}, false},
		{"cluster_baseline", map[string]string{"existing_iam_role_arn": role, "cloud": "Gcp"}, true},
		{"cluster_baseline", map[string]string{"existing_iam_role_arn": role, "mode": "Import"}, true},
		{"cluster_baseline", map[string]string{"mode": "Import"}, false},
		{"cluster_baseline", map[string]string{"existing_kms_key_arn": key}, false},
		{"cluster_baseline", map[string]string{"existing_iam_role_arn": role, "existing_kms_key_arn": key}, false},
		{"cluster_baseline", map[string]string{"existing_kms_key_arn": key, "cloud": "Azure"}, true},
		{"cluster_baseline", map[string]string{"existing_kms_key_arn": key, "mode": "Import"}, true},
	}
	for _, c := range cases {
		if got := invalidWhenFires(t, c.resource, c.set); got != c.want {
			t.Errorf("%s %v: invalidWhen fired=%v, want %v", c.resource, c.set, got, c.want)
		}
	}
}
