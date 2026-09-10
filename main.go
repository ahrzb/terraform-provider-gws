// terraform-provider-gws manages Google Workspace resources declaratively.
//
// Today: Gmail filters and labels. The naming (`gws_<service>_<thing>`) leaves room for the
// rest of the Workspace surface without renaming anything later.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/ahrzb/terraform-provider-gws/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		// Must match the `source` in required_providers, and the provider-source-address the
		// Nix package advertises.
		Address: "registry.opentofu.org/ahrzb/gws",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
