package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/engine"
	"k8s.io/apimachinery/pkg/util/yaml"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/artifacts/chart"
	"hauler.dev/go/hauler/v2/pkg/artifacts/file"
	"hauler.dev/go/hauler/v2/pkg/audit"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/cosign"
	"hauler.dev/go/hauler/v2/pkg/getter"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/retry"
	"hauler.dev/go/hauler/v2/pkg/store"
)

func AddFileCmd(ctx context.Context, o *flags.AddFileOpts, s *store.Layout, reference string, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	// content.OCI's per-artifact index.json write only fsyncs once per
	// indexCheckpointInterval (30s); the rest land in page cache. A single
	// `store add file` can therefore return success without its index entry
	// being durable, e.g. if a prior add against this store checkpointed
	// recently enough that this run's write falls inside the window. Force
	// one full fsync at the end so every `store add` subcommand leaves the
	// store durable, matching SyncCmd's trailing SaveIndex().
	defer func() {
		if err := s.OCI.SaveIndex(); err != nil {
			l.Warnf("failed to save index durably after adding file: %v", err)
		}
	}()

	cfg := v1.File{
		Path:                  reference,
		CaFile:                o.CaFile,
		InsecureSkipTLSVerify: o.InsecureSkipTLSVerify,
	}
	if len(o.Name) > 0 {
		cfg.Name = o.Name
	}

	l.Infof("adding file [%s] to the store", reference)

	return storeFile(ctx, s, cfg, ro, o.StoreRootOpts)
}

func storeFile(ctx context.Context, s *store.Layout, fi v1.File, ro *flags.CliRootOpts, rso *flags.StoreRootOpts) error {
	l := log.FromContext(ctx)

	start := time.Now()
	ignoreErrors := flags.ShouldIgnoreErrors(ro)

	if err := ctx.Err(); err != nil {
		log.BaseFromContext(ctx).Debugf("skipping file [%s]: %v", fi.Path, err)
		return err
	}

	copts := getter.ClientOptions{
		NameOverride:          fi.Name,
		InsecureSkipTLSVerify: fi.InsecureSkipTLSVerify,
		CAFile:                fi.CaFile,
	}

	f := file.NewFile(fi.Path, file.WithClient(getter.NewClient(copts)), file.WithContext(ctx))
	ref, err := reference.NewTagged(f.Name(fi.Path), consts.DefaultTag)
	if err != nil {
		if ignoreErrors {
			log.BaseFromContext(ctx).Warnf("unable to derive a store reference for file [%s]: %v... skipping...", fi.Path, err)
			return nil
		}
		log.BaseFromContext(ctx).Errorf("unable to derive a store reference for file [%s]: %v", fi.Path, err)
		return err
	}

	log.BaseFromContext(ctx).Debugf("adding file [%s] to the store as [%s]", fi.Path, ref.Name())

	var desc ocispec.Descriptor
	err = retry.Operation(ctx, rso, ro, func() error {
		var addErr error
		desc, addErr = s.AddArtifact(ctx, f, ref.Name())
		return addErr
	})
	if err != nil {
		if ignoreErrors {
			log.BaseFromContext(ctx).Warnf("unable to add file [%s] to store: %v... skipping...", fi.Path, err)
			return nil
		} else if errors.Is(err, context.Canceled) {
			// Under errgroup.WithContext fail-fast (runFileJobs), one real
			// failure cancels every other in-flight file's context -- see
			// storeImage's identical branch for the full rationale.
			log.BaseFromContext(ctx).Debugf("unable to add file [%s] to store: %v", fi.Path, err)
			return err
		} else {
			log.BaseFromContext(ctx).Errorf("unable to add file [%s] to store: %v", fi.Path, err)
			return err
		}
	}

	resolvedPath := fi.Path
	if !strings.HasPrefix(fi.Path, "http://") && !strings.HasPrefix(fi.Path, "https://") {
		if abs, err := filepath.Abs(fi.Path); err == nil {
			resolvedPath = abs
		}
	}
	if auditLevel(ro) != "none" {
		e := audit.Entry{
			StoreID:           s.StoreID,
			Store:             s.Root,
			Type:              "file",
			Command:           "store add file",
			Args:              []string{audit.SanitizeURL(fi.Path)},
			Reference:         audit.SanitizeURL(resolvedPath),
			PortableReference: audit.ShortFileRef(fi.Path),
			Digest:            desc.Digest.String(),
		}
		if auditLevel(ro) == "verbose" {
			sys := audit.BuildSystem()
			g := audit.BuildGlobal(ro, rso)
			e.System = &sys
			e.Global = &g
			e.Flags = map[string]any{
				"name": fi.Name,
			}
		}
		if err := audit.Append(ro.HaulerDir, e); err != nil {
			l.Warnf("failed to write audit entry: %v", err)
		}
		l.Debugf("generated audit id of [%s]", audit.ID())
	} else {
		l.Debugf("generated audit id of [none]")
	}

	// stats.Layers is always 1 here: File.Layers() always returns exactly
	// one layer (pkg/artifacts/file/file.go). f.Size() costs nothing extra
	// on the success path -- compute() already ran (and memoized its result)
	// inside the AddArtifact call above.
	var stats *store.ImageStats
	if size, sizeErr := f.Size(); sizeErr == nil {
		stats = &store.ImageStats{}
		stats.Layers.Store(1)
		stats.Bytes.Store(size)
	}

	log.BaseFromContext(ctx).Infof("%s", formatAddedLine(ref.Name(), stats, time.Since(start)))

	return nil
}

