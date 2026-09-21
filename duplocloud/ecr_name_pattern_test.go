package duplocloud

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// ecrAttrPattern returns the compiled pattern the shipped ecr spec puts on an
// attribute, failing the test when the attribute or its pattern is missing.
func ecrAttrPattern(t *testing.T, attr string) *regexp.Regexp {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, spec := range specs {
		if spec.Name != "ecr" {
			continue
		}
		for _, a := range spec.Attributes {
			if a.Name != attr {
				continue
			}
			if a.Pattern == "" {
				t.Fatalf("ecr.%s has no pattern — a bad value would only fail mid-apply, from the API", attr)
			}
			if a.PatternDescription == "" {
				t.Fatalf("ecr.%s has a pattern with no patternDescription — the plan error would not say what is wrong", attr)
			}
			re, err := regexp.Compile(a.Pattern)
			if err != nil {
				t.Fatalf("ecr.%s pattern does not compile: %v", attr, err)
			}
			return re
		}
		t.Fatalf("ecr spec has no %q attribute", attr)
	}
	t.Fatal("ecr spec not found")
	return nil
}

// The reported failure: name = "cbi/tf-igrc" reached the API and came back as
// "AwsEcr name 'cbi/tf-igrc' is invalid. Use alphanumeric characters and hyphens
// only…". The platform record name is validated as a DNS label for every
// resource type (ResourceServiceBase.ValidateNameFormat), and ECR does not
// override it — so the pattern here must mirror that rule exactly and reject the
// value at plan time instead.
func TestEcrNameRejectsPathSeparators(t *testing.T) {
	re := ecrAttrPattern(t, "name")

	rejected := []string{
		"cbi/tf-igrc", // the reported value
		"cbi_tf_igrc", // underscores are not in the backend's rule
		"cbi.tf.igrc", // nor dots
		"-cbi",        // leading hyphen
		"cbi-",        // trailing hyphen
		"cbi/",        // trailing slash
		"/cbi",        // leading slash
		"cbi//tf",     // consecutive slashes
		"cbi tf",      // space
	}
	for _, s := range rejected {
		if re.MatchString(s) {
			t.Errorf("name %q must be rejected at plan time — the API rejects it", s)
		}
	}

	accepted := []string{
		"cbi-tf-igrc", // the value to use alongside repository_name = "cbi/tf-igrc"
		"backend",
		"a",
		"a1",
		"Backend9",
	}
	for _, s := range accepted {
		if !re.MatchString(s) {
			t.Errorf("name %q is accepted by the API and must not be rejected here", s)
		}
	}
}

// repository_name is the AWS ECR name and legitimately carries a namespace path
// — it is the field the reported value belongs in, so a slash must pass here
// even though it is rejected on name.
func TestEcrRepositoryNameAllowsNamespacePaths(t *testing.T) {
	re := ecrAttrPattern(t, "repository_name")

	accepted := []string{
		"cbi/tf-igrc", // the reported value, in the field that takes it
		"team/backend",
		"team/sub/backend",
		"backend",
		"my-app.v2_final",
	}
	for _, s := range accepted {
		if !re.MatchString(s) {
			t.Errorf("repository_name %q is a valid ECR name and must not be rejected", s)
		}
	}

	rejected := []string{
		"Cbi/Tf",  // ECR names are lowercase only
		"cbi//tf", // consecutive slashes
		"/cbi",    // leading slash
		"cbi/",    // trailing slash
		"-cbi",    // leading separator
		"cbi-",    // trailing separator
		"cbi tf",  // space
	}
	for _, s := range rejected {
		if re.MatchString(s) {
			t.Errorf("repository_name %q is not a valid ECR name and must fail at plan time", s)
		}
	}
}

// The two patterns must disagree about the slash — that disagreement is the
// whole point, and is what makes the plan error able to say "put it in the other
// field".
func TestEcrNamePatternsDisagreeOnSlash(t *testing.T) {
	const reported = "cbi/tf-igrc"
	if ecrAttrPattern(t, "name").MatchString(reported) {
		t.Errorf("name accepts %q, so the API would still reject it mid-apply", reported)
	}
	if !ecrAttrPattern(t, "repository_name").MatchString(reported) {
		t.Errorf("repository_name rejects %q, but that is the field it belongs in", reported)
	}
}

