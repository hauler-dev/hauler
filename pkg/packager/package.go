package packager

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/mholt/archiver/v3"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
	"time"
)

const (
	k3sReleases = "https://github.com/k3s-io/k3s/releases/download"
	k3sVersion = "v1.21.0-rc1%2Bk3s1"
	k3sImages = "k3s-airgap-images-amd64.tar.zst"
	k3sBinary = "k3s"
)

type Packager struct {
	Cluster *v1alpha1.Cluster
	logger *logrus.Entry
	za *archiver.TarZstd
}

func NewPackager(cluster *v1alpha1.Cluster) *Packager {
	return &Packager{
		Cluster: cluster,
		logger: logrus.WithFields(logrus.Fields{
			"cluster": cluster.Metadata.Name,
			"driver": cluster.Driver.String(),
		}),
		za: &archiver.TarZstd{
			Tar: &archiver.Tar{
				OverwriteExisting:      true,
				MkdirAll:               true,
			},
		},
	}
}

func (p *Packager) Package(ctx context.Context, archivePath string) error {
	// TODO: real timeout
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	p.logger.Debugf("created temporary working directory for pacakging: %s", tmpdir)

	p.prepFS(tmpdir)

	p.logger.Infof("writing cluster configuration")
	err = viper.WriteConfigAs(filepath.Join(tmpdir, "cluster.yaml"))
	if err != nil {
		return err
	}

	// Fetch driver executable
	executableDest := filepath.Join(tmpdir, v1alpha1.HaulerBin)
	p.logger.Infof("fetching driver executable and saving to: %s", executableDest)
	if err := getArtfiact(ctx, p.Cluster.Driver.GetBinaryURL(), executableDest); err != nil {
		return err
	}

	// Fetch driver preloaded images
	preloadDriverImagesDest := filepath.Join(tmpdir, v1alpha1.ImagePreloadPath(p.Cluster.Driver))
	p.logger.Infof("fetching driver preload images and saving to: %s", preloadDriverImagesDest)
	if err := getArtfiact(ctx, p.Cluster.Driver.GetPreloadImages(), preloadDriverImagesDest); err != nil {
		return err
	}

	// Fetch images to preload on boot
	preloadImagesDest := filepath.Join(tmpdir, v1alpha1.ImagePreloadPath(p.Cluster.Driver), "hauler.tar")
	p.logger.Infof("fetching all cluster defined images to preload and saving to: %s", preloadImagesDest)
	if err := p.preloadImages(preloadImagesDest); err != nil {
		return err
	}

	// Chdirs to ensure folder structure doesn't have parent dir
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	err = os.Chdir(tmpdir)
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	p.logger.Infof("tarring and compressing %s", tmpdir)
	err = p.za.Archive([]string{"."}, filepath.Join(cwd, archivePath))
	if err != nil {
		return fmt.Errorf("failed creating archive %s: %v", filepath.Join(cwd, archivePath), err)
	}

	return nil
}

//TODO: factor this out of Packager
func getArtfiact(ctx context.Context, src, dst string) error {
	f := NewFile(src)
	_, err := f.Get(ctx, dst)
	if err != nil {
		return err
	}
	return nil
}

func (p *Packager) preloadImages(dest string) error {
	imageRefs := make(map[name.Reference]v1.Image)
	for _, imageName := range p.Cluster.PreloadImages {
		ref, err := name.ParseReference(string(imageName))
		if err != nil {
			return err
		}

		p.logger.Debugf("adding %s to preload image archive", ref.Name())
		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}

		imageRefs[ref] = img
	}

	//	Get preload images and save to a single compressed tarball
	if err := tarball.MultiRefWriteToFile(dest, imageRefs); err != nil {
		return err
	}

	// TODO: Pull with progress
	//c := make(chan v1.Update, 200)
	//go func() {
	//	_ = tarball.MultiRefWriteToFile(dest, imageRefs, tarball.WithProgress(c))
	//}()
	//for update := range c {
	//	switch {
	//	case update.Error != nil && update.Error == io.EOF:
	//		fmt.Printf("%d/%d", update.Complete, update.Total)
	//		return nil
	//	case update.Error != nil:
	//		fmt.Printf("error writing tarball: %v\n", update.Error)
	//		return nil
	//	default:
	//		fmt.Fprintf(os.Stderr, "receive update: %v\n", update)
	//	}
	//
	//}

	return nil
}

func (p *Packager) prepFS(dir string) {
	for _, fs := range v1alpha1.FS(p.Cluster.Driver) {
		path := filepath.Join(dir, fs)

		p.logger.Debugf("creating directory: %s", path)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			p.logger.Fatalf("failed to create directory %s: %v", path, err)
		}
	}
}
