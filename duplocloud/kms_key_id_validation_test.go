package duplocloud

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// DUPLOAI-5637: kms_key_id on rds_cluster / rds_instance rejects empty and
// whitespace-only values at plan time (HashiCorp guidance: null means unset),
// instead of the reverted emptyMeansUnset workaround.

const kmsPatternDescription = "kms_key_id must not be empty; omit it or set it to null to use the platform default"

var kmsSpecs = []string{"rds_cluster", "rds_instance"}

func kmsAttrSpec(t *testing.T, specName string) AttributeSpec {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, s := range specs {
		if s.Name != specName {
			continue
		}
		for _, a := range s.Attributes {
			if a.Name == "kms_key_id" {
				return a
			}
		}
		t.Fatalf("%s has no kms_key_id", specName)
	}
	t.Fatalf("spec %s not found", specName)
	return AttributeSpec{}
}

// runKmsValidators drives the real resource schema validators built from the shipped spec.
func runKmsValidators(t *testing.T, specName string, v types.String) []string {
	t.Helper()
	sa, ok := attrSchema(kmsAttrSpec(t, specName)).(schema.StringAttribute)
	if !ok {
		t.Fatalf("%s.kms_key_id is not a schema.StringAttribute", specName)
	}
	var msgs []string
	p := path.Root("kms_key_id")
	for _, val := range sa.Validators {
		resp := &validator.StringResponse{}
		val.ValidateString(context.Background(), validator.StringRequest{Path: p, ConfigValue: v}, resp)
		for _, d := range resp.Diagnostics.Errors() {
			msgs = append(msgs, d.Summary()+": "+d.Detail())
		}
	}
	return msgs
}

func TestKmsKeyID_SpecShape(t *testing.T) {
	for _, n := range kmsSpecs {
		a := kmsAttrSpec(t, n)
		if a.Type != "string" || !a.Optional || !a.Computed || !a.ForceNew || a.Required {
			t.Errorf("%s.kms_key_id must stay string optional+computed+forceNew as in v0.7.0: %+v", n, a)
		}
		if a.APIPath != "spec.storageKmsKeyId" {
			t.Errorf("%s.kms_key_id apiPath = %q, want spec.storageKmsKeyId", n, a.APIPath)
		}
		if a.Pattern != `\S` {
			t.Errorf("%s.kms_key_id pattern = %q, want \\S", n, a.Pattern)
		}
		if a.PatternDescription != kmsPatternDescription {
			t.Errorf("%s.kms_key_id patternDescription = %q", n, a.PatternDescription)
		}
		if a.MinLength != 0 {
			t.Errorf("%s.kms_key_id minLength = %d; plan says no minLength (duplicate error)", n, a.MinLength)
		}
		if strings.Contains(a.Description, "set it to an empty string") {
			t.Errorf("%s.kms_key_id description still tells users to set an empty string", n)
		}
		if !strings.Contains(a.Description, "Omit it or set it to null to use the resource group's default key; empty strings are rejected.") {
			t.Errorf("%s.kms_key_id description lacks the null/omit guidance", n)
		}
	}
}

func TestKmsKeyID_RejectsEmptyAndWhitespace(t *testing.T) {
	for _, n := range kmsSpecs {
		for _, s := range []string{"", " ", "\t", "\n", "\r", " \t\n", "   "} {
			msgs := runKmsValidators(t, n, types.StringValue(s))
			if len(msgs) == 0 {
				t.Errorf("%s: kms_key_id %q must fail at plan", n, s)
				continue
			}
			joined := strings.Join(msgs, "\n")
			if !strings.Contains(joined, kmsPatternDescription) {
				t.Errorf("%s: kms_key_id %q error lacks patternDescription: %s", n, s, joined)
			}
			if len(msgs) != 1 {
				t.Errorf("%s: kms_key_id %q gave %d errors, want exactly 1: %s", n, s, len(msgs), joined)
			}
		}
	}
}

func TestKmsKeyID_AcceptsRealValues(t *testing.T) {
	ok := []string{
		"arn:aws:kms:us-west-2:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
		"arn:aws:kms:us-west-2:123456789012:alias/x",
		"arn:aws:kms:eu-west-3:210987654321:key/mrk-0123456789abcdef0123456789abcdef",
		"1234abcd-12ab-34cd-56ef-1234567890ab",
		"a ", " a",
		strings.Repeat("k", 2048),
	}
	for _, n := range kmsSpecs {
		for _, s := range ok {
			if msgs := runKmsValidators(t, n, types.StringValue(s)); len(msgs) != 0 {
				t.Errorf("%s: kms_key_id %q must be accepted: %v", n, s, msgs)
			}
		}
		if msgs := runKmsValidators(t, n, types.StringNull()); len(msgs) != 0 {
			t.Errorf("%s: null kms_key_id must pass: %v", n, msgs)
		}
		if msgs := runKmsValidators(t, n, types.StringUnknown()); len(msgs) != 0 {
			t.Errorf("%s: unknown kms_key_id must pass: %v", n, msgs)
		}
	}
}

// Known gap (accepted in plan): RE2 \S is ASCII-only, so \v and Unicode spaces
// pass. Pin the behaviour so a change is visible.
func TestKmsKeyID_KnownGapUnicodeAndVerticalTab(t *testing.T) {
	for _, n := range kmsSpecs {
		if len(runKmsValidators(t, n, types.StringValue(""))) == 0 {
			t.Fatalf("%s: validator absent; known-gap pin is meaningless until \\S is wired", n)
		}
		for _, s := range []string{"\v", " ", " ", "\f"} {
			msgs := runKmsValidators(t, n, types.StringValue(s))
			want := s == "\f" // \f is in RE2 \s, so it is rejected
			if (len(msgs) > 0) != want {
				t.Errorf("%s: kms_key_id %q rejected=%v, known-gap pin expects %v", n, s, len(msgs) > 0, want)
			}
		}
	}
}

func TestKmsKeyID_DataSourceComputedOnlyNoValidatorFires(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Name != "rds_cluster" && s.Name != "rds_instance" {
			continue
		}
		ds := newDynamicDataSourceFactory(s, duplosdk.Endpoint{})()
		resp := &datasource.SchemaResponse{}
		ds.Schema(context.Background(), datasource.SchemaRequest{}, resp)
		a, ok := resp.Schema.Attributes["kms_key_id"].(dsschema.StringAttribute)
		if !ok {
			t.Errorf("%s data source: kms_key_id missing or not string", s.Name)
			continue
		}
		if !a.Computed || a.Optional || a.Required {
			t.Errorf("%s data source: kms_key_id must be computed-only, got %+v", s.Name, a)
		}
	}
}

func TestKmsKeyID_EmptyMeansUnsetFullyReverted(t *testing.T) {
	if _, ok := reflect.TypeOf(AttributeSpec{}).FieldByName("EmptyMeansUnset"); ok {
		t.Error("AttributeSpec.EmptyMeansUnset still exists; revert of 5ec0faa/f9f6dae/dd5f8b1 incomplete")
	}
	files, _ := filepath.Glob("*.go")
	specFiles, _ := filepath.Glob("specs/*.json")
	for _, f := range append(files, specFiles...) {
		if f == "kms_key_id_validation_test.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		low := strings.ToLower(string(b))
		if strings.Contains(low, "emptymeansunset") || strings.Contains(low, "omitemptyunsetfromstate") {
			t.Errorf("%s still references emptyMeansUnset", f)
		}
	}
}