// AWS requires at least 2 characters in a repository name, which the pattern
// cannot express: RE2 has no lookahead, so "2 or more overall" across
// optionally-repeating path segments would mean duplicating every alternative.
// minLength carries it instead, so a one-character name fails at plan rather
// than at AWS.
func TestEcrRepositoryNameLengthBounds(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, spec := range specs {
		if spec.Name != "ecr" {
			continue
		}
		for _, a := range spec.Attributes {
			if a.Name != "repository_name" {
				continue
			}
			if a.MinLength != 2 {
				t.Errorf("repository_name minLength = %d, want 2 (AWS's floor)", a.MinLength)
			}
			if a.MaxLength != 256 {
				t.Errorf("repository_name maxLength = %d, want 256 (AWS's ceiling)", a.MaxLength)
			}
			// The pattern alone would let a single character through — that is
			// exactly why the bound is declared separately.
			if !regexp.MustCompile(a.Pattern).MatchString("a") {
				t.Error("pattern unexpectedly rejects a 1-character name; minLength is then redundant")
			}
			return
		}
		t.Fatal("ecr spec has no repository_name attribute")
	}
	t.Fatal("ecr spec not found")
}

// minLength is a string-only constraint, must be positive, and cannot exceed
// maxLength — a spec asking for both would accept nothing.
func TestMinLengthSpecValidation(t *testing.T) {
	cases := []struct {
		name    string
		attr    string
		wantErr string
	}{
		{
			name:    "string with a sane bound is accepted",
			attr:    `{"name":"repo","type":"string","required":true,"apiPath":"repo","minLength":2,"maxLength":256}`,
			wantErr: "",
		},
		{
			name:    "non-string is rejected",
			attr:    `{"name":"count","type":"int","required":true,"apiPath":"count","minLength":2}`,
			wantErr: "only valid on a string",
		},
		{
			name:    "negative is rejected",
			attr:    `{"name":"repo","type":"string","required":true,"apiPath":"repo","minLength":-1}`,
			wantErr: "minLength must be positive",
		},
		{
			name:    "min above max is rejected",
			attr:    `{"name":"repo","type":"string","required":true,"apiPath":"repo","minLength":10,"maxLength":4}`,
			wantErr: "exceeds maxLength",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{
			  "name": "min_len_probe",
			  "description": "probe",
			  "idPath": "id",
			  "endpoint": {"uriBase": "/v3/admin/things"},
			  "attributes": [` + tc.attr + `]
			}`)
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
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// A negative maxLength on its own used to slip through: the gate guarding the
// "must be positive" check required a positive value, so the check could only
// fire when some other constraint happened to be set. Pins both bounds.
func TestNegativeLengthBoundsAreRejectedAlone(t *testing.T) {
	for _, attr := range []struct{ name, json, want string }{
		{"maxLength", `{"name":"repo","type":"string","required":true,"apiPath":"repo","maxLength":-5}`, "maxLength must be positive"},
		{"minLength", `{"name":"repo","type":"string","required":true,"apiPath":"repo","minLength":-5}`, "minLength must be positive"},
	} {
		t.Run(attr.name, func(t *testing.T) {
			raw := []byte(`{
			  "name": "neg_bound_probe",
			  "description": "probe",
			  "idPath": "id",
			  "endpoint": {"uriBase": "/v3/admin/things"},
			  "attributes": [` + attr.json + `]
			}`)
			var spec ResourceSpec
			if err := json.Unmarshal(raw, &spec); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := spec.validate()
			if err == nil {
				t.Fatalf("a negative %s alone must be rejected at spec load", attr.name)
			}
			if !strings.Contains(err.Error(), attr.want) {
				t.Fatalf("expected error containing %q, got %v", attr.want, err)
			}
		})
	}
}
