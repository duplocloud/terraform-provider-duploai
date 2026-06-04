package duplocloud

import (
	"context"

	"github.com/duplocloud/terraform-provider-duplocloud-helpdesk/duplosdk"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Provider returns the DuplocloudHelpdesk Terraform provider.
func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"duplo_host": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("DUPLO_HOST", nil),
				Description: "Base URL of the DuploCloud AI Helpdesk API (e.g. http://localhost:60021).",
			},
			"duplo_token": {
				Type:        schema.TypeString,
				Required:    true,
				Sensitive:   true,
				DefaultFunc: schema.EnvDefaultFunc("DUPLO_TOKEN", nil),
				Description: "Bearer token for DuploCloud API authentication.",
			},
			"ssl_no_verify": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Disable TLS certificate verification (development only).",
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			// Resources are registered here as they are implemented.
			// Example: "duplocloud_workspace": resourceDuploucloudWorkspace(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			// Data sources are registered here.
		},
		ConfigureContextFunc: providerConfigure,
	}
}

func providerConfigure(_ context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	host, _ := d.GetOk("duplo_host")
	token, _ := d.GetOk("duplo_token")
	sslNoVerify := d.Get("ssl_no_verify").(bool)

	client, err := duplosdk.NewClient(host.(string), token.(string), sslNoVerify)
	if err != nil {
		return nil, diag.FromErr(err)
	}
	return client, nil
}
