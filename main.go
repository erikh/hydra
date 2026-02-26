package main

import (
	"fmt"
	"os"

	"github.com/erikh/hydra/cmd"
)

func main() {
	app := cmd.NewApp()
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
