package main

import (
	"context"
	"flag"
	"log"

	"github.com/duplocloud/terraform-provider-duplocloud-helpdesk/duplocloud"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

//go:generate terraform fmt -recursive ./examples/
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "enable debugger support (delve)")
	flag.Parse()

	err := providerserver.Serve(context.Background(), duplocloud.New, providerserver.ServeOpts{
		Debug:   debug,
		Address: "registry.terraform.io/duplocloud/duplocloud-helpdesk",
	})
	if err != nil {
		log.Fatal(err)
	}
}
