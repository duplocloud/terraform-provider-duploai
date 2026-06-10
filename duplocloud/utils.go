package duplocloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

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

// baseDataSource holds the shared client and satisfies datasource.DataSourceWithConfigure.
// Embed this in every data source struct instead of repeating Configure each time.
type baseDataSource struct {
	*duplosdk.Client
}

func (d *baseDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*duplosdk.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("expected *duplosdk.Client, got %T", req.ProviderData))
		return
	}
	d.Client = client
}
