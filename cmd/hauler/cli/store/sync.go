package store

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/mitchellh/go-homedir"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/util/yaml"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/artifacts/file"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/cosign"
	"hauler.dev/go/hauler/v2/pkg/getter"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/retry"
	"hauler.dev/go/hauler/v2/pkg/store"
)

func SyncCmd(ctx context.Context, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	// Handle dry-run before any local side effects (temp dirs, store writes).
	if o.DryRun {
		for _, productName := range o.Products {
			parts := strings.Split(productName, "=")
			tag := strings.ReplaceAll(parts[1], "+", "-")

			ProductRegistry := o.ProductRegistry
			if o.ProductRegistry == "" {
				ProductRegistry = consts.CarbideRegistry
			}

			manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", ProductRegistry, parts[0], tag)
			fileName := fmt.Sprintf("%s-manifest.yaml", parts[0])

			parsedRef, err := gname.ParseReference(manifestLoc)
			if err != nil {
				return fmt.Errorf("failed to fetch product manifest for [%s]: %w", productName, err)
			}
			remoteImg, err := remote.Image(parsedRef,
				remote.WithAuthFromKeychain(authn.DefaultKeychain),
				remote.WithContext(ctx),
			)
			if err != nil {
				return fmt.Errorf("failed to fetch product manifest for [%s]: %w", productName, err)
			}
			mf, err := remoteImg.Manifest()
			if err != nil {
				return err
			}
			// Select the layer whose AnnotationTitle matches the expected
			// manifest filename, rather than assuming layer order.
			var layerDigest *gv1.Hash
			for _, desc := range mf.Layers {
				if desc.Annotations[ocispec.AnnotationTitle] == fileName {
					layerDigest = &desc.Digest
					break
				}
			}
			if layerDigest == nil {
				return fmt.Errorf("product manifest for [%s] has no layer with title %q", productName, fileName)
			}
			layer, err := remoteImg.LayerByDigest(*layerDigest)
			if err != nil {
				return err
			}
			rc, err := layer.Compressed()
			if err != nil {
				return err
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}

			// Ensure each manifest starts with a YAML document separator.
			if !strings.HasPrefix(string(content), "---") {
				content = append([]byte("---\n"), content...)
			}
			if _, err := os.Stdout.Write(content); err != nil {
				return err
			}
		}
		return nil
	}

	// caches stores opened via hauler.dev/store, keyed by abs path, so docs sharing a target reuse one Layout
	targetStores := map[string]*store.Layout{}

	// Everything below runs with a real store (s != nil; the dry-run branch
	// above already returned). Force one durable index checkpoint at the end
	// of the run since the per-artifact path only fsyncs on
	// indexCheckpointInterval -- deferred so it still runs on error paths,
	// where a partially-populated index is worth persisting. This does NOT
	// run on Ctrl-C (no signal handler is installed), which is fine: process
	// death doesn't lose page cache, so the index still reaches disk. Covers
	// any target stores from hauler.dev/store too, not just the primary one.
	defer func() {
		if err := s.OCI.SaveIndex(); err != nil {
			l.Warnf("failed to save index at end of sync: %v", err)
		}
		l.Debugf("%s", formatIOStats(s.OCI.Stats().Snapshot(), s.OCI.BlobConcurrency()))

		for _, ts := range targetStores {
			if err := ts.OCI.SaveIndex(); err != nil {
				l.Warnf("failed to save index for target store [%s]: %v", ts.Root, err)
			}
			l.Debugf("%s", formatIOStats(ts.OCI.Stats().Snapshot(), ts.OCI.BlobConcurrency()))
		}
	}()

	// rso.TempOverride is already resolved (flag or HAULER_TEMP_DIR) by Store().
	tempDir, err := os.MkdirTemp(rso.TempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	l.Debugf("using temporary directory at [%s]", tempDir)

	// if passed products, check for a remote manifest to retrieve and use
	for _, productName := range o.Products {
		l.Infof("processing product manifest for [%s] to store [%s]", productName, o.StoreDir)
		parts := strings.Split(productName, "=")
		tag := strings.ReplaceAll(parts[1], "+", "-")

		ProductRegistry := o.ProductRegistry // cli flag
		// if no cli flag use CarbideRegistry.
		if o.ProductRegistry == "" {
			ProductRegistry = consts.CarbideRegistry
		}

		manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", ProductRegistry, parts[0], tag)
		l.Infof("fetching product manifest from [%s]", manifestLoc)

		img := v1.Image{
			Name:                  manifestLoc,
			InsecureSkipTLSVerify: o.InsecureSkipTLSVerify,
			CaFile:                o.CaFile,
		}
		err := storeImage(ctx, s, img, o.Platform, o.ExcludeExtras, rso, ro, "", "", false)
		if err != nil {
			return fmt.Errorf("failed to fetch product manifest for [%s]: %w", productName, err)
		}
		err = ExtractCmd(ctx, &flags.ExtractOpts{StoreRootOpts: o.StoreRootOpts}, s, fmt.Sprintf("hauler/%s-manifest.yaml:%s", parts[0], tag))
		if err != nil {
			return err
		}
		fileName := fmt.Sprintf("%s-manifest.yaml", parts[0])
		fi, err := os.Open(fileName)
		if err != nil {
			return err
		}
		defer fi.Close()
		err = processContent(ctx, fi, o, s, rso, ro, targetStores)
		if err != nil {
			return err
		}
		l.Infof("processing completed successfully")
	}

	// If passed a hauler manifest, process it
	if len(o.FileName) != 0 {
		for _, fileName := range o.FileName {
			l.Infof("processing manifest [%s] to store [%s]", fileName, o.StoreDir)

			haulPath := fileName
			if strings.HasPrefix(haulPath, "http://") || strings.HasPrefix(haulPath, "https://") {
				l.Debugf("detected remote manifest... starting download... [%s]", haulPath)

				h := getter.NewHttp(o.InsecureSkipTLSVerify, o.CaFile)
				parsedURL, err := url.Parse(haulPath)
				if err != nil {
					return err
				}
				rc, err := h.Open(ctx, parsedURL)
				if err != nil {
					return err
				}
				defer rc.Close()

				fileName := h.Name(parsedURL)
				if fileName == "" {
					fileName = filepath.Base(parsedURL.Path)
				}
				haulPath = filepath.Join(tempDir, fileName)

				out, err := os.Create(haulPath)
				if err != nil {
					return err
				}
				defer out.Close()

				if _, err = io.Copy(out, rc); err != nil {
					return err
				}
			}

			fi, err := os.Open(haulPath)
			if err != nil {
				return err
			}
			defer fi.Close()

			err = processContent(ctx, fi, o, s, rso, ro, targetStores)
			if err != nil {
				return err
			}

			l.Infof("processing completed successfully")
		}
	}

	// If passed an image.txt file, process it
	if len(o.ImageTxt) != 0 {
		for _, imageTxt := range o.ImageTxt {
			l.Infof("processing image.txt [%s] to store [%s]", imageTxt, o.StoreDir)

			haulPath := imageTxt
			if strings.HasPrefix(haulPath, "http://") || strings.HasPrefix(haulPath, "https://") {
				l.Debugf("detected remote image.txt... starting download... [%s]", haulPath)

				h := getter.NewHttp(o.InsecureSkipTLSVerify, o.CaFile)
				parsedURL, err := url.Parse(haulPath)
				if err != nil {
					return err
				}
				rc, err := h.Open(ctx, parsedURL)
				if err != nil {
					return err
				}
				defer rc.Close()

				fileName := h.Name(parsedURL)
				if fileName == "" {
					fileName = filepath.Base(parsedURL.Path)
				}
				haulPath = filepath.Join(tempDir, fileName)

				out, err := os.Create(haulPath)
				if err != nil {
					return err
				}
				defer out.Close()

				if _, err = io.Copy(out, rc); err != nil {
					return err
				}
			}

			fi, err := os.Open(haulPath)
			if err != nil {
				return err
			}
			defer fi.Close()

			err = processImageTxt(ctx, fi, o, s, rso, ro)
			if err != nil {
				return err
			}

			l.Infof("processing completed successfully")
		}
	}

	return nil
}

// resolveBoolFlag applies CLI-first precedence for a plain-bool flag: an
// explicitly-set CLI flag wins outright (even when false); otherwise the flag
// is on if the resolved CLI value (e.g. from an env var), the per-item field,
// or the annotation is true. Plain bools have no unset state, so per-item and
// annotation can only turn a flag on, never force it back off.
func resolveBoolFlag(item, annTrue, global, cliChanged bool) bool {
	if cliChanged {
		return global
	}
	return global || item || annTrue
}

func processContent(ctx context.Context, fi *os.File, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, targetStores map[string]*store.Layout) error {
	l := log.FromContext(ctx)

	reader := yaml.NewYAMLReader(bufio.NewReader(fi))

	var docs [][]byte
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		docs = append(docs, raw)
	}

	for _, doc := range docs {
		obj, err := content.Load(doc)
		if err != nil {
			l.Warnf("skipping syncing due to %v", err)
			continue
		}

		gvk := obj.GroupVersionKind()

		switch gvk.Kind {

		case consts.FilesContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Files
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				a := cfg.GetAnnotations()
				docStore, err := resolveTargetStore(ctx, a, s, rso, ro, targetStores, o.StoreChanged)
				if err != nil {
					return err
				}
				docRso, err := resolveDocRetries(a, rso, o.RetriesChanged)
				if err != nil {
					return err
				}
				l.Infof("syncing content [%s] with [kind=%s] to store [%s]", gvk.GroupVersion(), gvk.Kind, docStore.Root)
				jobs := resolveFileJobs(o, a, cfg.Spec.Files)
				if err := runFileJobs(ctx, docStore, jobs, o.Concurrency, docRso, ro, newSyncProgress(o, ro)); err != nil {
					return err
				}

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1]", gvk.Version, gvk.Kind)
			}

		case consts.ImagesContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Images
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				a := cfg.GetAnnotations()
				docStore, err := resolveTargetStore(ctx, a, s, rso, ro, targetStores, o.StoreChanged)
				if err != nil {
					return err
				}
				docRso, err := resolveDocRetries(a, rso, o.RetriesChanged)
				if err != nil {
					return err
				}
				l.Infof("syncing content [%s] with [kind=%s] to store [%s]", gvk.GroupVersion(), gvk.Kind, docStore.Root)
				jobs, err := resolveImageJobs(o, a, cfg.Spec.Images)
				if err != nil {
					return err
				}
				if err := runImageJobs(ctx, docStore, jobs, o.Concurrency, docRso, ro, newSyncProgress(o, ro)); err != nil {
					return err
				}

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1]", gvk.Version, gvk.Kind)
			}

		case consts.ChartsContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Charts
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				a := cfg.GetAnnotations()
				docStore, err := resolveTargetStore(ctx, a, s, rso, ro, targetStores, o.StoreChanged)
				if err != nil {
					return err
				}
				docRso, err := resolveDocRetries(a, rso, o.RetriesChanged)
				if err != nil {
					return err
				}
				l.Infof("syncing content [%s] with [kind=%s] to store [%s]", gvk.GroupVersion(), gvk.Kind, docStore.Root)
				jobs, err := resolveChartJobs(o, a, filepath.Dir(fi.Name()), cfg.Spec.Charts)
				if err != nil {
					return err
				}
				if err := runChartJobs(ctx, docStore, jobs, o.Concurrency, docRso, ro, newSyncProgress(o, ro)); err != nil {
					return err
				}

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1]", gvk.Version, gvk.Kind)
			}

		default:
			return fmt.Errorf("unsupported kind [%s]... valid kinds are [Files, Images, Charts]", gvk.Kind)
		}
	}
	return nil
}

