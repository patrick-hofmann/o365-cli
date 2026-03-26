package main

import (
	"os"

	"github.com/yourname/o365-cli/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
