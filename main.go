package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/duplocloud/terraform-provider-duploai/duplocloud"
)

//go:generate terraform fmt -recursive ./examples/
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name duploai
//go:generate go run ./tools/gen_readme

// version is set at release time by goreleaser (-X main.version=...);
// local builds report "dev".
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "enable debugger support (delve)")
	flag.Parse()

	err := providerserver.Serve(context.Background(), duplocloud.NewVersioned(version), providerserver.ServeOpts{
		Debug:   debug,
		Address: "registry.terraform.io/duplocloud/duploai",
	})
	if err != nil {
		log.Fatal(err)
	}
}
