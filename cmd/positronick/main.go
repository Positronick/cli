package main

import (
	"os"

	"github.com/positronick/cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