func AddImageCmd(ctx context.Context, o *flags.AddImageOpts, s *store.Layout, reference string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	// AddImage's per-artifact AddIndex calls (base image, then one per
	// discovered cosign signature/attestation/SBOM/referrer) funnel through
	// content.OCI's 30-second durable checkpoint, so only the very first
	// call on a fresh store is fsync'd "for free" -- every call after that,
	// including the final one, may land only in page cache. Force one full
	// fsync at the end so `store add image` always ends durable, matching
	// SyncCmd's trailing SaveIndex(). Blobs are unaffected: writeBlobOnce
	// always fsyncs; only index.json entries are at risk.
	defer func() {
		if err := s.OCI.SaveIndex(); err != nil {
			l.Warnf("failed to save index durably after adding image: %v", err)
		}
	}()

	cfg := v1.Image{
		Name:                         reference,
		Key:                          o.Key,
		Tlog:                         o.Tlog,
		CertIdentity:                 o.CertIdentity,
		CertIdentityRegexp:           o.CertIdentityRegexp,
		CertOidcIssuer:               o.CertOidcIssuer,
		CertOidcIssuerRegexp:         o.CertOidcIssuerRegexp,
		CertGithubWorkflowRepository: o.CertGithubWorkflowRepository,
		Platform:                     o.Platform,
		Rewrite:                      o.Rewrite,
		ExcludeExtras:                o.ExcludeExtras,
		Local:                        o.Local,
		CaFile:                       o.CaFile,
		InsecureSkipTLSVerify:        o.InsecureSkipTLSVerify,
	}

	if o.Local {
		if o.Key != "" || o.CertIdentity != "" || o.CertIdentityRegexp != "" {
			return fmt.Errorf("--local cannot be combined with cosign verification flags (--key, --certificate-identity, --certificate-identity-regexp): signatures are not available from the Docker daemon")
		}
		if o.Platform != "" {
			l.Warnf("--platform is ignored when --local is set: the Docker daemon stores only the host platform image")
		}
		l.Infof("adding image [%s] from local Docker daemon to the store", cfg.Name)
		return storeLocalImage(ctx, s, cfg, rso, ro, o.Rewrite)
	}

	pinnedDigest, err := verifyAddImage(ctx, o, cfg.Name, rso, ro)
	if err != nil {
		// Semantics and log shape mirror store sync's per-job rule; see
		// logVerifyFailure's doc.
		if propagate := logVerifyFailure(l, cfg.Name, err, flags.ShouldIgnoreErrors(ro)); propagate {
			return err
		}
	}

	l.Infof("adding image [%s] to the store", cfg.Name)

	// verified is true only when verification was both requested and
	// succeeded: err is nil on success and non-nil on a failure that fell
	// through to here under --ignore-errors, so err == nil already rules out
	// the failed-but-stored-anyway case without checking ignoreErrors again.
	verified := err == nil && !addImageVerifyConfig(o).Empty()
	return storeImage(ctx, s, cfg, o.Platform, o.ExcludeExtras, rso, ro, o.Rewrite, pinnedDigest, verified)
}

// addImageVerifyConfig collapses o's verification flags into a cosign.Config,
// which is empty exactly when the invocation asked for no verification.
//
// The branch mirrors the key-then-keyless precedence `store add image` has
// always applied rather than forwarding whatever flags happen to be set: a key
// wins and the identity flags are ignored. cosign.Config.validate rejects that
// pairing outright, so building the Config from the raw flags would turn an
// invocation that works today into a hard error. imageJob.verifyConfig makes
// the same choice for `store sync`.
func addImageVerifyConfig(o *flags.AddImageOpts) cosign.Config {
	switch {
	case o.Key != "":
		return cosign.Config{Key: o.Key, Tlog: o.Tlog, InsecureSkipTLSVerify: o.InsecureSkipTLSVerify, CaFile: o.CaFile}
	case o.CertIdentityRegexp != "" || o.CertIdentity != "":
		return cosign.Config{
			CertIdentity:                 o.CertIdentity,
			CertIdentityRegexp:           o.CertIdentityRegexp,
			CertOidcIssuer:               o.CertOidcIssuer,
			CertOidcIssuerRegexp:         o.CertOidcIssuerRegexp,
			CertGithubWorkflowRepository: o.CertGithubWorkflowRepository,
			InsecureSkipTLSVerify:        o.InsecureSkipTLSVerify,
			CaFile:                       o.CaFile,
		}
	default:
		return cosign.Config{}
	}
}

// verifyAddImage pins ref to a digest and verifies that exact digest, returning
// the digest for storeImage to fetch. An invocation that asked for no
// verification returns the empty digest, which leaves storeImage resolving the
// tag as before -- there is no window to close when nothing is checked, and an
// unconditional HEAD would add a round trip to the common unsigned case.
//
// Verifying the digest rather than the tag is the point: a tag verified and
// then re-resolved by storeImage can move between the two calls, so the bytes
// stored need not be the bytes checked. resolveAndVerify closes the same window
// for `store sync`.
//
// The two post-pin failure branches (cosign.NewVerifier, v.Verify) return the
// pinned digest alongside the error rather than "": under --ignore-errors
// AddImageCmd stores the image anyway, and it must store the exact bytes that
// were checked even though the check failed. The pre-pin branches (a bad
// reference, or the pin itself failing) have no digest to give back.
//
// One Verifier per call, built directly rather than through a cosign.Cache:
// there is exactly one image to check, so there is nothing to share it with.
//
// Every error it returns is a *verifyError, with the same stage strings
// resolveAndVerify uses for its equivalent branches, so logVerifyFailure
// reports add.go and sync.go failures in an identical shape.
func verifyAddImage(ctx context.Context, o *flags.AddImageOpts, ref string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) (string, error) {
	l := log.FromContext(ctx)

	cfg := addImageVerifyConfig(o)
	if cfg.Empty() {
		return "", nil
	}

	r, err := name.ParseReference(ref)
	if err != nil {
		return "", &verifyError{stage: "unable to parse image reference", err: err}
	}

	pinned, err := pinDigest(ctx, r, rso, ro)
	if err != nil {
		return "", &verifyError{stage: "unable to resolve image digest", err: err}
	}

	if cfg.Keyless() {
		l.Infof("verifying keyless signature for [%s]", ref)
	}

	v, err := cosign.NewVerifier(ctx, cfg, rso, ro)
	if err != nil {
		return pinned, &verifyError{stage: "unable to configure signature verification", err: err}
	}
	defer v.Close()

	if err := v.Verify(ctx, r.Context().Digest(pinned).Name()); err != nil {
		return pinned, &verifyError{stage: "signature verification failed", err: err}
	}

	if cfg.Keyless() {
		l.Infof("✓ keyless signature verified for image [%s]", ref)
	} else {
		l.Infof("✓ signature verified for image [%s]", ref)
	}
	return pinned, nil
}

