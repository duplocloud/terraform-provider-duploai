package main

import (
	"flag"

	"github.com/duplocloud/terraform-provider-duplocloud-helpdesk/duplocloud"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
)

//go:generate terraform fmt -recursive ./examples/
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "enable debugger support (delve)")
	flag.Parse()

	plugin.Serve(&plugin.ServeOpts{
		Debug:        debug,
		ProviderAddr: "registry.terraform.io/duplocloud/duplocloud-helpdesk",
		ProviderFunc: func() *schema.Provider {
			return duplocloud.Provider()
		},
	})
}
