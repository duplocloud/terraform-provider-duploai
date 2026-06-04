package duplocloud

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// validateNoWhitespace rejects strings that contain whitespace.
func validateNoWhitespace(v interface{}, k string) (warns []string, errs []error) {
	val, ok := v.(string)
	if !ok {
		errs = append(errs, fmt.Errorf("%q must be a string", k))
		return
	}
	for _, c := range val {
		if c == ' ' || c == '\t' || c == '\n' {
			errs = append(errs, fmt.Errorf("%q must not contain whitespace", k))
			return
		}
	}
	return
}

// suppressEquivalentJSON suppresses diffs when two JSON strings are semantically equal.
func suppressEquivalentJSON(_, old, new string, _ *schema.ResourceData) bool {
	return old == new // extend with json.Unmarshal comparison if needed
}
