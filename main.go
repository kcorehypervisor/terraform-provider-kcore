package main

import (
	"flag"

	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	"github.com/kcorehypervisor/terraform-provider-kcore/internal/provider"
)

// Set by GoReleaser via -ldflags -X.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	var debugMode bool

	flag.BoolVar(&debugMode, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := &plugin.ServeOpts{
		ProviderFunc: provider.New,
		Debug:        debugMode,
		ProviderAddr: "registry.terraform.io/kcorehypervisor/kcore",
	}

	plugin.Serve(opts)
}
