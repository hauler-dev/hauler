package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ggcrtransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/audit"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/retry"
)

func addCopy(parent *cobra.Command, ro *flags.CliRootOpts) {
	o := &flags.ImageCopyOpts{}

	cmd := &cobra.Command{
		Use:     "copy <source-ref> <destination-ref>",
		Aliases: []string{"cp"},
		Short:   "(EXPERIMENTAL) Copy an artifact between registries",
		Example: `  # copy an image to another registry
  hauler copy busybox:latest registry.example.com/busybox:latest

  # copy a specific platform out of a multi-arch image
  hauler copy ghcr.io/hauler-dev/hauler-debug:v2.0.3 registry.example.com/hauler-debug:v2.0.3 --platform linux/amd64

  # copy to a registry with a self-signed certificate
  hauler copy busybox:latest registry.example.com/busybox:latest --insecure-skip-tls-verify

  # copy to a registry with no TLS at all
  hauler copy busybox:latest registry.example.com/busybox:latest --plain-http

  # copy every tag in a repository
  hauler copy busybox registry.example.com/busybox --all-tags

  # copy every tag, skipping any that already exist at the destination
  hauler copy busybox registry.example.com/busybox --all-tags --no-clobber`,
		Args: cobra.ExactArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// --jobs requires --all-tags
			if cmd.Flags().Changed("jobs") && !o.AllTags {
				return fmt.Errorf("--jobs requires --all-tags")
			}
			if o.Jobs < 1 {
				return fmt.Errorf("--jobs must be >= 1, got %d", o.Jobs)
			}
			return nil
		},
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
	rso := &flags.StoreRootOpts{Retries: retries}

	tr, err := content.BuildTransport(o.InsecureSkipTLSVerify, o.CaFile)
	if err != nil {
		return err
	}

	opts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithContext(ctx),
		remote.WithTransport(tr),
	}
	if o.AllowNondistributableArtifacts {
		opts = append(opts, remote.WithNondistributable)
	}

	var nameOpts []gname.Option
	if o.PlainHTTP {
		nameOpts = append(nameOpts, gname.Insecure)
	}

	if o.AllTags {
		return copyRepository(ctx, o, src, dst, opts, nameOpts, rso, ro)
	}

	srcRef, err := gname.ParseReference(src, nameOpts...)
	if err != nil {
		return fmt.Errorf("parsing source reference %q: %w", src, err)
	}
	dstRef, err := gname.ParseReference(dst, nameOpts...)
	if err != nil {
		return fmt.Errorf("parsing destination reference %q: %w", dst, err)
	}

	if o.NoClobber {
		existing, err := existingTagDigest(dstRef, opts)
		if err != nil {
			return err
		}
		if existing != "" {
			return fmt.Errorf("refusing to clobber existing tag %s@%s", dstRef, existing)
		}
	}

	l.Infof("copying [%s] to [%s]", src, dst)

	start := time.Now()
	var digest string
	err = retry.Operation(ctx, rso, ro, func() error {
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

	appendCopyAudit(ctx, ro, src, dst, digest, o)

	l.Infof("✓ copied [%s] to [%s] (%.1fs)", src, dst, time.Since(start).Seconds())

	return nil
}

// copyRepository mirrors crane's CopyRepository, copying every tag from src to dst concurrently, up to o.Jobs at a time.
func copyRepository(ctx context.Context, o *flags.ImageCopyOpts, src, dst string, opts []remote.Option, nameOpts []gname.Option, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	srcRepo, err := gname.NewRepository(src, nameOpts...)
	if err != nil {
		return fmt.Errorf("parsing source repository %q: %w", src, err)
	}
	dstRepo, err := gname.NewRepository(dst, nameOpts...)
	if err != nil {
		return fmt.Errorf("parsing destination repository %q: %w", dst, err)
	}

	tags, err := remote.List(srcRepo, opts...)
	if err != nil {
		return fmt.Errorf("listing tags for %q: %w", src, err)
	}

	// Snapshotted once up front, same as crane; a concurrent push at the destination between here and a tag's copy is an accepted race, not one hauler closes any better than crane does.
	skip := map[string]bool{}
	if o.NoClobber {
		have, err := remote.List(dstRepo, opts...)
		if err != nil && !isNotFoundStatus(err) {
			return fmt.Errorf("listing tags for %q: %w", dst, err)
		}
		for _, t := range have {
			skip[t] = true
		}
	}

	l.Infof("copying all tags from [%s] to [%s]", src, dst)

	start := time.Now()
	ignoreErrors := flags.ShouldIgnoreErrors(ro)
	var copied, skipped, failed atomic.Int64

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(o.Jobs)

	// Rebinds each per-tag remote call to gctx instead of opts' outer ctx, so a sibling's failure aborts in-flight calls promptly.
	tagOpts := append(append([]remote.Option{}, opts...), remote.WithContext(gctx))

	for _, tag := range tags {
		if skip[tag] {
			l.Debugf("skipping tag [%s]: already exists at [%s] (--no-clobber)", tag, dst)
			skipped.Add(1)
			continue
		}

		tag := tag
		g.Go(func() error {
			tagSrc, tagDst := src+":"+tag, dst+":"+tag

			srcRef, err := gname.ParseReference(tagSrc, nameOpts...)
			if err != nil {
				return fmt.Errorf("parsing source reference %q: %w", tagSrc, err)
			}
			dstRef, err := gname.ParseReference(tagDst, nameOpts...)
			if err != nil {
				return fmt.Errorf("parsing destination reference %q: %w", tagDst, err)
			}

			var digest string
			copyErr := retry.Operation(gctx, rso, ro, func() error {
				d, err := copyOnce(srcRef, dstRef, o.Platform, tagOpts)
				if err == nil {
					digest = d
				}
				return err
			})
			if copyErr != nil {
				if ignoreErrors {
					l.Warnf("unable to copy tag [%s]: %v... skipping...", tag, copyErr)
					failed.Add(1)
					return nil
				}
				return fmt.Errorf("copying tag [%s]: %w", tag, copyErr)
			}

			l.Debugf("✓ copied [%s] to [%s]", tagSrc, tagDst)
			appendCopyAudit(gctx, ro, tagSrc, tagDst, digest, o)
			copied.Add(1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		l.Errorf("unable to copy all tags from [%s] to [%s]: %v", src, dst, err)
		return err
	}

	l.Infof("✓ copied %d tag(s) from [%s] to [%s] (%d skipped due to --no-clobber, %d failed, %.1fs)", copied.Load(), src, dst, skipped.Load(), failed.Load(), time.Since(start).Seconds())

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

// existingTagDigest mirrors crane's Copy no-clobber check, returning the digest already at dstRef, or "" if it's a digest reference or a tag that doesn't exist yet.
func existingTagDigest(dstRef gname.Reference, opts []remote.Option) (string, error) {
	tag, ok := dstRef.(gname.Tag)
	if !ok {
		return "", nil
	}
	desc, err := remote.Head(tag, opts...)
	if err != nil {
		if isNotFoundStatus(err) {
			return "", nil
		}
		return "", err
	}
	return desc.Digest.String(), nil
}

// isNotFoundStatus reports a registry 404 or 403, the two statuses crane treats as "doesn't exist yet" rather than a real failure.
func isNotFoundStatus(err error) bool {
	var terr *ggcrtransport.Error
	if !errors.As(err, &terr) {
		return false
	}
	return terr.StatusCode == http.StatusNotFound || terr.StatusCode == http.StatusForbidden
}

// appendCopyAudit records one audit entry, shared between the single-copy and --all-tags paths.
func appendCopyAudit(ctx context.Context, ro *flags.CliRootOpts, src, dst, digest string, o *flags.ImageCopyOpts) {
	l := log.FromContext(ctx)

	if flags.AuditLevel(ro) == "none" {
		l.Debugf("generated audit id of [none]")
		return
	}

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
			"insecure-skip-tls-verify":         o.InsecureSkipTLSVerify,
			"plain-http":                       o.PlainHTTP,
			"ca-file":                          o.CaFile,
			"platform":                         o.Platform,
			"all-tags":                         o.AllTags,
			"no-clobber":                       o.NoClobber,
			"jobs":                             o.Jobs,
			"allow-nondistributable-artifacts": o.AllowNondistributableArtifacts,
		}
	}
	if err := audit.Append(ro.HaulerDir, e); err != nil {
		l.Warnf("failed to write audit entry: %v", err)
	}
	l.Debugf("generated audit id of [%s]", audit.ID())
}
