package main

import (
	"os"

	"github.com/vrotherford/cicdez/internal/cmd"
)

func main() {
	if err := cmd.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
