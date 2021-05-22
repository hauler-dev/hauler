package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

type relocateImagesOpts struct {
	relocateOpts
}

func NewRelocateImagesCommand() *cobra.Command {
	opts := &relocateImagesOpts{}

	cmd := &cobra.Command{
		Use: "images",
		Short: "relocate images",
		Long: "",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	return cmd
}

func (o *relocateImagesOpts) Run() error {
	//TODO
	fmt.Println("relocate images")
	return nil
}