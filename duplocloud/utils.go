package duplocloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// listToStrings extracts plain string values from a types.List of strings.
func listToStrings(l types.List) []string {
	elems := l.Elements()
	out := make([]string, 0, len(elems))
	for _, e := range elems {
		if s, ok := e.(types.String); ok {
			out = append(out, s.ValueString())
		}
	}
	return out
}

// stringsToList converts a []string into a types.List of strings.
func stringsToList(ss []string) types.List {
	elems := make([]attr.Value, 0, len(ss))
	for _, s := range ss {
		elems = append(elems, types.StringValue(s))
	}
	list, _ := types.ListValue(types.StringType, elems)
	return list
}

// baseResource holds the shared client and satisfies resource.ResourceWithConfigure.
// Embed this in every resource struct instead of repeating Configure each time.
type baseResource struct {
	*duplosdk.Client
}

func (r *baseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*duplosdk.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("expected *duplosdk.Client, got %T", req.ProviderData))
		return
	}
	r.Client = client
}

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
