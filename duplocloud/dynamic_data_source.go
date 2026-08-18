package duplocloud

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

var (
	_ datasource.DataSource              = &dynamicDataSource{}
	_ datasource.DataSourceWithConfigure = &dynamicDataSource{}
)

// dsReadRetryWindow bounds how long a data source read retries transient
// failures — read-after-create lag (the backend may not return a just-created
// object immediately), throttling, and server errors — before reporting the
// last error. The flip side: a lookup of a genuinely nonexistent id takes this
// long to fail.
const dsReadRetryWindow = 60 * time.Second

// dynamicDataSource is the generic Read-only engine for spec-driven data sources.
// One instance is created per spec that has DataSource:true in specs/.
// Schema, Read, and state-building all delegate to shared engine functions so
// no logic is duplicated between the resource and data source paths.
type dynamicDataSource struct {
	baseDataSource
	spec     ResourceSpec
	endpoint duplosdk.Endpoint
}

func newDynamicDataSourceFactory(spec ResourceSpec, endpoint duplosdk.Endpoint) func() datasource.DataSource {
	return func() datasource.DataSource {
		return &dynamicDataSource{spec: spec, endpoint: endpoint}
	}
}

func (d *dynamicDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.spec.Name
}

// Schema derives the data source schema from the resource spec automatically:
//   - Path-parameter attributes (e.g. workspace_id) → Required
//   - All other readable attributes                  → Computed
//   - Write-only attributes (no apiPath/responsePath) → excluded
//
// The engine-injected "id" attribute is Required: the user supplies the object
// ID and the engine uses it to call GET /{id}.
func (d *dynamicDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	pathParamSet := make(map[string]bool)
	for _, p := range d.endpoint.PathParams() {
		pathParamSet[p] = true
	}

	attrs := map[string]dsschema.Attribute{
		d.spec.lookupName(): dsschema.StringAttribute{
			Required:    true,
			Description: d.spec.lookupDescription(),
		},
	}

	for _, a := range d.spec.Attributes {
		isPathParam := pathParamSet[a.Name]

		// Exclude write-only attributes that the API never returns in responses.
		// These have no apiPath and no responsePath (only createPath/updatePath).
		// responsePathList() also covers ResponsePaths (cloud-agnostic fallbacks).
		if !isPathParam && len(a.responsePathList()) == 0 {
			continue
		}

		// Adjust flags for data source context:
		//   path params   → Required (user must supply to scope the API call)
		//   everything else → Computed (value comes entirely from the API response)
		adj := a
		if isPathParam {
			adj.Required = true
			adj.Optional = false
			adj.Computed = false
		} else {
			adj.Required = false
			adj.Optional = false
			adj.Computed = true
		}
		attrs[a.Name] = dsAttrSchema(adj)
	}

	resp.Schema = dsschema.Schema{
		Description: d.spec.Description,
		Attributes:  attrs,
	}
}

func (d *dynamicDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// readPathParamScope accepts attrReader; tfsdk.Config satisfies the interface.
	scope, _ := readPathParamScope(ctx, d.endpoint, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var idVal types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(d.spec.lookupName()), &idVal)...)
	if resp.Diagnostics.HasError() {
		return
	}
	objID := idVal.ValueString()
	log.Printf("[TRACE] dynamic datasource %s Read(%s): start", d.spec.Name, objID)

	// A resource's Terraform ID is composite ("scope.../object's id", see composeID)
	// so users can chain `id = duploai_x.y.id` directly; the API itself takes only
	// the trailing object id. State keeps the id exactly as configured — Terraform
	// rejects a data source result whose config-set attributes change.
	apiID := objID
	if i := strings.LastIndex(apiID, "/"); i >= 0 {
		apiID = apiID[i+1:]
	}

	api := duplosdk.NewRESTResource[map[string]any](d.Client, d.endpoint, scope, nil)
	obj, clientErr := api.GetWithRetry(ctx, apiID, dsReadRetryWindow)
	if clientErr != nil {
		if clientErr.IsNotFound() {
			resp.Diagnostics.AddError("Object not found",
				d.spec.Name+" with id "+objID+" does not exist.")
			return
		}
		resp.Diagnostics.AddError("Error reading "+d.spec.Name, clientErr.Error())
		return
	}

	// refreshInputs=true: always populate all readable attributes from the response.
	// buildStateRaw skips write-only attrs (responsePath=="") and attrs absent from
	// the data source schema type (hasType guard), so the full spec.Attributes list
	// is safe to pass even though the schema exposes only a subset.
	// applyPreserveSplit=false: a data source has no user-managed set to split
	// against, so a PreserveUnmanagedInto attribute reads back the full list.
	state := buildStateRaw(d.spec.Attributes, req.Config.Raw, *obj, scope, objID, true, false, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.Raw = state
	log.Printf("[TRACE] dynamic datasource %s Read(%s): end", d.spec.Name, objID)
}