func AddChartCmd(ctx context.Context, o *flags.AddChartOpts, s *store.Layout, chartName string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {

	l := log.FromContext(ctx)

	// Nothing in the chart path forces an fsync: every descriptor it adds --
	// the chart, its dependencies, and each discovered image -- lands through
	// content.OCI's 30-second durable checkpoint, so a whole run can return
	// success with index.json still only in page cache. This trailing
	// SaveIndex is what makes the run durable, matching SyncCmd's.
	defer func() {
		if err := s.OCI.SaveIndex(); err != nil {
			l.Warnf("failed to save index durably after adding chart: %v", err)
		}
	}()

	l.Infof("adding chart [%s] to the store", chartName)

	// The job owns its *action.ChartPathOptions rather than the caller's, so
	// the invariant every chartJob holds -- no two jobs share a pointee --
	// is true of dependency-derived jobs and this one alike.
	chartOpts := *o.ChartOpts
	opts := *o
	opts.ChartOpts = &chartOpts

	job := chartJob{
		cfg: v1.Chart{
			Name:    chartName,
			RepoURL: o.ChartOpts.RepoURL,
			Version: o.ChartOpts.Version,
		},
		opts:    opts,
		rewrite: o.Rewrite,
	}

	return runChartJobs(ctx, s, []chartJob{job}, o.Concurrency, rso, ro, newProgressRenderer(o.NoProgress, ro.LogLevel))
}

// formatAddedLine formats the completion line logged after an artifact
// (image or file) is added to the store. When stats has at least one layer
// recorded, it includes layer count and human-readable total blob size;
// otherwise it falls back to an elapsed-only line and must never print
// "0 layers" (stats == nil covers storeLocalImage, whose s.AddLocalImage
// path never populates ImageStats).
func formatAddedLine(ref string, stats *store.ImageStats, elapsed time.Duration) string {
	if stats != nil {
		if layers := stats.Layers.Load(); layers > 0 {
			unit := "layer"
			if layers != 1 {
				unit = "layers"
			}
			return fmt.Sprintf("✓ added %s (%d %s, %s, %.1fs)", ref, layers, unit, humanize.Bytes(uint64(stats.Bytes.Load())), elapsed.Seconds())
		}
	}
	return fmt.Sprintf("✓ added %s (%.1fs)", ref, elapsed.Seconds())
}

func storeLocalImage(ctx context.Context, s *store.Layout, i v1.Image, _ *flags.StoreRootOpts, ro *flags.CliRootOpts, rewrite string) error {
	l := log.FromContext(ctx)

	start := time.Now()
	ignoreErrors := flags.ShouldIgnoreErrors(ro)

	l.Debugf("resolving image [%s] from local Docker daemon (rewrite=%q)", i.Name, rewrite)

	r, err := name.ParseReference(i.Name)
	if err != nil {
		if ignoreErrors {
			l.Warnf("unable to parse image [%s]: %v... skipping...", i.Name, err)
			return nil
		}
		l.Errorf("unable to parse image [%s]: %v", i.Name, err)
		return err
	}

	localDigest, err := s.AddLocalImage(ctx, r.Name())
	if err != nil {
		if ignoreErrors {
			l.Warnf("unable to add image [%s] from Docker daemon to store: %v... skipping...", r.Name(), err)
			return nil
		}
		l.Errorf("unable to add image [%s] from Docker daemon to store: %v", r.Name(), err)
		return err
	}

	if rewrite != "" {
		rawRewrite := rewrite
		rewrite = strings.TrimPrefix(rewrite, "/")
		if !strings.Contains(rewrite, ":") {
			if tag, ok := r.(name.Tag); ok {
				rewrite = rewrite + ":" + tag.TagStr()
			} else {
				return fmt.Errorf("cannot rewrite digest reference [%s] without an explicit tag in the rewrite", r.Name())
			}
		}
		newRef, err := name.ParseReference(rewrite)
		if err != nil {
			return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
		}
		if err := rewriteReference(ctx, s, r, newRef, rawRewrite); err != nil {
			return err
		}
	}

	if auditLevel(ro) != "none" {
		e := audit.Entry{
			StoreID:   s.StoreID,
			Store:     s.Root,
			Type:      "image",
			Command:   "store add image",
			Args:      []string{i.Name},
			Reference: r.Name(),
			Digest:    localDigest,
		}
		if auditLevel(ro) == "verbose" {
			sys := audit.BuildSystem()
			g := audit.BuildGlobal(ro, nil)
			e.System = &sys
			e.Global = &g
			e.Flags = map[string]any{
				"verified": false,
				"local":    true,
				"rewrite":  rewrite,
			}
		}
		if err := audit.Append(ro.HaulerDir, e); err != nil {
			l.Warnf("failed to write audit entry: %v", err)
		}
		l.Debugf("generated audit id of [%s]", audit.ID())
	} else {
		l.Debugf("generated audit id of [none]")
	}

	l.Infof("%s", formatAddedLine(r.Name()+" from local Docker daemon", nil, time.Since(start)))
	return nil
}

// storeImage fetches and stores image i. verified records whether
// verification was both requested and actually succeeded for the exact bytes
// being stored (pinnedDigest) -- callers compute it themselves rather than
// storeImage re-deriving it from i's verification fields, since under
// --ignore-errors those fields stay set even after a failed check and i alone
// can no longer distinguish "verified" from "verification requested."
func storeImage(ctx context.Context, s *store.Layout, i v1.Image, platform string, excludeExtras bool, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, rewrite string, pinnedDigest string, verified bool) error {
	l := log.FromContext(ctx)

	start := time.Now()
	ignoreErrors := flags.ShouldIgnoreErrors(ro)

	if err := ctx.Err(); err != nil {
		log.BaseFromContext(ctx).Debugf("skipping image [%s]: %v", i.Name, err)
		return err
	}

	insecureSkipTLSVerify := i.InsecureSkipTLSVerify
	caFile := i.CaFile

	log.BaseFromContext(ctx).Debugf("resolving image [%s] (verified=%t, platform=%q, excludeExtras=%t, insecureSkipTLSVerify=%t, caFile=%q, rewrite=%q, digest=%q)", i.Name, verified, platform, excludeExtras, insecureSkipTLSVerify, caFile, rewrite, pinnedDigest)

	r, err := name.ParseReference(i.Name)
	if err != nil {
		if ignoreErrors {
			log.BaseFromContext(ctx).Warnf("unable to parse image [%s]: %v... skipping...", i.Name, err)
			return nil
		} else {
			log.BaseFromContext(ctx).Errorf("unable to parse image [%s]: %v", i.Name, err)
			return err
		}
	}

	// fetch image along with any associated signatures and attestations.
	// A fresh store.ImageStats is built inside the closure on every attempt,
	// not once outside it, so a failed attempt's partial layer/byte counts
	// aren't left for a retry to accumulate on top of. Only a successful
	// attempt publishes its stats pointer to the outer variable, so
	// formatAddedLine below reports the attempt that actually succeeded.
	var imageDigest string
	var stats *store.ImageStats
	err = retry.Operation(ctx, rso, ro, func() error {
		attemptStats := &store.ImageStats{}
		var addErr error
		imageDigest, addErr = s.AddImage(store.WithImageStats(ctx, attemptStats), r.Name(), platform, excludeExtras, pinnedDigest, insecureSkipTLSVerify, caFile)
		if addErr == nil {
			stats = attemptStats
		}
		return addErr
	})
	if err != nil {
		if ignoreErrors {
			log.BaseFromContext(ctx).Warnf("unable to add image [%s] to store: %v... skipping...", r.Name(), err)
			return nil
		} else if errors.Is(err, context.Canceled) {
			// Under errgroup.WithContext fail-fast (runImageJobs), one real
			// failure cancels every other in-flight image's context. Logging
			// this at ERROR would produce N-1 alarming lines for something
			// that isn't the actual failure -- the real error is reported by
			// whichever job's storeImage call hit it first.
			log.BaseFromContext(ctx).Debugf("unable to add image [%s] to store: %v", r.Name(), err)
			return err
		} else {
			log.BaseFromContext(ctx).Errorf("unable to add image [%s] to store: %v", r.Name(), err)
			return err
		}
	}

	if rewrite != "" {
		rawRewrite := rewrite
		rewrite = strings.TrimPrefix(rewrite, "/")
		if !strings.Contains(rewrite, ":") {
			if tag, ok := r.(name.Tag); ok {
				rewrite = rewrite + ":" + tag.TagStr()
			} else {
				return fmt.Errorf("cannot rewrite digest reference [%s] without an explicit tag in the rewrite", r.Name())
			}
		}
		// rename image name in store
		newRef, err := name.ParseReference(rewrite)
		if err != nil {
			return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
		}
		if err := rewriteReference(ctx, s, r, newRef, rawRewrite); err != nil {
			return err
		}
	}

	if auditLevel(ro) != "none" {
		e := audit.Entry{
			StoreID:   s.StoreID,
			Store:     s.Root,
			Type:      "image",
			Command:   "store add image",
			Args:      []string{i.Name},
			Reference: r.Name(),
			Digest:    imageDigest,
		}
		if auditLevel(ro) == "verbose" {
			sys := audit.BuildSystem()
			g := audit.BuildGlobal(ro, rso)
			e.System = &sys
			e.Global = &g
			e.Flags = map[string]any{
				"verified":                               verified,
				"platform":                               platform,
				"key":                                    i.Key,
				"use-tlog-verify":                        i.Tlog,
				"certificate-identity":                   i.CertIdentity,
				"certificate-identity-regexp":            i.CertIdentityRegexp,
				"certificate-oidc-issuer":                i.CertOidcIssuer,
				"certificate-oidc-issuer-regexp":         i.CertOidcIssuerRegexp,
				"certificate-github-workflow-repository": i.CertGithubWorkflowRepository,
				"ca-file":                                i.CaFile,
				"insecure-skip-tls-verify":               i.InsecureSkipTLSVerify,
				"rewrite":                                rewrite,
				"exclude-extras":                         excludeExtras,
			}
		}
		if err := audit.Append(ro.HaulerDir, e); err != nil {
			l.Warnf("failed to write audit entry: %v", err)
		}
		l.Debugf("generated audit id of [%s]", audit.ID())
	} else {
		l.Debugf("generated audit id of [none]")
	}

	log.BaseFromContext(ctx).Infof("%s", formatAddedLine(r.Name(), stats, time.Since(start)))
	return nil
}

func rewriteReference(ctx context.Context, s *store.Layout, oldRef name.Reference, newRef name.Reference, rawRewrite string) error {
	//TODO: improve string manipulation
	oldRefContext := oldRef.Context()
	newRefContext := newRef.Context()
	oldRepo := oldRefContext.RepositoryStr()
	newRepo := newRefContext.RepositoryStr()

	oldTag := oldRef.Identifier()
	if tag, ok := oldRef.(name.Tag); ok {
		oldTag = tag.TagStr()
	}
	newTag := newRef.Identifier()
	if tag, ok := newRef.(name.Tag); ok {
		newTag = tag.TagStr()
	}

	// ContainerdImageNameKey stores annotationRef.Name() verbatim, which includes the
	// "index.docker.io" prefix for docker.io images. Do not strip "index." here or the
	// comparison will never match images stored by writeImage/writeIndex.
	oldRegistry := oldRefContext.RegistryStr()
	newRegistry := newRefContext.RegistryStr()
	// If user omitted a registry in the rewrite string, go-containerregistry defaults to
	// index.docker.io. Preserve the original registry when the source is non-docker.
	if newRegistry == "index.docker.io" && !strings.HasPrefix(rawRewrite, "docker.io") && !strings.HasPrefix(rawRewrite, "index.docker.io") {
		newRegistry = oldRegistry
		newRepo = strings.TrimPrefix(newRepo, "library/") //if rewrite has library/ prefix in path it is stripped off unless registry specified in rewrite
	}
	oldTotal := oldRepo + ":" + oldTag
	newTotal := newRepo + ":" + newTag
	oldTotalReg := oldRegistry + "/" + oldTotal
	newTotalReg := newRegistry + "/" + newTotal

	log.BaseFromContext(ctx).Infof("rewriting [%s] to [%s]", oldTotalReg, newTotalReg)

	//find and update reference
	matched, err := s.OCI.UpdateAnnotations(
		func(d ocispec.Descriptor) bool {
			return d.Annotations[ocispec.AnnotationRefName] == oldTotal && d.Annotations[consts.ContainerdImageNameKey] == oldTotalReg
		},
		func(a map[string]string) {
			a[ocispec.AnnotationRefName] = newTotal
			a[consts.ContainerdImageNameKey] = newTotalReg
		},
	)
	if err != nil {
		return err
	}

	if matched == 0 {
		return fmt.Errorf("could not find image [%s] in store", oldRef.Name())
	}

	return nil
}

// imageregex parses image references starting with "image:" and with optional spaces or optional quotes
var imageRegex = regexp.MustCompile(`(?m)^[ \t-]*image:[ \t]*['"]?([^\s'"#]+)`)

// helmAnnotatedImage parses images references from helm chart annotations
type helmAnnotatedImage struct {
	Image string `yaml:"image"`
	Name  string `yaml:"name,omitempty"`
}

// imagesFromChartAnnotations parses image references from helm chart annotations
func imagesFromChartAnnotations(c *helmchart.Chart) ([]string, error) {
	if c == nil || c.Metadata == nil || c.Metadata.Annotations == nil {
		return nil, nil
	}

	// support multiple annotations
	keys := []string{
		"helm.sh/images",
		"images",
	}

	var out []string
	for _, k := range keys {
		raw, ok := c.Metadata.Annotations[k]
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}

		var items []helmAnnotatedImage
		if err := yaml.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("failed to parse helm chart annotation %q: %w", k, err)
		}

		for _, it := range items {
			img := strings.TrimSpace(it.Image)
			if img == "" {
				continue
			}
			img = strings.TrimPrefix(img, "/")
			out = append(out, img)
		}
	}

	slices.Sort(out)
	out = slices.Compact(out)

	return out, nil
}