// resolveTargetStore picks a doc's store based on its hauler.dev/store annotation,
// falling back to def. Opens (or reuses, via targetStores) the target store otherwise.
// An explicit CLI --store wins: cliStoreSet short-circuits to def, ignoring the annotation.
func resolveTargetStore(ctx context.Context, a map[string]string, def *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, targetStores map[string]*store.Layout, cliStoreSet bool) (*store.Layout, error) {
	if cliStoreSet {
		return def, nil
	}

	target := a[consts.AnnotationTargetStore]
	if target == "" {
		return def, nil
	}

	abs, err := flags.ResolveStoreDir(ctx, ro, target)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target store [%s]: %w", target, err)
	}

	if abs == def.Root {
		return def, nil
	}

	if ts, ok := targetStores[abs]; ok {
		return ts, nil
	}

	// only overriding StoreDir, everything else still comes from rso
	altOpts := *rso
	altOpts.StoreDir = abs
	ts, err := altOpts.Store(ctx, ro)
	if err != nil {
		return nil, fmt.Errorf("failed to open target store [%s]: %w", target, err)
	}

	targetStores[abs] = ts
	return ts, nil
}

// resolveDocRetries returns a copy of rso with Retries overridden by a doc's
// hauler.dev/retries annotation, or rso unchanged if it's not set. Copy, not
// mutation, so it can't leak into a sibling doc. An explicit CLI --retries
// wins: cliRetriesSet returns rso unchanged, ignoring the annotation.
func resolveDocRetries(a map[string]string, rso *flags.StoreRootOpts, cliRetriesSet bool) (*flags.StoreRootOpts, error) {
	if cliRetriesSet {
		return rso, nil
	}

	v, ok := a[consts.AnnotationRetries]
	if !ok || v == "" {
		return rso, nil
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q: %w", consts.AnnotationRetries, v, err)
	}
	if n < 0 {
		return nil, fmt.Errorf("%s must be >= 0, got %d", consts.AnnotationRetries, n)
	}
	if n == 0 {
		n = consts.DefaultRetries
	}

	docRso := *rso
	docRso.Retries = n
	return &docRso, nil
}

