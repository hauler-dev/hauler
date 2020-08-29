package main

import (
	"log"

	"github.com/rancherfederal/k3ama/cmd/k3ama/app"
)

func main() {
	root := app.NewRootCommand()

	if err := root.Execute(); err != nil {
		log.Fatalln(err)
	}
}
