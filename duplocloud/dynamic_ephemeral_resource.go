package duplocloud

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	ephschema "github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// dynamicEphemeralResource is the ephemeral twin of a data source spec marked
// `ephemeral: true` (Terraform 1.10+). It reads exactly what the data source
// reads, but Terraform never writes the result to plan or state — the right
// home for short-lived secrets such as k8s_credentials' token. The data source
// stays registered, so existing configs are unaffected.
type dynamicEphemeralResource struct {
	ds *dynamicDataSource
}

var (
	_ ephemeral.EphemeralResource              = &dynamicEphemeralResource{}
	_ ephemeral.EphemeralResourceWithConfigure = &dynamicEphemeralResource{}
)

func newDynamicEphemeralResourceFactory(spec ResourceSpec, endpoint duplosdk.Endpoint) func() ephemeral.EphemeralResource {
	return func() ephemeral.EphemeralResource {
		return &dynamicEphemeralResource{ds: &dynamicDataSource{spec: spec, endpoint: endpoint}}
	}
}

func (e *dynamicEphemeralResource) Metadata(_ context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + e.ds.spec.Name
}

func (e *dynamicEphemeralResource) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*duplosdk.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("expected *duplosdk.Client, got %T", req.ProviderData))
		return
	}
	e.ds.Client = client
}

// Schema is the data source schema, converted attribute by attribute, so the
// two can never drift apart.
func (e *dynamicEphemeralResource) Schema(ctx context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	var dsResp datasource.SchemaResponse
	e.ds.Schema(ctx, datasource.SchemaRequest{}, &dsResp)
	attrs := make(map[string]ephschema.Attribute, len(dsResp.Schema.Attributes))
	for n, a := range dsResp.Schema.Attributes {
		attrs[n] = toEphemeralAttr(a)
	}
	resp.Schema = ephschema.Schema{
		Description: dsResp.Schema.Description,
		Attributes:  attrs,
	}
}

func (e *dynamicEphemeralResource) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	if v, ok := e.ds.read(ctx, req.Config, &resp.Diagnostics); ok {
		resp.Result.Raw = v
	}
}

// toEphemeralAttr converts a data source attribute to the same attribute in
// the ephemeral schema package (identical fields; validators are shared types).
func toEphemeralAttr(a dsschema.Attribute) ephschema.Attribute {
	switch t := a.(type) {
	case dsschema.StringAttribute:
		return ephschema.StringAttribute{Required: t.Required, Optional: t.Optional, Computed: t.Computed, Sensitive: t.Sensitive,
			Description: t.Description, DeprecationMessage: t.DeprecationMessage, Validators: t.Validators}
	case dsschema.BoolAttribute:
		return ephschema.BoolAttribute{Required: t.Required, Optional: t.Optional, Computed: t.Computed, Sensitive: t.Sensitive,
			Description: t.Description, DeprecationMessage: t.DeprecationMessage, Validators: t.Validators}
	case dsschema.Int64Attribute:
		return ephschema.Int64Attribute{Required: t.Required, Optional: t.Optional, Computed: t.Computed, Sensitive: t.Sensitive,
			Description: t.Description, DeprecationMessage: t.DeprecationMessage, Validators: t.Validators}
	case dsschema.Float64Attribute:
		return ephschema.Float64Attribute{Required: t.Required, Optional: t.Optional, Computed: t.Computed, Sensitive: t.Sensitive,
			Description: t.Description, DeprecationMessage: t.DeprecationMessage, Validators: t.Validators}
	case dsschema.ListAttribute:
		return ephschema.ListAttribute{ElementType: t.ElementType, Required: t.Required, Optional: t.Optional, Computed: t.Computed,
			Sensitive: t.Sensitive, Description: t.Description, DeprecationMessage: t.DeprecationMessage, Validators: t.Validators}
	case dsschema.SetAttribute:
		return ephschema.SetAttribute{ElementType: t.ElementType, Required: t.Required, Optional: t.Optional, Computed: t.Computed,
			Sensitive: t.Sensitive, Description: t.Description, DeprecationMessage: t.DeprecationMessage, Validators: t.Validators}
	case dsschema.MapAttribute:
		return ephschema.MapAttribute{ElementType: t.ElementType, Required: t.Required, Optional: t.Optional, Computed: t.Computed,
			Sensitive: t.Sensitive, Description: t.Description, DeprecationMessage: t.DeprecationMessage, Validators: t.Validators}
	case dsschema.SingleNestedAttribute:
		return ephschema.SingleNestedAttribute{Attributes: toEphemeralAttrs(t.Attributes), Required: t.Required, Optional: t.Optional,
			Computed: t.Computed, Sensitive: t.Sensitive, Description: t.Description, DeprecationMessage: t.DeprecationMessage}
	case dsschema.ListNestedAttribute:
		return ephschema.ListNestedAttribute{NestedObject: ephschema.NestedAttributeObject{Attributes: toEphemeralAttrs(t.NestedObject.Attributes)},
			Required: t.Required, Optional: t.Optional, Computed: t.Computed, Sensitive: t.Sensitive, Description: t.Description, DeprecationMessage: t.DeprecationMessage}
	case dsschema.SetNestedAttribute:
		return ephschema.SetNestedAttribute{NestedObject: ephschema.NestedAttributeObject{Attributes: toEphemeralAttrs(t.NestedObject.Attributes)},
			Required: t.Required, Optional: t.Optional, Computed: t.Computed, Sensitive: t.Sensitive, Description: t.Description, DeprecationMessage: t.DeprecationMessage}
	case dsschema.MapNestedAttribute:
		return ephschema.MapNestedAttribute{NestedObject: ephschema.NestedAttributeObject{Attributes: toEphemeralAttrs(t.NestedObject.Attributes)},
			Required: t.Required, Optional: t.Optional, Computed: t.Computed, Sensitive: t.Sensitive, Description: t.Description, DeprecationMessage: t.DeprecationMessage}
	}
	panic(fmt.Sprintf("toEphemeralAttr: unsupported data source attribute %T", a))
}

func toEphemeralAttrs(in map[string]dsschema.Attribute) map[string]ephschema.Attribute {
	out := make(map[string]ephschema.Attribute, len(in))
	for n, a := range in {
		out[n] = toEphemeralAttr(a)
	}
	return out
}
