package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/spf13/cobra"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/audit"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/retry"
)

func addCopy(parent *cobra.Command, ro *flags.CliRootOpts) {
	o := &flags.ImageCopyOpts{}

	cmd := &cobra.Command{
		Use:     "copy SRC DST",
		Aliases: []string{"cp"},
		Short:   "(EXPERIMENTAL) Copy an artifact between registries",
		Example: `  # copy an image to another registry
  hauler copy busybox:latest registry.example.com/busybox:latest

  # copy a specific platform out of a multi-arch image
  hauler copy ghcr.io/hauler-dev/hauler-debug:v2.0.3 registry.example.com/hauler-debug:v2.0.3 --platform linux/amd64

  # copy to a registry with a self-signed certificate
  hauler copy busybox:latest registry.example.com/busybox:latest --insecure-skip-tls-verify

  # copy to a registry with no TLS at all
  hauler copy busybox:latest registry.example.com/busybox:latest --plain-http`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return CopyImageCmd(cmd.Context(), o, args[0], args[1], ro)
		},
	}
	o.AddFlags(cmd)
	parent.AddCommand(cmd)
}

// CopyImageCmd copies src to dst directly, registry to registry, no store involved.
func CopyImageCmd(ctx context.Context, o *flags.ImageCopyOpts, src, dst string, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	retries, err := flags.ResolveRetries(o.Retries)
	if err != nil {
		return err
	}

	tr, err := content.BuildTransport(o.InsecureSkipTLSVerify, o.CaFile)
	if err != nil {
		return err
	}

	opts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithContext(ctx),
		remote.WithTransport(tr),
	}

	var nameOpts []gname.Option
	if o.PlainHTTP {
		nameOpts = append(nameOpts, gname.Insecure)
	}

	srcRef, err := gname.ParseReference(src, nameOpts...)
	if err != nil {
		return fmt.Errorf("parsing source reference %q: %w", src, err)
	}
	dstRef, err := gname.ParseReference(dst, nameOpts...)
	if err != nil {
		return fmt.Errorf("parsing destination reference %q: %w", dst, err)
	}

	l.Infof("copying [%s] to [%s]", src, dst)

	start := time.Now()
	var digest string
	err = retry.Operation(ctx, &flags.StoreRootOpts{Retries: retries}, ro, func() error {
		d, copyErr := copyOnce(srcRef, dstRef, o.Platform, opts)
		if copyErr == nil {
			digest = d
		}
		return copyErr
	})
	if err != nil {
		l.Errorf("unable to copy [%s] to [%s]: %v", src, dst, err)
		return err
	}

	if flags.AuditLevel(ro) != "none" {
		e := audit.Entry{
			Command:   "copy",
			Args:      []string{src, dst},
			Type:      "image",
			Reference: dst,
			Digest:    digest,
		}
		if flags.AuditLevel(ro) == "verbose" {
			sys := audit.BuildSystem()
			g := audit.BuildGlobal(ro, nil)
			e.System = &sys
			e.Global = &g
			e.Flags = map[string]any{
				"insecure-skip-tls-verify": o.InsecureSkipTLSVerify,
				"plain-http":               o.PlainHTTP,
				"ca-file":                  o.CaFile,
				"platform":                 o.Platform,
			}
		}
		if err := audit.Append(ro.HaulerDir, e); err != nil {
			l.Warnf("failed to write audit entry: %v", err)
		}
		l.Debugf("generated audit id of [%s]", audit.ID())
	} else {
		l.Debugf("generated audit id of [none]")
	}

	l.Infof("✓ copied [%s] to [%s] (%.1fs)", src, dst, time.Since(start).Seconds())

	return nil
}

// copyOnce copies srcRef to dstRef and returns the digest copied. No
// platform filter keeps a multi-arch index intact; platform picks one child.
func copyOnce(srcRef, dstRef gname.Reference, platform string, opts []remote.Option) (string, error) {
	desc, err := remote.Get(srcRef, opts...)
	if err != nil {
		return "", fmt.Errorf("fetching descriptor for %q: %w", srcRef.Name(), err)
	}

	if idx, idxErr := desc.ImageIndex(); idxErr == nil && platform == "" {
		if err := remote.WriteIndex(dstRef, idx, opts...); err != nil {
			return "", fmt.Errorf("writing index for %q: %w", dstRef.Name(), err)
		}
		d, err := idx.Digest()
		if err != nil {
			return "", fmt.Errorf("getting index digest for %q: %w", srcRef.Name(), err)
		}
		return d.String(), nil
	}

	var img gv1.Image
	if platform != "" {
		p, err := gv1.ParsePlatform(platform)
		if err != nil {
			return "", err
		}
		img, err = remote.Image(srcRef, append(append([]remote.Option{}, opts...), remote.WithPlatform(*p))...)
		if err != nil {
			return "", fmt.Errorf("fetching image %q: %w", srcRef.Name(), err)
		}
	} else {
		img, err = desc.Image()
		if err != nil {
			return "", fmt.Errorf("fetching image %q: %w", srcRef.Name(), err)
		}
	}

	if err := remote.Write(dstRef, img, opts...); err != nil {
		return "", fmt.Errorf("writing image for %q: %w", dstRef.Name(), err)
	}
	d, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("getting image digest for %q: %w", srcRef.Name(), err)
	}
	return d.String(), nil
}
