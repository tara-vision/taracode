package main

import (
	"fmt"
	"os"

	"github.com/tara-vision/taracode/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
