package main

import (
	"log"

	"github.com/rancherfederal/hauler/cmd/haulerctl/app"
)

func main() {
	root := app.NewRootCommand()

	if err := root.Execute(); err != nil {
		log.Fatalln(err)
	}
}
