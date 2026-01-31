package main

import (
	"os"

	"github.com/tara-vision/taracode/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Cobra already prints the error, just exit with non-zero status
		os.Exit(1)
	}
}
