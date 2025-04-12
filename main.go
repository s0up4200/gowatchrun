package main

import (
	"os"

	"github.com/s0up4200/gowatchrun/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