// imagesFromImagesLock parses image references from images lock files in the chart directory
func imagesFromImagesLock(chartDir string) ([]string, error) {
	var out []string

	for _, name := range []string{
		"images.lock",
		"images-lock.yaml",
		"images.lock.yaml",
		".images.lock.yaml",
	} {
		p := filepath.Join(chartDir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		matches := imageRegex.FindAllSubmatch(b, -1)
		for _, m := range matches {
			if len(m) > 1 {
				out = append(out, string(m[1]))
			}
		}
	}

	if len(out) == 0 {
		return nil, nil
	}

	for i := range out {
		out[i] = strings.TrimPrefix(out[i], "/")
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return out, nil
}

func applyDefaultRegistry(img string, defaultRegistry string) (string, error) {
	img = strings.TrimSpace(strings.TrimPrefix(img, "/"))
	if img == "" || defaultRegistry == "" {
		return img, nil
	}

	ref, err := reference.Parse(img)
	if err != nil {
		return "", err
	}

	if ref.Context().RegistryStr() != "" {
		return img, nil
	}

	newRef, err := reference.Relocate(img, defaultRegistry)
	if err != nil {
		return "", err
	}

	return newRef.Name(), nil
}

// chartJob is the fully-resolved set of inputs needed to fetch and store a
// single chart; see resolveChartJobs.
type chartJob struct {
	cfg     v1.Chart
	opts    flags.AddChartOpts // held by value; ChartOpts is allocated per job
	rewrite string
	parent  string // "" for a top-level chart, else the parent chart's ref
	depth   int
}

// resolveChartJobs produces one top-level chartJob per entry in charts,
// applying the Charts precedence rules against the manifest's annotations and
// the sync flags. manifestDir is the directory holding the manifest, which
// each chart's relative valuesFiles paths resolve against. It reads chart
// credentials from the environment via resolveChartCreds, so it can fail
// before any chart is fetched if a chart's usernameEnv/passwordEnv pair is
// misconfigured.
//
// The three precedence rules are not uniform. registry is CLI > annotation.
// excludeExtras is a one-way switch that any of the three sources can flip on
// and none can flip off, since a plain bool has no unset state. platform is
// CLI > per-chart > annotation. That last rule must stay identical to
// resolveImageJobs's, or a single `hauler store sync` run would pull a
// chart's discovered images for a different platform than the manifest's own
// Images section.
//
// Every job allocates its own *action.ChartPathOptions. flags.AddChartOpts
// holds that as a pointer, so copying the struct alone would leave sibling
// jobs sharing one pointee, and a per-chart RepoURL/Version write would land
// in every other chart's options.
func resolveChartJobs(o *flags.SyncOpts, annotations map[string]string, manifestDir string, charts []v1.Chart) ([]chartJob, error) {
	registry := o.Registry
	if registry == "" {
		registry = annotations[consts.ImageAnnotationRegistry]
	}

	jobs := make([]chartJob, 0, len(charts))
	for _, ch := range charts {
		excludeExtras := resolveBoolFlag(ch.ExcludeExtras, annotations[consts.ImageAnnotationExcludeExtras] == "true", o.ExcludeExtras, o.ExcludeExtrasChanged)

		platform := o.Platform
		if o.Platform == "" {
			if ch.Platform != "" {
				platform = ch.Platform
			} else if annotations[consts.ImageAnnotationPlatform] != "" {
				platform = annotations[consts.ImageAnnotationPlatform]
			}
		}

		var valuesFiles []string
		for _, path := range ch.ValuesFiles {
			valuesFiles = append(valuesFiles, filepath.Join(manifestDir, path))
		}

		chartUsername, chartPassword, err := resolveChartCreds(ch)
		if err != nil {
			return nil, err
		}

		// caFile precedence: cli > per-chart > annotation.
		caFile := o.CaFile
		if caFile == "" {
			if ch.CaFile != "" {
				caFile = ch.CaFile
			} else if annotations[consts.ImageAnnotationCaFile] == "true" {
				caFile = annotations[consts.ImageAnnotationCaFile]
			}
		}

		// a CA file and skipping TLS verification are mutually exclusive: providing one forces verification on
		insecureSkipTLSVerify := o.CaFile == "" && resolveBoolFlag(ch.InsecureSkipTLSVerify, annotations[consts.ImageAnnotationInsecureSkipTLSVerify] == "true", o.InsecureSkipTLSVerify, o.InsecureChanged)

		jobs = append(jobs, chartJob{
			cfg: ch,
			opts: flags.AddChartOpts{
				ChartOpts: &action.ChartPathOptions{
					RepoURL:               ch.RepoURL,
					Version:               ch.Version,
					Verify:                ch.Verify,
					Keyring:               ch.Keyring,
					Username:              chartUsername,
					Password:              chartPassword,
					PassCredentialsAll:    ch.PassCredentialsAll,
					CertFile:              ch.CertFile,
					KeyFile:               ch.KeyFile,
					CaFile:                caFile,
					InsecureSkipTLSVerify: insecureSkipTLSVerify,
					PlainHTTP:             ch.PlainHTTP,
				},
				AddImages:       ch.AddImages,
				AddDependencies: ch.AddDependencies,
				ExcludeExtras:   excludeExtras,
				Registry:        registry,
				Platform:        platform,
				ValuesFiles:     valuesFiles,
			},
			rewrite: ch.Rewrite,
		})
	}

	return jobs, nil
}

// dedupeImageJobs collapses repeat pulls out of a chart tree's discovered
// images, keeping each (name, platform, excludeExtras) triple's first
// occurrence and the order it was first seen in. Platform and excludeExtras
// belong in the key because they change what storeImage actually fetches, not
// just how it is recorded.
//
// Precondition: every job is chart-discovered -- local is false and no
// verification field (needsPubKey, key, needsKeyless, certIdentity*, tlog) is
// set. Those fields are outside the key, so passing manifest-derived jobs here
// would silently drop a local Docker-daemon pull in favor of a same-named
// remote one, or let an unverified job displace one that would have had its
// signature checked. Chart image discovery has no syntax for either, which is
// why the key stays narrow.
func dedupeImageJobs(jobs []imageJob) []imageJob {
	type key struct {
		name          string
		platform      string
		excludeExtras bool
	}

	seen := make(map[key]struct{}, len(jobs))
	out := make([]imageJob, 0, len(jobs))
	for _, j := range jobs {
		k := key{name: j.img.Name, platform: j.platform, excludeExtras: j.excludeExtras}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, j)
	}

	return out
}

// maxChartDepth bounds how many dependency levels a chart tree is walked.
// The seen set already terminates repo-based cycles; this cap is what stops
// a graph whose every node is a *distinct* name|repo|version -- which the
// seen set cannot detect -- from walking forever. 10 is far past anything
// real: helm's own dependency trees bottom out within two or three levels.
const maxChartDepth = 10

// chartFetcher fetches one chart, returning the images and dependency charts
// it discovered. traverseChartLevels calls it concurrently.
type chartFetcher func(ctx context.Context, j chartJob) ([]imageJob, []chartJob, error)

// traverseChartLevels walks a chart dependency graph breadth-first, fetching
// each level concurrently (bounded by concurrency) before deriving the next,
// and returns every image discovered along the way plus the number of charts
// fetched.
//
// Level-by-level rather than depth-first because a level is the widest set of
// charts provably independent of each other: a dependency's inputs are not
// known until its parent has been fetched and expanded. It is also what makes
// runChartJobs' single shared temp root a requirement rather than a
// convenience -- see its doc comment.
//
// Cycles are cut by a seen set keyed on name|repoURL|version. A chart already
// scheduled is never scheduled again, which also collapses the common case of
// several siblings depending on the same subchart.
//
// Error semantics match runRemoteImageJobsWith: each level is one
// errgroup.WithContext with SetLimit(concurrency), so under --ignore-errors
// fetch returns nil and the walk continues, and otherwise the first failure
// cancels its level's derived context and surfaces verbatim.
func traverseChartLevels(ctx context.Context, jobs []chartJob, concurrency int, fetch chartFetcher) ([]imageJob, int, error) {
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		mu      sync.Mutex
		images  []imageJob
		deps    []chartJob
		fetched int
		level   = jobs
		seen    = make(map[string]bool, len(jobs))
		depth   int
	)

	for _, j := range level {
		seen[chartJobKey(j)] = true
	}

	for ; len(level) > 0 && depth < maxChartDepth; depth++ {
		deps = nil

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(concurrency)
		for _, j := range level {
			g.Go(func() error {
				gotImages, gotDeps, err := fetch(gctx, j)
				if err != nil {
					return err
				}
				mu.Lock()
				images = append(images, gotImages...)
				deps = append(deps, gotDeps...)
				fetched++
				mu.Unlock()
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, 0, err
		}

		next := make([]chartJob, 0, len(deps))
		for _, d := range deps {
			k := chartJobKey(d)
			if seen[k] {
				continue
			}
			seen[k] = true
			next = append(next, d)
		}
		level = next
	}

	// Truncation is silent otherwise: the charts still queued in level are
	// simply dropped, and nothing downstream would show them missing.
	if depth == maxChartDepth && len(level) > 0 {
		log.FromContext(ctx).Warnf("stopping chart dependency traversal at depth [%d]... [%d] dependent chart(s) not fetched", maxChartDepth, len(level))
	}

	return images, fetched, nil
}

// chartJobKey identifies a chart for the traversal's seen set. Name alone is
// not enough: the same chart name at two versions, or from two repositories,
// is two genuinely different pulls.
func chartJobKey(j chartJob) string {
	return j.cfg.Name + "|" + j.cfg.RepoURL + "|" + j.cfg.Version
}

// runChartJobs stores every chart in jobs and everything the tree below them
// references: dependency charts are walked breadth-first and concurrently by
// traverseChartLevels, then every image discovered along the way is pulled in
// one deduplicated pass by the same runner `store sync`'s Images documents
// use. Bounded by concurrency in both phases.
//
// One temp root serves the whole call, and its lifetime is a hard constraint
// rather than a convenience. A file:// dependency is named by a path *inside*
// its parent's expanded directory, and BFS fetches it only after the parent's
// entire level has returned -- so a per-chart temp dir removed when fetchChart
// returns would delete that path out from under the child. Charts are capped
// at ~1MB, so holding every expansion until the call ends is cheap.
//
// Only the chart traversal runs inside log.CaptureOutput. Helm's downloader can
// still print to stdout from a transitive dependency, and debug=true routes
// that to DEBUG -- silent at the default level, which is what the per-call
// os.Stdout swap deleted from pkg/content/chart achieved. The image phase is
// outside it because the capture is scoped to what Helm prints; hauler's own
// log lines never reach it either way, since log.NewLogger binds its writer at
// construction (pkg/log/log.go).
func runChartJobs(ctx context.Context, s *store.Layout, jobs []chartJob, concurrency int, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, progress *log.Renderer) error {
	if len(jobs) == 0 {
		return nil
	}
	if concurrency < 1 {
		concurrency = 1
	}

	l := log.FromContext(ctx)

	// rso.TempOverride is already resolved (flag or HAULER_TEMP_DIR) by Store().
	tempRoot, err := os.MkdirTemp(rso.TempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempRoot)

	// baseLogger is the logger every job's per-chart logger is derived from --
	// see runImageJobs's identical baseLogger for the full rationale. It is
	// built before CaptureOutput swaps os.Stdout so NewLogger's color decision
	// still sees the real terminal, and the Renderer holds the real *os.File
	// either way, so progress rows keep reaching the terminal rather than the
	// capture pipe.
	baseLogger := l
	if progress != nil {
		baseLogger = log.NewLogger(progress)
		progress.Start()
		defer progress.Stop()
	}

	fetch := func(fctx context.Context, j chartJob) ([]imageJob, []chartJob, error) {
		name := chartDisplayName(j.cfg.Name)
		// Invoked from inside traverseChartLevels' errgroup goroutine, so
		// Began lands after semaphore acquisition -- see runRemoteImageJobsWith.
		if progress != nil {
			progress.Began(name)
		}
		fields := log.Fields{"chart": name}
		if j.parent != "" {
			fields["parent"] = j.parent
		}
		jctx := baseLogger.With(fields).WithContext(fctx)
		jctx = log.WithBaseLogger(jctx, baseLogger)
		images, deps, err := fetchChart(jctx, s, j, tempRoot, rso, ro)
		if progress != nil {
			progress.Finished(name)
		}
		return images, deps, err
	}

	// CaptureOutput wraps a non-nil fn error as "function execution failed:
	// %w". Carrying the walk's error out in a variable keeps the one real
	// failure reaching the caller verbatim, as runImageJobs' does.
	var (
		discovered []imageJob
		charts     int
		walkErr    error
	)
	if err := log.CaptureOutput(baseLogger, true, func() error {
		discovered, charts, walkErr = traverseChartLevels(baseLogger.WithContext(ctx), jobs, concurrency, fetch)
		return nil
	}); err != nil {
		return err
	}
	if walkErr != nil {
		return walkErr
	}

	images := dedupeImageJobs(discovered)
	if len(images) == 0 {
		return nil
	}

	baseLogger.Infof("identified %d unique image(s) across %d chart(s)", len(images), charts)

	// Chart-discovered images are always remote and never carry verification
	// inputs, so they go straight to the remote runner -- no local Docker pass
	// and no verify pass to run first.
	return runRemoteImageJobsWith(ctx, s, images, concurrency, rso, ro, progress, baseLogger)
}

// chartDisplayName is the short name used for a chart's progress row and
// "chart=" log field. A file:// dependency's job is named by a filesystem
// path into its parent's expansion, which is neither short nor stable across
// runs, so those collapse to the basename.
func chartDisplayName(name string) string {
	if strings.Contains(name, string(os.PathSeparator)) {
		return filepath.Base(name)
	}
	return name
}

// fetchChart stores one chart and reports what it references: the images
// discovered inside it when --add-images, and the dependency charts to walk
// next when --add-dependencies. It does not recurse -- traverseChartLevels
// owns the walk -- and it never writes through j.opts.ChartOpts, which is what
// lets sibling jobs run concurrently.
//
// tempRoot is runChartJobs' shared root. The chart expands into a fresh
// subdirectory of it, and both dependency branches resolve against that
// subdirectory, so it must outlive this call; see runChartJobs.
func fetchChart(ctx context.Context, s *store.Layout, j chartJob, tempRoot string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) ([]imageJob, []chartJob, error) {
	l := log.FromContext(ctx)

	start := time.Now()
	ignoreErrors := flags.ShouldIgnoreErrors(ro)
	displayName := chartDisplayName(j.cfg.Name)

	if err := ctx.Err(); err != nil {
		log.BaseFromContext(ctx).Debugf("skipping chart [%s]: %v", displayName, err)
		return nil, nil, err
	}

	log.BaseFromContext(ctx).Debugf("adding chart [%s] to the store", displayName)

	chrt, err := chart.NewChart(j.cfg.Name, j.opts.ChartOpts)
	if err != nil {
		return nil, nil, err
	}

	c, err := chrt.Load()
	if err != nil {
		return nil, nil, err
	}

	ref, err := reference.NewTagged(c.Name(), c.Metadata.Version)
	if err != nil {
		return nil, nil, err
	}

	var chartDesc ocispec.Descriptor
	err = retry.Operation(ctx, rso, ro, func() error {
		var addErr error
		chartDesc, addErr = s.AddArtifact(ctx, chrt, ref.Name())
		return addErr
	})
	if err != nil {
		if ignoreErrors {
			log.BaseFromContext(ctx).Warnf("unable to add chart [%s] to store: %v... skipping...", ref.Name(), err)
			return nil, nil, nil
		} else if errors.Is(err, context.Canceled) {
			// Under traverseChartLevels' fail-fast errgroup, one real failure
			// cancels every other in-flight chart's context -- see
			// storeImage's identical branch for the full rationale.
			log.BaseFromContext(ctx).Debugf("unable to add chart [%s] to store: %v", ref.Name(), err)
			return nil, nil, err
		}
		log.BaseFromContext(ctx).Errorf("unable to add chart [%s] to store: %v", ref.Name(), err)
		return nil, nil, err
	}

	if j.rewrite != "" {
		if err := rewriteChartReference(ctx, s, ref, j.rewrite); err != nil {
			return nil, nil, err
		}
	}

	if auditLevel(ro) != "none" {
		e := audit.Entry{
			StoreID:   s.StoreID,
			Store:     s.Root,
			Type:      "chart",
			Command:   "store add chart",
			Args:      []string{c.Name()},
			Reference: c.Name() + ":" + c.Metadata.Version,
			Digest:    chartDesc.Digest.String(),
		}
		if auditLevel(ro) == "verbose" {
			sys := audit.BuildSystem()
			g := audit.BuildGlobal(ro, rso)
			e.System = &sys
			e.Global = &g
			e.Flags = map[string]any{
				"repo":                     audit.SanitizeURL(j.cfg.RepoURL),
				"version":                  c.Metadata.Version,
				"rewrite":                  j.rewrite,
				"add-images":               j.opts.AddImages,
				"add-dependencies":         j.opts.AddDependencies,
				"exclude-extras":           j.opts.ExcludeExtras,
				"values":                   j.opts.ValuesFiles,
				"platform":                 j.opts.Platform,
				"registry":                 j.opts.Registry,
				"kube-version":             j.opts.KubeVersion,
				"verify":                   j.opts.ChartOpts.Verify,
				"insecure-skip-tls-verify": j.opts.ChartOpts.InsecureSkipTLSVerify,
				"ca-file":                  j.opts.ChartOpts.CaFile,
				"cert-file":                j.opts.ChartOpts.CertFile,
				"key-file":                 j.opts.ChartOpts.KeyFile,
			}
		}
		if err := audit.Append(ro.HaulerDir, e); err != nil {
			l.Warnf("failed to write audit entry: %v", err)
		}
		l.Debugf("generated audit id of [%s]", audit.ID())
	} else {
		l.Debugf("generated audit id of [none]")
	}

	chartPath := chrt.Path()
	chartPathInfo, err := os.Stat(chartPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to stat chart path '%s': %w", chartPath, err)
	}
	if !chartPathInfo.IsDir() {
		// A subdirectory per job, not <tempRoot>/<chart name>: two concurrent
		// jobs can be the same chart at different versions, and both expand
		// into a directory named for the chart.
		expandDir, err := os.MkdirTemp(tempRoot, "chart-")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create temp dir: %w", err)
		}
		l.Debugf("extracting chart archive [%s]", filepath.Base(chartPath))
		if err := util.ExpandFile(expandDir, chartPath); err != nil {
			return nil, nil, fmt.Errorf("failed to extract chart: %w", err)
		}

		// expanded chart should be in a directory matching the chart name
		expectedChartDir := filepath.Join(expandDir, c.Name())
		if _, err := os.Stat(expectedChartDir); err != nil {
			return nil, nil, fmt.Errorf("chart archive did not expand into expected directory '%s': %w", c.Name(), err)
		}
		chartPath = expectedChartDir
	}

	var imageJobs []imageJob
	if j.opts.AddImages {
		userValues := map[string]any{}

		for _, valuesFile := range j.opts.ValuesFiles {
			l.Debugf("loading values for chart [%s]", valuesFile)

			valuesContent, err := os.ReadFile(valuesFile)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read values file [%s]: %w", valuesFile, err)
			}

			vals, err := loader.LoadValues(bytes.NewReader(valuesContent))
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read helm values file [%s]: %w", valuesFile, err)
			}

			userValues = loader.MergeMaps(userValues, vals)
		}

		// set helm default capabilities
		caps := common.DefaultCapabilities.Copy()

		// only parse and override if provided kube version
		if j.opts.KubeVersion != "" {
			kubeVersion, err := common.ParseKubeVersion(j.opts.KubeVersion)
			if err != nil {
				l.Warnf("invalid kube-version [%s], using default kubernetes version", j.opts.KubeVersion)
			} else {
				caps.KubeVersion = *kubeVersion
			}
		}

		// Reload a fresh chart for rendering so ProcessDependencies can safely
		// rename aliased deps / drop disabled ones without mutating the chart
		// used later by --add-dependencies.
		renderChart, err := loader.Load(chrt.Path())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to reload chart for image discovery: %w", err)
		}

		// Match helm install/template: coalesce parent values into subcharts
		// (including dependency aliases) and honor conditions before rendering.
		if err := util.ProcessDependencies(renderChart, userValues); err != nil {
			return nil, nil, fmt.Errorf("failed to process chart dependencies for image discovery: %w", err)
		}

		values, err := commonutil.ToRenderValues(renderChart, userValues, common.ReleaseOptions{Namespace: "hauler"}, caps)
		if err != nil {
			return nil, nil, err
		}

		// helper for normalization and deduping slices
		normalizeUniq := func(in []string) []string {
			if len(in) == 0 {
				return nil
			}
			for i := range in {
				in[i] = strings.TrimPrefix(in[i], "/")
			}
			slices.Sort(in)
			return slices.Compact(in)
		}

		// Collect images by method so we can debug counts
		var (
			templateImages   []string
			annotationImages []string
			lockImages       []string
		)

		// parse helm chart templates and values for images
		rendered, err := engine.Render(renderChart, values)
		if err != nil {
			// charts may fail due to values so still try helm chart annotations and lock
			l.Warnf("failed to render chart [%s]: %v", c.Name(), err)
			rendered = map[string]string{}
		}

		for _, manifest := range rendered {
			matches := imageRegex.FindAllStringSubmatch(manifest, -1)
			for _, match := range matches {
				if len(match) > 1 {
					templateImages = append(templateImages, match[1])
				}
			}
		}

		// parse helm chart annotations for images
		annotationImages, err = imagesFromChartAnnotations(c)
		if err != nil {
			l.Warnf("failed to parse helm chart annotation for [%s:%s]: %v", c.Name(), c.Metadata.Version, err)
			annotationImages = nil
		}

		// parse images lock files for images
		lockImages, err = imagesFromImagesLock(chartPath)
		if err != nil {
			l.Warnf("failed to parse images lock: %v", err)
			lockImages = nil
		}

		// normalization and deduping the slices
		templateImages = normalizeUniq(templateImages)
		annotationImages = normalizeUniq(annotationImages)
		lockImages = normalizeUniq(lockImages)

		// merge all sources then final dedupe
		images := append(append(templateImages, annotationImages...), lockImages...)
		images = normalizeUniq(images)

		l.Debugf("image references identified for helm template: [%d] image(s)", len(templateImages))
		l.Debugf("image references identified for helm chart annotations: [%d] image(s)", len(annotationImages))
		l.Debugf("image references identified for helm image lock file: [%d] image(s)", len(lockImages))
		l.Debugf("successfully parsed and deduped image references: [%d] image(s)", len(images))
		l.Debugf("successfully parsed image references %v", images)

		if len(images) > 0 {
			log.BaseFromContext(ctx).Infof("identified [%d] image(s) in [%s:%s]", len(images), c.Name(), c.Metadata.Version)
		}

		for _, image := range images {
			relocated, err := applyDefaultRegistry(image, j.opts.Registry)
			if err != nil {
				if ignoreErrors {
					l.Warnf("unable to apply registry to image [%s]: %v... skipping...", image, err)
					continue
				}
				return nil, nil, fmt.Errorf("unable to apply registry to image [%s]: %w", image, err)
			}

			// Chart-discovered images inherit the chart's own TLS settings --
			// there is no separate per-discovered-image TLS knob in a chart
			// manifest, so the registry a chart's images live in is assumed
			// to share the chart repo's trust configuration.
			imageJobs = append(imageJobs, imageJob{
				img: v1.Image{
					Name:                  relocated,
					CaFile:                j.opts.ChartOpts.CaFile,
					InsecureSkipTLSVerify: j.opts.ChartOpts.InsecureSkipTLSVerify,
				},
				platform:      j.opts.Platform,
				excludeExtras: j.opts.ExcludeExtras,
			})
		}
	}

	var deps []chartJob
	if j.opts.AddDependencies {
		for _, dep := range c.Metadata.Dependencies {
			l.Infof("adding dependent chart [%s:%s]", dep.Name, dep.Version)

			depOpts := j.opts
			depOpts.AddDependencies = true
			// Do not rediscover images on dependency charts in isolation.
			// Parent --add-images already renders the full tree (with alias
			// overrides and conditions) after ProcessDependencies.
			depOpts.AddImages = false

			// depOpts is a struct copy, so it still points at the parent's
			// *action.ChartPathOptions; the RepoURL/Version writes below would
			// otherwise land in the parent's options -- a data race against
			// whatever sibling job is reading them. Copying the value carries
			// the parent's auth/TLS settings over without the aliasing.
			depChartOpts := *j.opts.ChartOpts
			depOpts.ChartOpts = &depChartOpts

			var depCfg v1.Chart
			if strings.HasPrefix(dep.Repository, "file://") || dep.Repository == "" {
				// A file:// subchart is already unpacked inside the parent's
				// expansion, so it is addressed by path with nothing left to
				// resolve from a repository.
				subchartPath := filepath.Join(chartPath, "charts", dep.Name)

				depCfg = v1.Chart{Name: subchartPath}
				depChartOpts.RepoURL = ""
				depChartOpts.Version = ""
			} else {
				depCfg = v1.Chart{Name: dep.Name, RepoURL: dep.Repository, Version: dep.Version}
				depChartOpts.RepoURL = dep.Repository
				depChartOpts.Version = dep.Version
			}

			deps = append(deps, chartJob{
				cfg:    depCfg,
				opts:   depOpts,
				parent: ref.Name(),
				depth:  j.depth + 1,
			})
		}
	}

	// Chart.Layers() always returns exactly one layer, the chart archive
	// itself. Re-deriving it costs a re-read (and, for an already-expanded
	// directory chart, a re-tar) of at most ~1MB; anything unexpected falls
	// back to nil stats and formatAddedLine's elapsed-only form.
	var stats *store.ImageStats
	if layers, layersErr := chrt.Layers(); layersErr == nil && len(layers) == 1 {
		if size, sizeErr := layers[0].Size(); sizeErr == nil {
			stats = &store.ImageStats{}
			stats.Layers.Store(1)
			stats.Bytes.Store(size)
		}
	}

	log.BaseFromContext(ctx).Infof("%s", formatAddedLine(ref.Name(), stats, time.Since(start)))

	return imageJobs, deps, nil
}

