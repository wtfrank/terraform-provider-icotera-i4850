package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/wtfrank/terraform-provider-icotera-i4850/internal/provider"
)

func main() {
	opts := providerserver.ServeOpts{
		Address: "hashicorp.com/wtfrank/icotera_i4850",
	}

	err := providerserver.Serve(context.Background(), provider.New, opts)

	if err != nil {
		log.Fatal(err.Error())
	}
}
