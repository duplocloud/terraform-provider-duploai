package duplocloud

import (
	"context"

	"github.com/duplocloud/terraform-provider-duplocloud-helpdesk/duplosdk"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &helpdeskProvider{}

type helpdeskProvider struct{}

// New returns a new instance of the DuploCloud Helpdesk provider.
func New() provider.Provider {
	return &helpdeskProvider{}
}

func (p *helpdeskProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "duplocloud"
}

func (p *helpdeskProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"duplo_host": schema.StringAttribute{
				Required:    true,
				Description: "Base URL of the DuploCloud AI Helpdesk API (e.g. http://localhost:60021).",
			},
			"duplo_token": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				Description: "Bearer token for DuploCloud API authentication.",
			},
			"ssl_no_verify": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable TLS certificate verification (development only).",
			},
		},
	}
}

type providerModel struct {
	DuploHost   types.String `tfsdk:"duplo_host"`
	DuploToken  types.String `tfsdk:"duplo_token"`
	SSLNoVerify types.Bool   `tfsdk:"ssl_no_verify"`
}

func (p *helpdeskProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client, err := duplosdk.NewClient(
		config.DuploHost.ValueString(),
		config.DuploToken.ValueString(),
		config.SSLNoVerify.ValueBool(),
	)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create client", err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *helpdeskProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

func (p *helpdeskProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
