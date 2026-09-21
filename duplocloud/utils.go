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

// ── Collection reads ─────────────────────────────────────────────────────────

// specCollectionElement is the one place a spec's collection-read settings are
// turned into a read. Both the resource and the data source go through it, so
// neither can honor ReadListPath while the other quietly ignores it — which is
// exactly what happened to the security-group-rule data sources, whose response
// wraps its elements ({"ownSecurityGroupId":…,"rules":[…]}) and so failed to
// decode as a bare array on every plan.
func specCollectionElement(spec *ResourceSpec, api *duplosdk.RESTResource[map[string]any], objID string) (*map[string]any, duplosdk.ClientError) {
	return readCollectionElementAt(api, spec.Endpoint.ReadListPath, spec.IDPath, objID)
}

// readCollectionElementAt GETs a collection and returns the element whose
// idPath value equals objID, or nil when the collection does not hold it. A
// non-empty listPath means the elements are nested under that path inside an
// enveloping object rather than being the response itself (see
// EndpointSpec.ReadListPath); an empty listPath reads a bare array.
func readCollectionElementAt(api *duplosdk.RESTResource[map[string]any], listPath, idPath, objID string) (*map[string]any, duplosdk.ClientError) {
	var items []map[string]any
	var clientErr duplosdk.ClientError
	if listPath == "" {
		items, clientErr = api.GetCollection()
	} else {
		var envelope map[string]any
		envelope, clientErr = api.GetCollectionEnvelope()
		if clientErr == nil {
			for _, e := range toAnySlice(extractPath(envelope, strings.Split(listPath, "."))) {
				if m, ok := e.(map[string]any); ok {
					items = append(items, m)
				}
			}
		}
	}
	if clientErr != nil {
		return nil, clientErr
	}
	idSegs := strings.Split(idPath, ".")
	for i := range items {
		if fmt.Sprint(extractPath(items[i], idSegs)) == objID {
			return &items[i], nil
		}
	}
	return nil, nil
}
