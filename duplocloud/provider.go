package duplocloud

import (
	"context"
	"fmt"
	"time"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &duploaiProvider{}

type duploaiProvider struct{}

// New returns a new instance of the DuploCloud AI provider.
func New() provider.Provider {
	return &duploaiProvider{}
}

func (p *duploaiProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "duploai"
}

func (p *duploaiProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"duplo_host": schema.StringAttribute{
				Required:    true,
				Description: "Base URL of the DuploCloud AI API (e.g. http://localhost:60021).",
			},
			"duplo_token": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Bearer token for DuploCloud API authentication.",
			},
			"ssl_no_verify": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable TLS certificate verification (development only).",
			},
			"http_timeout": schema.Int64Attribute{
				Optional:    true,
				Description: "HTTP client timeout in seconds for DuploCloud API calls (default 60).",
			},
		},
	}
}

type providerModel struct {
	DuploHost   types.String `tfsdk:"duplo_host"`
	DuploToken  types.String `tfsdk:"duplo_token"`
	SSLNoVerify types.Bool   `tfsdk:"ssl_no_verify"`
	HTTPTimeout types.Int64  `tfsdk:"http_timeout"`
}

func (p *duploaiProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var timeout time.Duration
	if !config.HTTPTimeout.IsNull() {
		timeout = time.Duration(config.HTTPTimeout.ValueInt64()) * time.Second
	}

	client, err := duplosdk.NewClient(
		config.DuploHost.ValueString(),
		config.DuploToken.ValueString(),
		config.SSLNoVerify.ValueBool(),
		timeout,
	)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create client", err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *duploaiProvider) Resources(_ context.Context) []func() resource.Resource {
	specs, err := loadResourceSpecs()
	if err != nil {
		// Embedded specs are compiled in; a failure here is a developer error
		// that should surface loudly at startup rather than silently drop a
		// resource.
		panic(fmt.Sprintf("loading resource specs: %s", err))
	}
	factories := make([]func() resource.Resource, 0, len(specs))
	for _, spec := range specs {
		endpoint, ok := duplosdk.LookupEndpoint(spec.Name)
		if !ok {
			panic(fmt.Sprintf("no API endpoint registered for resource %q (add it to the duplosdk package)", spec.Name))
		}
		if err := spec.checkPathParams(endpoint.PathParams()); err != nil {
			panic(fmt.Sprintf("resource %q: %s", spec.Name, err))
		}
		factories = append(factories, newDynamicResourceFactory(spec, endpoint))
	}
	return factories
}

func (p *duploaiProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
