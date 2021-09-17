package app

import (
	"github.com/spf13/cobra"
)

type imageServeOpts struct {
	port int
	path string
}

func NewRegistryServeCommand() *cobra.Command {
	opts := imageServeOpts{}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "serve the oci pull copmliant registry from the local registry store",
		Run: func(cmd *cobra.Command, args []string) {
			opts.Run()
		},
	}

	f := cmd.Flags()
	f.IntVarP(&opts.port, "port", "p", 5000, "port to expose on")
	f.StringVarP(&opts.path, "store", "s", "hauler", "path to image store contents")

	return cmd
}

func (o imageServeOpts) Run() {
	// ctx, _ := context.WithCancel(context.Background())
	//
	// cfg := registry.DefaultConfiguration(o.path, ":3333")
	// r, err := registry.NewRegistry(ctx, cfg)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	//
	// err = r.Start(ctx)
	// if err != nil {
	// 	panic(err)
	// }
	//
	// alp, _ := name.ParseReference("alpine:latest")
	// img, _ := remote.Image(alp)
	//
	// // resp, err := http.Get("http://localhost:3333/v2/")
	// // if err != nil {
	// // 	panic(err)
	// // }
	// // defer resp.Body.Close()
	// //
	// // p, err := io.ReadAll(resp.Body)
	// // fmt.Println(string(p))
	//
	// local, _ := name.ParseReference("localhost:3333/alpine:latest")
	// if err := remote.Write(local, img); err != nil {
	// 	logrus.Fatal(err)
	// }
	//
	// ref := "localhost:3333/oras:test"
	//
	// resolver := docker.NewResolver(docker.ResolverOptions{})
	//
	// memoryStore := content.NewMemoryStore()
	// desc := memoryStore.Add("hello.txt", "my.custom.media.type", []byte("Hello World!\n"))
	// pushContents := []ocispec.Descriptor{desc}
	// desc, err = oras.Push(ctx, resolver, ref, memoryStore, pushContents)
	// if err != nil {
	// 	logrus.Fatal(err)
	// }
	// fmt.Println("pushed: ", desc)
	//
	// fmt.Println("main done")
}