// resolveChartCreds reads credentials for a Chart entry from the env vars
// named by UsernameEnv and PasswordEnv.  Both fields must be set or both must
// be empty; a mix is a configuration error.  If both are set, the env vars
// must be non-empty at runtime.
func resolveChartCreds(ch v1.Chart) (username, password string, err error) {
	if ch.UsernameEnv == "" && ch.PasswordEnv == "" {
		return "", "", nil
	}
	if ch.UsernameEnv == "" || ch.PasswordEnv == "" {
		return "", "", fmt.Errorf("chart %q: usernameEnv and passwordEnv must both be set or both be empty", ch.Name)
	}
	username = os.Getenv(ch.UsernameEnv)
	password = os.Getenv(ch.PasswordEnv)
	if username == "" || password == "" {
		return "", "", fmt.Errorf("chart %q: env vars %q and %q must both be set and non-empty", ch.Name, ch.UsernameEnv, ch.PasswordEnv)
	}
	return username, password, nil
}

func processImageTxt(ctx context.Context, fi *os.File, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)
	l.Infof("syncing images from [%s] to store", filepath.Base(fi.Name()))
	var jobs []imageJob
	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		l.Debugf("adding image [%s] to the store [%s]", line, o.StoreDir)
		jobs = append(jobs, imageJob{
			img: v1.Image{
				Name:                  line,
				CaFile:                o.CaFile,
				InsecureSkipTLSVerify: o.InsecureSkipTLSVerify,
			},
			platform:      o.Platform,
			excludeExtras: o.ExcludeExtras,
		})
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return runImageJobs(ctx, s, jobs, o.Concurrency, rso, ro, newSyncProgress(o, ro))
}

// newSyncProgress returns a live progress Renderer over os.Stdout when
// eligible (see log.ShouldShowProgress), or nil otherwise; runImageJobs
// treats a nil progress as a no-op.
//
// The session spans verification, which runs inside the pull worker. A live
// session survives a concurrent log.CaptureOutput regardless: log.NewLogger
// binds its writer once at construction (pkg/log/log.go) and the Renderer holds
// the real *os.File, so CaptureOutput's swap of the os.Stdout/os.Stderr package
// variables reaches neither. runChartJobs depends on that, running its Helm
// capture inside a live session.
func newSyncProgress(o *flags.SyncOpts, ro *flags.CliRootOpts) *log.Renderer {
	return newProgressRenderer(o.NoProgress, ro.LogLevel)
}

// newProgressRenderer returns a live progress Renderer when the run is
// eligible (see log.ShouldShowProgress), or nil otherwise; the run* helpers
// treat nil as "no progress display".
func newProgressRenderer(noProgress bool, logLevel string) *log.Renderer {
	if !log.ShouldShowProgress(noProgress, logLevel) {
		return nil
	}
	return log.NewRenderer(os.Stdout)
}

