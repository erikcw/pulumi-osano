// Package main is the Pulumi Osano provider plugin entrypoint.
package main

import (
	"context"
	"fmt"
	"os"

	provider "github.com/jflavan/pulumi-osano/provider"
)

func main() {
	if err := provider.Provider().Run(context.Background(), provider.Name, provider.Version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
