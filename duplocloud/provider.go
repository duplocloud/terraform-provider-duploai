package duplocloud

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
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
				Optional:    true,
				Description: "Base URL of the DuploCloud AI API (e.g. http://localhost:60021). Required unless set via the DUPLO_HOST or duplo_host env var.",
			},
			"duplo_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Bearer token for DuploCloud API authentication. Required unless set via the DUPLO_TOKEN or duplo_token env var.",
			},
			"ssl_no_verify": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable TLS certificate verification (development only). May also be set via the SSL_NO_VERIFY or ssl_no_verify env var. Accepted values: 1/t/T/TRUE/true/True/0/f/F/FALSE/false/False.",
			},
			"http_timeout": schema.Int64Attribute{
				Optional:    true,
				Description: "HTTP client timeout in seconds for DuploCloud API calls (default 60). May also be set via the HTTP_TIMEOUT or http_timeout env var (integer seconds only).",
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

	if config.DuploHost.IsNull() || config.DuploHost.ValueString() == "" {
		config.DuploHost = types.StringValue(envFallback("DUPLO_HOST", "duplo_host"))
	}
	if config.DuploToken.IsNull() || config.DuploToken.ValueString() == "" {
		config.DuploToken = types.StringValue(envFallback("DUPLO_TOKEN", "duplo_token"))
	}
	if config.SSLNoVerify.IsNull() {
		if v := envFallback("SSL_NO_VERIFY", "ssl_no_verify"); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				resp.Diagnostics.AddAttributeError(
					path.Root("ssl_no_verify"),
					"Invalid SSL_NO_VERIFY value",
					fmt.Sprintf("Cannot parse %q as a boolean. Accepted values: 1/t/T/TRUE/true/True/0/f/F/FALSE/false/False.", v),
				)
			} else {
				config.SSLNoVerify = types.BoolValue(b)
			}
		}
	}
	if config.HTTPTimeout.IsNull() {
		if v := envFallback("HTTP_TIMEOUT", "http_timeout"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				resp.Diagnostics.AddAttributeError(
					path.Root("http_timeout"),
					"Invalid HTTP_TIMEOUT value",
					fmt.Sprintf("Cannot parse %q as an integer number of seconds.", v),
				)
			} else {
				config.HTTPTimeout = types.Int64Value(n)
			}
		}
	}

	if config.DuploHost.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("duplo_host"),
			"Missing duplo_host",
			"Set duplo_host in the provider block or the DUPLO_HOST (or duplo_host) env var.",
		)
	}
	if config.DuploToken.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("duplo_token"),
			"Missing duplo_token",
			"Set duplo_token in the provider block or the DUPLO_TOKEN (or duplo_token) env var.",
		)
	}
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

func envFallback(upper, lower string) string {
	if v := os.Getenv(upper); v != "" {
		return v
	}
	return os.Getenv(lower)
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
		if spec.DataSourceOnly {
			continue
		}
		endpoint, buildErr := spec.BuildEndpoint()
		if buildErr != nil {
			panic(fmt.Sprintf("resource %q: %s", spec.Name, buildErr))
		}
		if err := spec.checkPathParams(endpoint.PathParams()); err != nil {
			panic(fmt.Sprintf("resource %q: %s", spec.Name, err))
		}
		factories = append(factories, newDynamicResourceFactory(spec, endpoint))
	}
	return factories
}

func (p *duploaiProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	specs, err := loadResourceSpecs()
	if err != nil {
		panic(fmt.Sprintf("loading specs for data sources: %s", err))
	}
	var factories []func() datasource.DataSource
	for _, spec := range specs {
		if !spec.DataSource && !spec.DataSourceOnly {
			continue
		}
		endpoint, buildErr := spec.BuildEndpoint()
		if buildErr != nil {
			panic(fmt.Sprintf("data source %q: %s", spec.Name, buildErr))
		}
		if err := spec.checkPathParams(endpoint.PathParams()); err != nil {
			panic(fmt.Sprintf("data source %q: %s", spec.Name, err))
		}
		factories = append(factories, newDynamicDataSourceFactory(spec, endpoint))
	}
	return factories
}
