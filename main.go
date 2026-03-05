// Package main is the entry point for the Icotera i4850 Terraform provider.
package main

import (
	"context"
	"log"

	"github.com/francis-fisher/terraform-provider-icotera-i4850/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var (
	version string = "dev"
)

func main() {
	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/francis-fisher/icotera-i4850",
	}

	err := providerserver.Serve(context.Background(), provider.New, opts)

	if err != nil {
		log.Fatal(err.Error())
	}
}

//go:generate go run -modfile tools/go.mod github.com/golangci/golangci-lint/cmd/golangci-lint run ./...
