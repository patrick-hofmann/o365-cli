package main

import (
	"os"

	"github.com/patrick-hofmann/o365-cli/v2/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