// formatIOStats renders one line summarizing a sync's disk contention.
// ceiling is the store's configured blob-write limit, so peak-inflight
// reads as a fraction of what was permitted rather than a bare number.
//
// blobs/written/cached/bytes cover only the WriteBlob path (store.AddImage
// and friends); a registry push shares the same blob semaphore without
// calling WriteBlob, so those counters can read zero on a store-to-store
// copy. blobsem-wait sums wait time across every goroutine that touched the
// semaphore, not wall-clock, so it can exceed the run's total duration.
func formatIOStats(st content.IOStatsSnapshot, ceiling int) string {
	return fmt.Sprintf(
		"io stats: blobs=%d written=%d cached=%d bytes=%s peak-inflight=%d/%d blobsem-wait=%s index-writes=%d durable=%d index-bytes=%s index-lock-wait=%s",
		st.BlobsWritten+st.BlobsCached,
		st.BlobsWritten,
		st.BlobsCached,
		humanize.Bytes(uint64(st.BlobBytesWritten)),
		st.BlobPeakInFlight,
		ceiling,
		st.BlobSemWait.Round(time.Millisecond),
		st.IndexWrites,
		st.IndexDurableWrites,
		humanize.Bytes(uint64(st.IndexBytesWritten)),
		st.IndexLockWait.Round(time.Millisecond),
	)
}

// imageJob is the fully-resolved set of inputs needed to verify (if
// applicable) and store a single image; see resolveImageJobs.
type imageJob struct {
	img           v1.Image // Name already relocated to the target registry if applicable
	platform      string
	excludeExtras bool
	rewrite       string
	local         bool

	// resolved verification inputs, collapsed into a cosign.Config by
	// verifyConfig and consumed by the pull worker
	needsPubKey, needsKeyless            bool
	key                                  string
	tlog                                 bool
	certIdentity, certIdentityRegexp     string
	certOidcIssuer, certOidcIssuerRegexp string
	certGithubWorkflowRepository         string
}

