package main

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/app"
)

func main() {
	root := app.NewRootCommand()
	cobra.CheckErr(root.Execute())
}
