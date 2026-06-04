package duplocloud

import (
	"fmt"
	"strings"
)

// splitID splits a composite Terraform resource ID of the form "part1/part2[/...]"
// into its constituent parts. Returns an error if fewer than minParts are found.
func splitID(id string, minParts int) ([]string, error) {
	parts := strings.SplitN(id, "/", minParts+1)
	if len(parts) < minParts {
		return nil, fmt.Errorf("invalid resource ID %q: expected at least %d part(s) separated by '/'", id, minParts)
	}
	return parts[:minParts], nil
}

// strPtr returns a pointer to s. Use for optional SDK string fields.
func strPtr(s string) *string { return &s }

// boolPtr returns a pointer to b. Use for optional SDK bool fields.
func boolPtr(b bool) *bool { return &b }

// intPtr returns a pointer to i. Use for optional SDK int fields.
func intPtr(i int) *int { return &i }
