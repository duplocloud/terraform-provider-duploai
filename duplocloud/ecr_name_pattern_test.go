package duplocloud

import (
	"regexp"
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
