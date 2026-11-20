package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
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
  hauler copy busybox registry.example.com/busybox --all-tags --no-clobber

  # copy an image without its signatures, attestations, sboms, and referrers
  hauler copy busybox:latest registry.example.com/busybox:latest --exclude-extras`,
		Args: cobra.ExactArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// --jobs requires --all-tags
			if cmd.Flags().Changed("jobs") && !o.AllTags {
				return fmt.Errorf("--jobs requires --all-tags")
			}
			if o.Jobs < 1 {
				return fmt.Errorf("invalid --jobs [%d]... must be at least 1", o.Jobs)
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

	// parsed once up front so a bad --platform fails fast instead of retrying
	var platform *gv1.Platform
	if o.Platform != "" {
		platform, err = gv1.ParsePlatform(o.Platform)
		if err != nil {
			return fmt.Errorf("failed to parse platform [%s]: %w", o.Platform, err)
		}
	}

	if o.AllTags {
		return copyRepository(ctx, o, src, dst, platform, opts, nameOpts, rso, ro)
	}

	srcRef, err := gname.ParseReference(src, nameOpts...)
	if err != nil {
		return fmt.Errorf("failed to parse source reference [%s]: %w", src, err)
	}
	dstRef, err := gname.ParseReference(dst, nameOpts...)
	if err != nil {
		return fmt.Errorf("failed to parse destination reference [%s]: %w", dst, err)
	}

	if o.NoClobber {
		existing, err := existingTagDigest(dstRef, opts)
		if err != nil {
			return err
		}
		if existing != "" {
			return fmt.Errorf("tag [%s] already exists with digest [%s]... not overwriting due to --no-clobber", dstRef, existing)
		}
	}

	l.Infof("copying [%s] to [%s]", src, dst)

	start := time.Now()
	var digest string
	var extras int
	err = retry.Operation(ctx, rso, ro, func() error {
		d, n, copyErr := copyWithExtras(srcRef, dstRef, platform, o.ExcludeExtras, opts)
		if copyErr == nil {
			digest, extras = d, n
		}
		return copyErr
	})
	if err != nil {
		l.Errorf("unable to copy [%s] to [%s]: %v", src, dst, err)
		return err
	}

	appendCopyAudit(ctx, ro, src, dst, digest, o)

	l.Infof("✓ copied [%s] to [%s] with %d extra(s) (%.1fs)", src, dst, extras, time.Since(start).Seconds())

	return nil
}

// copyRepository mirrors crane's CopyRepository, copying every tag from src to dst concurrently, up to o.Jobs at a time.
func copyRepository(ctx context.Context, o *flags.ImageCopyOpts, src, dst string, platform *gv1.Platform, opts []remote.Option, nameOpts []gname.Option, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	srcRepo, err := gname.NewRepository(src, nameOpts...)
	if err != nil {
		return fmt.Errorf("failed to parse source repository [%s]: %w", src, err)
	}
	dstRepo, err := gname.NewRepository(dst, nameOpts...)
	if err != nil {
		return fmt.Errorf("failed to parse destination repository [%s]: %w", dst, err)
	}

	tags, err := remote.List(srcRepo, opts...)
	if err != nil {
		return fmt.Errorf("failed to list tags for [%s]: %w", src, err)
	}

	// Snapshotted once up front, same as crane; a concurrent push at the destination between here and a tag's copy is an accepted race, not one hauler closes any better than crane does.
	skip := map[string]bool{}
	if o.NoClobber {
		have, err := remote.List(dstRepo, opts...)
		if err != nil && !isNotFoundStatus(err) {
			return fmt.Errorf("failed to list tags for [%s]: %w", dst, err)
		}
		for _, t := range have {
			skip[t] = true
		}
	}

	l.Infof("copying all tags from [%s] to [%s]", src, dst)

	start := time.Now()
	ignoreErrors := flags.ShouldIgnoreErrors(ro)
	var copied, extras, skipped, failed atomic.Int64

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(o.Jobs)

	// Rebinds each per-tag remote call to gctx instead of opts' outer ctx, so a sibling's failure aborts in-flight calls promptly.
	tagOpts := append(append([]remote.Option{}, opts...), remote.WithContext(gctx))

	for _, tag := range tags {
		// extras tags are copied with the image they belong to, or not at all with --exclude-extras
		if extrasTag.MatchString(tag) {
			l.Debugf("tag [%s] is a signature, attestation, sbom, or referrers tag... skipping...", tag)
			continue
		}
		if skip[tag] {
			l.Debugf("tag [%s] already exists at [%s]... skipping due to --no-clobber...", tag, dst)
			skipped.Add(1)
			continue
		}

		tag := tag
		g.Go(func() error {
			tagSrc, tagDst := src+":"+tag, dst+":"+tag

			srcRef, err := gname.ParseReference(tagSrc, nameOpts...)
			if err != nil {
				return fmt.Errorf("failed to parse source reference [%s]: %w", tagSrc, err)
			}
			dstRef, err := gname.ParseReference(tagDst, nameOpts...)
			if err != nil {
				return fmt.Errorf("failed to parse destination reference [%s]: %w", tagDst, err)
			}

			var digest string
			var n int
			copyErr := retry.Operation(gctx, rso, ro, func() error {
				d, c, err := copyWithExtras(srcRef, dstRef, platform, o.ExcludeExtras, tagOpts)
				if err == nil {
					digest, n = d, c
				}
				return err
			})
			if copyErr != nil {
				if ignoreErrors {
					l.Warnf("unable to copy tag [%s]: %v... skipping...", tag, copyErr)
					failed.Add(1)
					return nil
				}
				return fmt.Errorf("failed to copy tag [%s]: %w", tag, copyErr)
			}

			l.Debugf("✓ copied [%s] to [%s]", tagSrc, tagDst)
			appendCopyAudit(gctx, ro, tagSrc, tagDst, digest, o)
			copied.Add(1)
			extras.Add(int64(n))
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		l.Errorf("unable to copy all tags from [%s] to [%s]: %v", src, dst, err)
		return err
	}

	l.Infof("✓ copied %d tag(s) from [%s] to [%s] with %d extra(s) (%d skipped due to --no-clobber, %d failed, %.1fs)", copied.Load(), src, dst, extras.Load(), skipped.Load(), failed.Load(), time.Since(start).Seconds())

	return nil
}

// extrasTag matches the cosign sha256-<hex>.sig/.att/.sbom tags and the bare sha256-<hex> referrers fallback tag
var extrasTag = regexp.MustCompile(`^sha256-[0-9a-f]{64}(\.sig|\.att|\.sbom)?$`)

// copyWithExtras copies srcRef to dstRef, then its signatures, attestations, sboms, and referrers unless excludeExtras is set
func copyWithExtras(srcRef, dstRef gname.Reference, platform *gv1.Platform, excludeExtras bool, opts []remote.Option) (string, int, error) {
	digest, subjects, err := copyOnce(srcRef, dstRef, platform, opts)
	if err != nil || excludeExtras {
		return digest, 0, err
	}

	n := 0
	copied := map[string]bool{}
	for _, subject := range subjects {
		c, err := copyExtras(srcRef.Context(), dstRef.Context(), subject, copied, opts)
		n += c
		if err != nil {
			return digest, n, err
		}
	}
	return digest, n, nil
}

// copyExtras copies the cosign tag artifacts and oci referrers of one subject digest, skipping any already in copied
func copyExtras(srcRepo, dstRepo gname.Repository, subject gv1.Hash, copied map[string]bool, opts []remote.Option) (int, error) {
	n := 0
	prefix := strings.ReplaceAll(subject.String(), ":", "-")
	for _, ext := range []string{".sig", ".att", ".sbom"} {
		tag := prefix + ext
		desc, err := remote.Get(srcRepo.Tag(tag), opts...)
		if err != nil {
			if isMissingManifest(err) {
				continue
			}
			return n, fmt.Errorf("failed to fetch [%s] for [%s]: %w", tag, subject, err)
		}
		d, _, err := writeDescriptor(desc, dstRepo.Tag(tag), opts)
		if err != nil {
			return n, err
		}
		copied[d] = true
		n++
	}

	idx, err := remote.Referrers(srcRepo.Digest(subject.String()), opts...)
	if err != nil {
		if isMissingManifest(err) {
			return n, nil
		}
		return n, fmt.Errorf("failed to list referrers for [%s]: %w", subject, err)
	}
	m, err := idx.IndexManifest()
	if err != nil {
		return n, fmt.Errorf("failed to read referrers for [%s]: %w", subject, err)
	}
	for _, r := range m.Manifests {
		if copied[r.Digest.String()] {
			continue
		}
		desc, err := remote.Get(srcRepo.Digest(r.Digest.String()), opts...)
		if err != nil {
			if isMissingManifest(err) {
				continue
			}
			return n, fmt.Errorf("failed to fetch referrer [%s] for [%s]: %w", r.Digest, subject, err)
		}
		if _, _, err := writeDescriptor(desc, dstRepo.Digest(r.Digest.String()), opts); err != nil {
			return n, err
		}
		copied[r.Digest.String()] = true
		n++
	}
	return n, nil
}

// copyOnce copies srcRef to dstRef and returns the digest and the subjects whose extras belong with it... a nil platform keeps a multi-arch index intact
func copyOnce(srcRef, dstRef gname.Reference, platform *gv1.Platform, opts []remote.Option) (string, []gv1.Hash, error) {
	desc, err := remote.Get(srcRef, opts...)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch descriptor for [%s]: %w", srcRef.Name(), err)
	}

	if platform == nil {
		return writeDescriptor(desc, dstRef, opts)
	}

	img, err := remote.Image(srcRef, append(append([]remote.Option{}, opts...), remote.WithPlatform(*platform))...)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch image [%s]: %w", srcRef.Name(), err)
	}
	return writeImage(img, dstRef, opts)
}

// writeDescriptor writes desc to dstRef whole and returns its digest plus every child manifest digest when it is an index
func writeDescriptor(desc *remote.Descriptor, dstRef gname.Reference, opts []remote.Option) (string, []gv1.Hash, error) {
	idx, err := desc.ImageIndex()
	if err != nil {
		img, err := desc.Image()
		if err != nil {
			return "", nil, fmt.Errorf("failed to fetch image [%s]: %w", desc.Digest, err)
		}
		return writeImage(img, dstRef, opts)
	}

	if err := remote.WriteIndex(dstRef, idx, opts...); err != nil {
		return "", nil, fmt.Errorf("failed to write index for [%s]: %w", dstRef.Name(), err)
	}
	m, err := idx.IndexManifest()
	if err != nil {
		return "", nil, fmt.Errorf("failed to read index [%s]: %w", desc.Digest, err)
	}
	subjects := []gv1.Hash{desc.Digest}
	for _, child := range m.Manifests {
		subjects = append(subjects, child.Digest)
	}
	return desc.Digest.String(), subjects, nil
}

// writeImage writes img to dstRef and returns its digest as the only subject
func writeImage(img gv1.Image, dstRef gname.Reference, opts []remote.Option) (string, []gv1.Hash, error) {
	if err := remote.Write(dstRef, img, opts...); err != nil {
		return "", nil, fmt.Errorf("failed to write image for [%s]: %w", dstRef.Name(), err)
	}
	d, err := img.Digest()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get image digest for [%s]: %w", dstRef.Name(), err)
	}
	return d.String(), []gv1.Hash{d}, nil
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

// isMissingManifest reports a 404 or a manifest/name unknown code, the same test the store uses before skipping an extra
func isMissingManifest(err error) bool {
	var terr *ggcrtransport.Error
	if !errors.As(err, &terr) {
		return false
	}
	if terr.StatusCode == http.StatusNotFound {
		return true
	}
	for _, diag := range terr.Errors {
		if diag.Code == ggcrtransport.ManifestUnknownErrorCode || diag.Code == ggcrtransport.NameUnknownErrorCode {
			return true
		}
	}
	return false
}

// appendCopyAudit records one audit entry, shared between the single-copy and --all-tags paths.
func appendCopyAudit(ctx context.Context, ro *flags.CliRootOpts, src, dst, digest string, o *flags.ImageCopyOpts) {
	l := log.FromContext(ctx)

	if ro.AuditLevel == "none" {
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
	if ro.AuditLevel == "verbose" {
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
			"exclude-extras":                   o.ExcludeExtras,
			"jobs":                             o.Jobs,
			"allow-nondistributable-artifacts": o.AllowNondistributableArtifacts,
		}
	}
	if err := audit.Append(ro.HaulerDir, e); err != nil {
		l.Warnf("failed to write audit entry: %v", err)
	}
	l.Debugf("generated audit id of [%s]", audit.ID())
}