// resolveImageJobs applies the precedence rules (CLI > per-image > annotation)
// to every image in images, producing one imageJob per image. For the boolean
// flags (tlog, exclude-extras, insecure-skip-tls-verify) an explicit CLI flag
// wins outright; otherwise per-image or annotation can only turn them on (see
// resolveBoolFlag). It is pure -- cosign verification happens later, inside the
// pull worker; see resolveAndVerify.
func resolveImageJobs(o *flags.SyncOpts, a map[string]string, images []v1.Image) ([]imageJob, error) {
	var jobs []imageJob

	for _, i := range images {
		if !i.Local && (a[consts.ImageAnnotationRegistry] != "" || o.Registry != "") {
			newRef, _ := reference.Parse(i.Name)
			newReg := o.Registry
			if o.Registry == "" && a[consts.ImageAnnotationRegistry] != "" {
				newReg = a[consts.ImageAnnotationRegistry]
			}
			if newRef.Context().RegistryStr() == "" {
				var relErr error
				newRef, relErr = reference.Relocate(i.Name, newReg)
				if relErr != nil {
					return nil, relErr
				}
			}
			i.Name = newRef.Name()
		}

		// caFile precedence: cli >per-image > annotation.
		if o.CaFile == "" && i.CaFile == "" && a[consts.ImageAnnotationCaFile] != "" {
			i.CaFile = a[consts.ImageAnnotationCaFile]
		} else if o.CaFile != "" {
			i.CaFile = o.CaFile
		}

		// a CA file and skipping TLS verification are mutually exclusive: providing one forces verification on
		i.InsecureSkipTLSVerify = o.CaFile == "" && resolveBoolFlag(i.InsecureSkipTLSVerify, a[consts.ImageAnnotationInsecureSkipTLSVerify] == "true", o.InsecureSkipTLSVerify, o.InsecureChanged)

		if i.Local {
			needsPubKeyVerification := a[consts.ImageAnnotationKey] != "" || o.Key != "" || i.Key != ""
			needsKeylessVerification := a[consts.ImageAnnotationCertIdentityRegexp] != "" || a[consts.ImageAnnotationCertIdentity] != "" ||
				o.CertIdentityRegexp != "" || o.CertIdentity != "" ||
				i.CertIdentityRegexp != "" || i.CertIdentity != ""
			if needsPubKeyVerification || needsKeylessVerification {
				return nil, fmt.Errorf("image [%s]: --local cannot be combined with cosign verification options", i.Name)
			}

			rewrite := ""
			if i.Rewrite != "" {
				rewrite = i.Rewrite
			}
			jobs = append(jobs, imageJob{img: i, local: true, rewrite: rewrite})
			continue
		}

		hasAnnotationIdentityOptions := a[consts.ImageAnnotationCertIdentityRegexp] != "" || a[consts.ImageAnnotationCertIdentity] != ""
		hasCliIdentityOptions := o.CertIdentityRegexp != "" || o.CertIdentity != ""
		hasImageIdentityOptions := i.CertIdentityRegexp != "" || i.CertIdentity != ""

		needsKeylessVerificaton := hasAnnotationIdentityOptions || hasCliIdentityOptions || hasImageIdentityOptions
		needsPubKeyVerification := a[consts.ImageAnnotationKey] != "" || o.Key != "" || i.Key != ""

		job := imageJob{img: i}

		if needsPubKeyVerification {
			key := o.Key
			if o.Key == "" {
				if i.Key != "" {
					expanded, err := homedir.Expand(i.Key)
					if err != nil {
						return nil, err
					}
					key = expanded
				} else if a[consts.ImageAnnotationKey] != "" {
					expanded, err := homedir.Expand(a[consts.ImageAnnotationKey])
					if err != nil {
						return nil, err
					}
					key = expanded
				}
			}

			tlog := resolveBoolFlag(i.Tlog, a[consts.ImageAnnotationTlog] == "true", o.Tlog, o.TlogChanged)

			job.needsPubKey = true
			job.key = key
			job.tlog = tlog
		} else if needsKeylessVerificaton { //Keyless signature verification
			certIdentityRegexp := o.CertIdentityRegexp
			if o.CertIdentityRegexp == "" {
				if i.CertIdentityRegexp != "" {
					certIdentityRegexp = i.CertIdentityRegexp
				} else if a[consts.ImageAnnotationCertIdentityRegexp] != "" {
					certIdentityRegexp = a[consts.ImageAnnotationCertIdentityRegexp]
				}
			}

			certIdentity := o.CertIdentity
			if o.CertIdentity == "" {
				if i.CertIdentity != "" {
					certIdentity = i.CertIdentity
				} else if a[consts.ImageAnnotationCertIdentity] != "" {
					certIdentity = a[consts.ImageAnnotationCertIdentity]
				}
			}

			certOidcIssuer := o.CertOidcIssuer
			if o.CertOidcIssuer == "" {
				if i.CertOidcIssuer != "" {
					certOidcIssuer = i.CertOidcIssuer
				} else if a[consts.ImageAnnotationCertOidcIssuer] != "" {
					certOidcIssuer = a[consts.ImageAnnotationCertOidcIssuer]
				}
			}

			certOidcIssuerRegexp := o.CertOidcIssuerRegexp
			if o.CertOidcIssuerRegexp == "" {
				if i.CertOidcIssuerRegexp != "" {
					certOidcIssuerRegexp = i.CertOidcIssuerRegexp
				} else if a[consts.ImageAnnotationCertOidcIssuerRegexp] != "" {
					certOidcIssuerRegexp = a[consts.ImageAnnotationCertOidcIssuerRegexp]
				}
			}

			certGithubWorkflowRepository := o.CertGithubWorkflowRepository
			if o.CertGithubWorkflowRepository == "" {
				if i.CertGithubWorkflowRepository != "" {
					certGithubWorkflowRepository = i.CertGithubWorkflowRepository
				} else if a[consts.ImageAnnotationCertGithubWorkflowRepository] != "" {
					certGithubWorkflowRepository = a[consts.ImageAnnotationCertGithubWorkflowRepository]
				}
			}

			job.needsKeyless = true
			job.certIdentity = certIdentity
			job.certIdentityRegexp = certIdentityRegexp
			job.certOidcIssuer = certOidcIssuer
			job.certOidcIssuerRegexp = certOidcIssuerRegexp
			job.certGithubWorkflowRepository = certGithubWorkflowRepository
		}

		platform := o.Platform
		if o.Platform == "" {
			if i.Platform != "" {
				platform = i.Platform
			} else if a[consts.ImageAnnotationPlatform] != "" {
				platform = a[consts.ImageAnnotationPlatform]
			}
		}

		rewrite := ""
		if i.Rewrite != "" {
			rewrite = i.Rewrite
		}

		excludeExtras := resolveBoolFlag(i.ExcludeExtras, a[consts.ImageAnnotationExcludeExtras] == "true", o.ExcludeExtras, o.ExcludeExtrasChanged)

		job.platform = platform
		job.rewrite = rewrite
		job.excludeExtras = excludeExtras

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// verifyConfig collapses j's resolved verification inputs into the key
// cosign.Cache uses to share one Verifier -- and therefore one trust-material
// setup -- across every image with identical settings.
//
// The branch mirrors resolveImageJobs' own exclusive key-then-keyless
// precedence rather than forwarding whatever fields happen to be set. A
// manifest naming both a key and an identity has always verified against the
// key alone, and cosign.Config.validate rejects that pairing outright, so
// building the Config from the raw inputs would turn a working manifest into a
// hard error.
//
// It returns the zero Config -- the one cosign.Config.Empty reports -- exactly
// when neither flag is set, which is what keeps the "does this image verify?"
// gate identical to the one the old batch pass used.
func (j imageJob) verifyConfig() cosign.Config {
	switch {
	case j.needsPubKey:
		return cosign.Config{
			Key:                   j.key,
			Tlog:                  j.tlog,
			InsecureSkipTLSVerify: j.img.InsecureSkipTLSVerify,
			CaFile:                j.img.CaFile,
		}
	case j.needsKeyless:
		return cosign.Config{
			CertIdentity:                 j.certIdentity,
			CertIdentityRegexp:           j.certIdentityRegexp,
			CertOidcIssuer:               j.certOidcIssuer,
			CertOidcIssuerRegexp:         j.certOidcIssuerRegexp,
			CertGithubWorkflowRepository: j.certGithubWorkflowRepository,
			InsecureSkipTLSVerify:        j.img.InsecureSkipTLSVerify,
			CaFile:                       j.img.CaFile,
		}
	default:
		return cosign.Config{}
	}
}

// resolveAndVerify pins j's tag to a digest and verifies that exact digest,
// returning the digest for storeImage to fetch.
//
// Resolving here rather than in a prior pass is the point of the change: a
// batch verify pass left the whole pass's duration between checking a tag and
// pulling it, during which the tag could move. Verifying the digest and handing
// the same digest to storeImage closes that window -- the bytes stored are the
// bytes checked.
//
// A job that requested no verification is not resolved at all. There is no
// window to close when nothing is checked, and an unconditional HEAD would add
// a registry round trip per image to the overwhelmingly common unsigned case.
// The empty digest it returns leaves storeImage resolving the tag as before.
//
// Every error it returns is a *verifyError, so the caller can say which of the
// four steps failed instead of blaming them all on the signature. The two
// post-pin failure branches (cache.Get, v.Verify) return the pinned digest
// alongside the error, not "": under --ignore-errors the caller stores the
// image anyway, and it must store the exact bytes that were checked even
// though the check failed, not let storeImage re-resolve the tag. The
// pre-pin branches (a bad reference, or the pin itself failing) have no
// digest to give back.
func resolveAndVerify(ctx context.Context, cache *cosign.Cache, j imageJob, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) (string, error) {
	cfg := j.verifyConfig()
	if cfg.Empty() {
		return "", nil
	}
	logVerifyInputs(ctx, j)

	ref, err := gname.ParseReference(j.img.Name)
	if err != nil {
		return "", &verifyError{stage: "unable to parse image reference", err: err}
	}

	pinned, err := pinDigest(ctx, ref, rso, ro)
	if err != nil {
		return "", &verifyError{stage: "unable to resolve image digest", err: err}
	}

	// ctx is run-scoped (the errgroup's), never a per-image timeout, which
	// cosign.Cache.Get requires: the ctx of whichever goroutine finds cfg cold
	// ends up inside the registry options every image sharing cfg then uses.
	v, err := cache.Get(ctx, cfg)
	if err != nil {
		return pinned, &verifyError{stage: "unable to configure signature verification", err: err}
	}
	// Verify applies --retries itself, discriminating the errors a retry may
	// touch; see cosign.Verifier.verifyImage.
	if err := v.Verify(ctx, ref.Context().Digest(pinned).Name()); err != nil {
		return pinned, &verifyError{stage: "signature verification failed", err: err}
	}

	if cfg.Keyless() {
		log.BaseFromContext(ctx).Infof("✓ keyless signature verified for image [%s]", j.img.Name)
	} else {
		log.BaseFromContext(ctx).Infof("✓ signature verified for image [%s]", j.img.Name)
	}
	return pinned, nil
}

// pinDigest resolves ref to the digest its tag currently names, under the
// caller's --retries budget. Every caller that verifies before storing goes
// through it, so the pin is retried on exactly one code path.
//
// The pin is the one network call on the verify path that a transient blip can
// lose a *valid, signed* image to: in a sync a bare failure here drops the image
// and the run still exits 0, which reads to the user as silent data loss. It
// gets the same --retries budget the verify and store steps have.
// retry.Operation checks ctx before every attempt and aborts its backoff on
// cancellation, so a cancelled run still fails fast rather than sleeping out
// the budget.
func pinDigest(ctx context.Context, ref gname.Reference, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) (string, error) {
	var pinned string
	err := retry.Operation(ctx, rso, ro, func() error {
		desc, headErr := remote.Head(ref,
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
			remote.WithContext(ctx),
		)
		if headErr != nil {
			return headErr
		}
		pinned = desc.Digest.String()
		return nil
	})
	if err != nil {
		return "", err
	}
	return pinned, nil
}

// verifyError names which step of resolveAndVerify failed. A bad reference, an
// unreachable registry, an unreadable key, and a signature that did not check
// out are four different problems, and reporting all of them as "signature
// verification failed" tells users with a network fault that they have a
// signing fault.
//
// stage reads as the head of "<stage> for image [<ref>]".
type verifyError struct {
	stage string
	err   error
}

func (e *verifyError) Error() string { return e.stage + ": " + e.err.Error() }
func (e *verifyError) Unwrap() error { return e.err }

// logVerifyFailure reports err against ref and reports whether the caller
// should propagate it (fail the run) rather than proceed to storeImage with
// whatever digest resolveAndVerify already pinned.
//
// context.Canceled always propagates and logs at DEBUG, regardless of
// ignoreErrors: under the errgroup's fail-fast, one real storeImage failure
// cancels gctx and every other in-flight job lands here with a
// context.Canceled that has nothing to do with its own image. Logging those
// at ERROR would bury the single real failure under N-1 lines claiming
// signature problems the user does not have, and under --ignore-errors,
// treating a cancellation as an ordinary ignorable failure would store an
// image whose bytes were never actually checked because the run was already
// being torn down.
//
// Every other failure's fate depends on ignoreErrors: without it, this fails
// the run (ERROR, propagate=true) -- a reversal of the old behavior of
// dropping just this one image, made because a dropped signature failure let
// a manifest sync report success while quietly missing a scheduled image.
// With it, this logs a WARN and does not propagate, so the caller falls
// through to storeImage with whatever digest resolveAndVerify already pinned
// (possibly none, if the failure happened before pinning). This function only
// reports the verification outcome; it makes no claim about what storeImage
// does next -- storeImage has its own ignoreErrors handling and logs its own
// success or skip line immediately after. An unverified image reaching the
// store (and potentially an airgapped environment) is what --ignore-errors
// buys once verification is involved, not a bug to guard against.
func logVerifyFailure(l log.Logger, ref string, err error, ignoreErrors bool) bool {
	stage := "verification failed"
	cause := err
	var ve *verifyError
	if errors.As(err, &ve) {
		stage = ve.stage
		cause = ve.err
	}
	if errors.Is(err, context.Canceled) {
		l.Debugf("%s for image [%s]: %s", stage, ref, flattenVerifyError(cause))
		return true
	}
	if ignoreErrors {
		l.Warnf("⚠ %s for image [%s]: %s", stage, ref, flattenVerifyError(cause))
		return false
	}
	l.Errorf("✗ %s for image [%s]: %s... aborting...", stage, ref, flattenVerifyError(cause))
	return true
}

// flattenVerifyError renders err as one line. cosign's ErrNoMatchingSignatures
// joins one failure sentence per signature-verification attempt with "\n ",
// so a single failed image can carry the identical sentence repeated several
// times in a row; collapsing consecutive repeats keeps the log line from
// restating the same cause N times.
func flattenVerifyError(err error) string {
	if err == nil {
		return ""
	}

	var fragments []string
	for _, line := range strings.Split(err.Error(), "\n") {
		if f := strings.TrimSpace(line); f != "" {
			fragments = append(fragments, f)
		}
	}

	var out []string
	for i := 0; i < len(fragments); {
		j := i + 1
		for j < len(fragments) && fragments[j] == fragments[i] {
			j++
		}
		if n := j - i; n > 1 {
			out = append(out, fmt.Sprintf("%s (x%d)", fragments[i], n))
		} else {
			out = append(out, fragments[i])
		}
		i = j
	}

	return strings.Join(out, "; ")
}

// logVerifyInputs echoes j's resolved verification settings at debug, so a run
// started against the wrong key or identity is diagnosable from --log-level
// debug alone. One line per job rather than one per field: at sync concurrency
// the per-field lines interleaved into an unreadable stream.
func logVerifyInputs(ctx context.Context, j imageJob) {
	// The ref is named inline, so this takes the unadorned base logger and not
	// the per-job one that would append a duplicating "image=" field -- the
	// same convention storeImage's completion line follows.
	l := log.BaseFromContext(ctx)
	switch {
	case j.needsPubKey:
		l.Debugf("verifying image [%s] with key [%s] and transparency log [%t]", j.img.Name, j.key, j.tlog)
	case j.needsKeyless:
		l.Debugf("verifying image [%s] keylessly with certIdentity [%s] certIdentityRegexp [%s] certOidcIssuer [%s] certOidcIssuerRegexp [%s] certGithubWorkflowRepository [%s]",
			j.img.Name, j.certIdentity, j.certIdentityRegexp, j.certOidcIssuer, j.certOidcIssuerRegexp, j.certGithubWorkflowRepository)
	}
}

// runImageJobs stores every job, local Docker daemon images first
// (serially), then remote images concurrently (bounded by concurrency).
// Local jobs run through storeLocalImage, whose ensureDockerHost mutates
// the process-wide environment via os.Setenv, and all contend on one
// Docker daemon anyway -- nothing to gain, and a mutation race to lose,
// from running them concurrently.
//
// This is the progress-session-owning wrapper around
// runRemoteImageJobsWith. The local pass deliberately runs before
// progress.Start(): storeLocalImage logs through the ambient logger, not
// the Renderer, so running it inside a live session would interleave its
// output with the Renderer's erase/redraw cycle and corrupt the display.
func runImageJobs(ctx context.Context, s *store.Layout, jobs []imageJob, concurrency int, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, progress *log.Renderer) error {
	l := log.FromContext(ctx)

	var localJobs, remoteJobs []imageJob
	for _, j := range jobs {
		if j.local {
			localJobs = append(localJobs, j)
		} else {
			remoteJobs = append(remoteJobs, j)
		}
	}

	for _, j := range localJobs {
		if err := storeLocalImage(ctx, s, j.img, rso, ro, j.rewrite); err != nil {
			return err
		}
	}

	// baseLogger is the logger every job's per-image logger is derived from.
	// When progress is active, it's built over the Renderer instead, so
	// every log line for a job -- including its "✓ added ..." line and any
	// errors -- flows through the Renderer's erase/write/redraw path rather
	// than writing straight to the ambient logger's destination.
	baseLogger := l
	if progress != nil && len(remoteJobs) > 0 {
		baseLogger = log.NewLogger(progress)
		progress.Start()
		defer progress.Stop()
	}

	return runRemoteImageJobsWith(ctx, s, remoteJobs, concurrency, rso, ro, progress, baseLogger)
}

// runRemoteImageJobsWith stores remote image jobs concurrently, bounded by
// concurrency, inside a progress session the caller already started (or nil)
// and against a baseLogger the caller already derived. It never calls
// Start/Stop, so one caller can span a single session across several phases.
// jobs must be remote-only -- runImageJobs runs the local Docker pass itself,
// before the session opens.
//
// errgroup.WithContext + SetLimit(concurrency) gives both fail-fast and
// --ignore-errors semantics: with --ignore-errors storeImage warns and
// returns nil, so g.Wait() never observes an error; otherwise a failing job
// cancels the group's derived context, which every other in-flight storeImage
// call observes via content.OCI.WriteBlob's context-aware writes, and g.Wait()
// returns that one real error, not an aggregate.
//
// Each job resolves, verifies, and stores in one goroutine rather than across
// separate passes, so verification runs at the same concurrency as the pulls
// and no time passes between checking a tag and fetching it.
func runRemoteImageJobsWith(ctx context.Context, s *store.Layout, jobs []imageJob, concurrency int, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, progress *log.Renderer, baseLogger log.Logger) error {
	if concurrency < 1 {
		concurrency = 1
	}

	// The cache is per-call rather than per-run so its lifetime is bracketed by
	// the g.Wait() below: Verifier.Close must not run while a Verify is still
	// in flight. One call covers one document's images, which is where the
	// sharing matters -- a Rancher manifest's 880 images build trust material
	// once between them, not 880 times. A manifest file holding several Images
	// documents builds it once per document.
	cache := cosign.NewCache(rso, ro)
	defer cache.Close()

	ignoreErrors := flags.ShouldIgnoreErrors(ro)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	for _, j := range jobs {
		g.Go(func() error {
			// Began must be called from inside the goroutine, after
			// g.Go's semaphore acquisition, not from the outer loop:
			// g.Go blocks the outer loop until a concurrency slot is
			// free, so this is the earliest point the job has actually
			// started. Calling Began any earlier would mark a queued
			// job "in flight" while it's still waiting for a slot.
			//
			// Finished is deferred so the verify-failure return below
			// clears the row too; a live region that keeps drawing a
			// dropped image never stops.
			if progress != nil {
				progress.Began(j.img.Name)
				defer progress.Finished(j.img.Name)
			}
			jl := baseLogger.With(log.Fields{"image": j.img.Name})
			jctx := jl.WithContext(gctx)
			// storeImage lines that already name their ref inline (e.g.
			// its "✓ added <ref> ..." line) fetch this unadorned base
			// logger via log.BaseFromContext instead of log.FromContext,
			// so they don't duplicate the ref with the "image=..." field
			// below. Lines that don't name the ref (retry.Operation's
			// warnings, store-layer debug output) keep calling
			// log.FromContext(ctx) and keep the field for attribution.
			jctx = log.WithBaseLogger(jctx, baseLogger)

			pinned, err := resolveAndVerify(jctx, cache, j, rso, ro)
			if err != nil {
				// A verification failure either fails the run or is stored
				// unverified, depending on --ignore-errors -- see
				// logVerifyFailure's doc for the exact rule, including the
				// context.Canceled case that overrides both. propagate=false
				// falls through to the same storeImage call the success path
				// uses, with whatever digest resolveAndVerify already pinned
				// (or "" if the failure happened before the pin).
				if propagate := logVerifyFailure(baseLogger, j.img.Name, err, ignoreErrors); propagate {
					return err
				}
			}
			// verified is true only when verification was both requested
			// and succeeded: err is nil on success and stays non-nil on a
			// failure that fell through to here under --ignore-errors.
			verified := err == nil && !j.verifyConfig().Empty()
			return storeImage(jctx, s, j.img, j.platform, j.excludeExtras, rso, ro, j.rewrite, pinned, verified)
		})
	}
	return g.Wait()
}

// fileJob is the fully-resolved set of inputs needed to store a single
// file; see resolveFileJobs. Unlike imageJob, there's no verification pass.
type fileJob struct {
	file v1.File
}

// resolveFileJobs converts every v1.File in files into a fileJob. caFile is
// CLI > per-file > annotation; insecureSkipTLSVerify is CLI-first (an explicit
// CLI flag wins, otherwise per-file or annotation can only turn it on -- see
// resolveBoolFlag). It is pure.
func resolveFileJobs(o *flags.SyncOpts, a map[string]string, files []v1.File) []fileJob {
	jobs := make([]fileJob, 0, len(files))
	for _, f := range files {

		// caFile precedence: cli >per-image > annotation.
		if o.CaFile == "" && f.CaFile == "" && a[consts.ImageAnnotationCaFile] != "" {
			f.CaFile = a[consts.ImageAnnotationCaFile]
		} else if o.CaFile != "" {
			f.CaFile = o.CaFile
		}

		// a CA file and skipping TLS verification are mutually exclusive: providing one forces verification on
		f.InsecureSkipTLSVerify = o.CaFile == "" && resolveBoolFlag(f.InsecureSkipTLSVerify, a[consts.ImageAnnotationInsecureSkipTLSVerify] == "true", o.InsecureSkipTLSVerify, o.InsecureChanged)

		jobs = append(jobs, fileJob{file: f})
	}
	return jobs
}

// fileJobName returns the identifier used for a file job's progress row and
// per-job log field: the name override when set (matching the ref
// storeFile/reference.NewTagged will actually derive), otherwise the raw
// source path.
func fileJobName(f v1.File) string {
	if f.Name != "" {
		return f.Name
	}
	return f.Path
}

// runFileJobs stores every job concurrently, bounded by concurrency, with
// no local/remote partitioning by scheme: unlike images, no file source
// contends on a shared process-wide resource the way storeLocalImage's
// Docker-daemon path does (see runImageJobs), so file://, directory://, and
// http(s):// sources all run through the same errgroup. pkg/content's OCI
// store blob semaphore already bounds total in-flight blob writes
// regardless of scheme.
//
// Every job shares one *file.LayerCache (pkg/artifacts/file/cache.go),
// attached to each job's context, so two Files entries with the identical
// source Path -- e.g. the same URL listed twice, once plain and once with a
// name override, as testdata/hauler-manifest-pipeline.yaml does -- fetch
// the content exactly once regardless of concurrency, rather than once per
// manifest entry.
//
// Fail-fast and --ignore-errors semantics mirror runImageJobs exactly.
//
// Like runImageJobs, this is only the progress-session-owning wrapper; the
// work lives in runFileJobsWith.
func runFileJobs(ctx context.Context, s *store.Layout, jobs []fileJob, concurrency int, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, progress *log.Renderer) error {
	l := log.FromContext(ctx)

	// baseLogger is the logger every job's per-file logger is derived from --
	// see runImageJobs's identical baseLogger for the full rationale.
	baseLogger := l
	if progress != nil && len(jobs) > 0 {
		baseLogger = log.NewLogger(progress)
		progress.Start()
		defer progress.Stop()
	}

	return runFileJobsWith(ctx, s, jobs, concurrency, rso, ro, progress, baseLogger)
}

// runFileJobsWith stores file jobs concurrently, bounded by concurrency,
// inside a progress session the caller already started (or nil) and against a
// baseLogger the caller already derived. It never calls Start/Stop -- see
// runRemoteImageJobsWith, its image-side counterpart.
func runFileJobsWith(ctx context.Context, s *store.Layout, jobs []fileJob, concurrency int, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, progress *log.Renderer, baseLogger log.Logger) error {
	if concurrency < 1 {
		concurrency = 1
	}

	cache := file.NewLayerCache()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	for _, j := range jobs {
		g.Go(func() error {
			name := fileJobName(j.file)
			// Began must be called after g.Go's semaphore acquisition;
			// see runRemoteImageJobsWith.
			if progress != nil {
				progress.Began(name)
			}
			jl := baseLogger.With(log.Fields{"file": name})
			jctx := jl.WithContext(gctx)
			jctx = log.WithBaseLogger(jctx, baseLogger)
			jctx = file.WithLayerCacheContext(jctx, cache)
			err := storeFile(jctx, s, j.file, ro, rso)
			if progress != nil {
				progress.Finished(name)
			}
			return err
		})
	}
	return g.Wait()
}