// rewriteChartReference retags a stored chart's index entry from ref to
// rewrite. A rewrite that omits a tag inherits ref's.
func rewriteChartReference(ctx context.Context, s *store.Layout, ref name.Reference, rewrite string) error {
	rewrite = strings.TrimPrefix(rewrite, "/")
	newRef, err := name.ParseReference(rewrite)
	if err != nil {
		// error... don't continue with a bad reference
		return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
	}

	// if rewrite omits a tag... keep the existing tag
	oldTag := ref.Identifier()
	if tag, ok := ref.(name.Tag); ok {
		oldTag = tag.TagStr()
	}
	if !strings.Contains(rewrite, ":") {
		rewrite = strings.Join([]string{rewrite, oldTag}, ":")
		newRef, err = name.ParseReference(rewrite)
		if err != nil {
			return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
		}
	}

	// rename chart name in store
	oldRepo := ref.Context().RepositoryStr()
	newRepo := newRef.Context().RepositoryStr()
	newTag := newRef.Identifier()
	if tag, ok := newRef.(name.Tag); ok {
		newTag = tag.TagStr()
	}

	oldTotal := oldRepo + ":" + oldTag
	newTotal := newRepo + ":" + newTag

	log.BaseFromContext(ctx).Debugf("rewriting [%s] to [%s]", oldTotal, newTotal)

	matched, err := s.OCI.UpdateAnnotations(
		func(d ocispec.Descriptor) bool {
			return d.Annotations[ocispec.AnnotationRefName] == oldTotal
		},
		func(a map[string]string) {
			a[ocispec.AnnotationRefName] = newTotal
		},
	)
	if err != nil {
		return err
	}

	if matched == 0 {
		return fmt.Errorf("could not find chart [%s] in store", ref.Name())
	}

	return nil
}
