package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
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
	"helm.sh/helm/v4/pkg/action"
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

	// Everything below runs with a real store (s != nil; the dry-run branch
	// above already returned). Force one durable index checkpoint at the end
	// of the run since the per-artifact path only fsyncs on
	// indexCheckpointInterval -- deferred so it still runs on error paths,
	// where a partially-populated index is worth persisting. This does NOT
	// run on Ctrl-C (no signal handler is installed), which is fine: process
	// death doesn't lose page cache, so the index still reaches disk.
	defer func() {
		if err := s.OCI.SaveIndex(); err != nil {
			l.Warnf("failed to save index at end of sync: %v", err)
		}
		l.Debugf("%s", formatIOStats(s.OCI.Stats().Snapshot(), s.OCI.BlobConcurrency()))
	}()

	tempOverride := rso.TempOverride

	if tempOverride == "" {
		tempOverride = os.Getenv(consts.HaulerTempDir)
	}

	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
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
			Name: manifestLoc,
		}
		err := storeImage(ctx, s, img, o.Platform, o.ExcludeExtras, rso, ro, "")
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
		err = processContent(ctx, fi, o, s, rso, ro)
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

				h := getter.NewHttp()
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

			err = processContent(ctx, fi, o, s, rso, ro)
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

				h := getter.NewHttp()
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

func processContent(ctx context.Context, fi *os.File, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
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
		l.Infof("syncing content [%s] with [kind=%s] to store [%s]", gvk.GroupVersion(), gvk.Kind, o.StoreDir)

		switch gvk.Kind {

		case consts.FilesContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Files
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				jobs := resolveFileJobs(cfg.Spec.Files)
				if err := runFileJobs(ctx, s, jobs, o.Concurrency, rso, ro, newSyncProgress(o, ro)); err != nil {
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
				jobs, err := resolveImageJobs(o, a, cfg.Spec.Images)
				if err != nil {
					return err
				}
				jobs = verifyImageJobs(ctx, jobs, rso, ro)
				if err := runImageJobs(ctx, s, jobs, o.Concurrency, rso, ro, newSyncProgress(o, ro)); err != nil {
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
				registry := o.Registry
				annotation := cfg.GetAnnotations()
				if registry == "" {
					if annotation != nil {
						registry = annotation[consts.ImageAnnotationRegistry]
					}
				}

				for i, ch := range cfg.Spec.Charts {
					// Resolve excludeExtras: per-chart field > chart manifest annotation > CLI flag.
					excludeExtras := o.ExcludeExtras
					if !o.ExcludeExtras && annotation != nil && annotation[consts.ImageAnnotationExcludeExtras] == "true" {
						excludeExtras = true
					}
					if ch.ExcludeExtras {
						excludeExtras = ch.ExcludeExtras
					}

					var valuesFiles []string
					for _, path := range ch.ValuesFiles {
						valuesFiles = append(valuesFiles, filepath.Join(filepath.Dir(fi.Name()), path))
					}

					platform := o.Platform
					if annotation != nil && annotation[consts.ImageAnnotationPlatform] != "" {
						platform = annotation[consts.ImageAnnotationPlatform]
					}
					if ch.Platform != "" {
						platform = ch.Platform
					}

					chartUsername, chartPassword, err := resolveChartCreds(ch)
					if err != nil {
						return err
					}

					if err := storeChart(ctx, s, ch,
						&flags.AddChartOpts{
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
								CaFile:                ch.CaFile,
								InsecureSkipTLSVerify: ch.InsecureSkipTLSVerify,
								PlainHTTP:             ch.PlainHTTP,
							},
							AddImages:       ch.AddImages,
							AddDependencies: ch.AddDependencies,
							ExcludeExtras:   excludeExtras,
							Registry:        registry,
							Platform:        platform,
							ValuesFiles:     valuesFiles,
						},
						rso, ro,
						cfg.Spec.Charts[i].Rewrite,
					); err != nil {
						return err
					}
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
			img:           v1.Image{Name: line},
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
// treats a nil progress as a no-op. Constructing the Renderer here, after
// verifyImageJobs has already run fully and serially, satisfies "don't
// start the renderer until the verify pass is done" for free.
func newSyncProgress(o *flags.SyncOpts, ro *flags.CliRootOpts) *log.Renderer {
	if !log.ShouldShowProgress(o.NoProgress, ro.LogLevel) {
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

	// resolved verification inputs, consumed by the verify pass
	needsPubKey, needsKeyless            bool
	key                                  string
	tlog                                 bool
	certIdentity, certIdentityRegexp     string
	certOidcIssuer, certOidcIssuerRegexp string
	certGithubWorkflowRepository         string
}

// resolveImageJobs applies the precedence rules (per-image > annotation >
// CLI, except registry relocation which is CLI > annotation) to every image
// in images, producing one imageJob per image. It is pure -- cosign
// verification happens later, in verifyImageJobs.
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
			if o.Key == "" && a[consts.ImageAnnotationKey] != "" {
				expanded, err := homedir.Expand(a[consts.ImageAnnotationKey])
				if err != nil {
					return nil, err
				}
				key = expanded
			}
			if i.Key != "" {
				expanded, err := homedir.Expand(i.Key)
				if err != nil {
					return nil, err
				}
				key = expanded
			}

			tlog := o.Tlog
			if !o.Tlog && a[consts.ImageAnnotationTlog] == "true" {
				tlog = true
			}
			if i.Tlog {
				tlog = i.Tlog
			}

			job.needsPubKey = true
			job.key = key
			job.tlog = tlog
		} else if needsKeylessVerificaton { //Keyless signature verification
			certIdentityRegexp := o.CertIdentityRegexp
			if o.CertIdentityRegexp == "" && a[consts.ImageAnnotationCertIdentityRegexp] != "" {
				certIdentityRegexp = a[consts.ImageAnnotationCertIdentityRegexp]
			}
			if i.CertIdentityRegexp != "" {
				certIdentityRegexp = i.CertIdentityRegexp
			}

			certIdentity := o.CertIdentity
			if o.CertIdentity == "" && a[consts.ImageAnnotationCertIdentity] != "" {
				certIdentity = a[consts.ImageAnnotationCertIdentity]
			}
			if i.CertIdentity != "" {
				certIdentity = i.CertIdentity
			}

			certOidcIssuer := o.CertOidcIssuer
			if o.CertOidcIssuer == "" && a[consts.ImageAnnotationCertOidcIssuer] != "" {
				certOidcIssuer = a[consts.ImageAnnotationCertOidcIssuer]
			}
			if i.CertOidcIssuer != "" {
				certOidcIssuer = i.CertOidcIssuer
			}

			certOidcIssuerRegexp := o.CertOidcIssuerRegexp
			if o.CertOidcIssuerRegexp == "" && a[consts.ImageAnnotationCertOidcIssuerRegexp] != "" {
				certOidcIssuerRegexp = a[consts.ImageAnnotationCertOidcIssuerRegexp]
			}
			if i.CertOidcIssuerRegexp != "" {
				certOidcIssuerRegexp = i.CertOidcIssuerRegexp
			}

			certGithubWorkflowRepository := o.CertGithubWorkflowRepository
			if o.CertGithubWorkflowRepository == "" && a[consts.ImageAnnotationCertGithubWorkflowRepository] != "" {
				certGithubWorkflowRepository = a[consts.ImageAnnotationCertGithubWorkflowRepository]
			}
			if i.CertGithubWorkflowRepository != "" {
				certGithubWorkflowRepository = i.CertGithubWorkflowRepository
			}

			job.needsKeyless = true
			job.certIdentity = certIdentity
			job.certIdentityRegexp = certIdentityRegexp
			job.certOidcIssuer = certOidcIssuer
			job.certOidcIssuerRegexp = certOidcIssuerRegexp
			job.certGithubWorkflowRepository = certGithubWorkflowRepository
		}

		platform := o.Platform
		if o.Platform == "" && a[consts.ImageAnnotationPlatform] != "" {
			platform = a[consts.ImageAnnotationPlatform]
		}
		if i.Platform != "" {
			platform = i.Platform
		}

		rewrite := ""
		if i.Rewrite != "" {
			rewrite = i.Rewrite
		}

		excludeExtras := o.ExcludeExtras
		if !o.ExcludeExtras && a[consts.ImageAnnotationExcludeExtras] == "true" {
			excludeExtras = true
		}
		if i.ExcludeExtras {
			excludeExtras = i.ExcludeExtras
		}

		job.platform = platform
		job.rewrite = rewrite
		job.excludeExtras = excludeExtras

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// verifyImageJobs runs cosign verification (network I/O) for every job that
// needs it, returning the jobs that passed plus any that didn't need
// verification. A failure logs an error and drops the job silently --
// SyncCmd never fails the whole run over one bad signature, regardless of
// --ignore-errors.
func verifyImageJobs(ctx context.Context, jobs []imageJob, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) []imageJob {
	l := log.FromContext(ctx)

	var out []imageJob
	for _, j := range jobs {
		if j.local {
			out = append(out, j)
			continue
		}

		if j.needsPubKey {
			l.Debugf("key for image [%s]", j.key)
			l.Debugf("transparency log for verification [%t]", j.tlog)

			if err := cosign.VerifySignature(ctx, j.key, j.tlog, j.img.Name, rso, ro); err != nil {
				l.Errorf("signature verification failed for image [%s]... skipping...\n%v", j.img.Name, err)
				continue
			}
			l.Infof("signature verified for image [%s]", j.img.Name)
		} else if j.needsKeyless {
			l.Debugf("certIdentityRegexp for image [%s]", j.certIdentityRegexp)
			l.Debugf("certIdentity for image [%s]", j.certIdentity)
			l.Debugf("certOidcIssuer for image [%s]", j.certOidcIssuer)
			l.Debugf("certOidcIssuerRegexp for image [%s]", j.certOidcIssuerRegexp)
			l.Debugf("certGithubWorkflowRepository for image [%s]", j.certGithubWorkflowRepository)

			// Keyless (Fulcio) certs expire after ~10 min; tlog is always
			// required to prove the cert was valid at signing time.
			if err := cosign.VerifyKeylessSignature(ctx, j.certIdentity, j.certIdentityRegexp, j.certOidcIssuer, j.certOidcIssuerRegexp, j.certGithubWorkflowRepository, j.img.Name, rso, ro); err != nil {
				l.Errorf("signature verification failed for image [%s]... skipping...\n%v", j.img.Name, err)
				continue
			}
			l.Infof("keyless signature verified for image [%s]", j.img.Name)
		}

		out = append(out, j)
	}

	return out
}

// runImageJobs stores every job, local Docker daemon images first
// (serially), then remote images concurrently (bounded by concurrency).
// Local jobs run through storeLocalImage, whose ensureDockerHost mutates
// the process-wide environment via os.Setenv, and all contend on one
// Docker daemon anyway -- nothing to gain, and a mutation race to lose,
// from running them concurrently.
//
// The remote pass uses errgroup.WithContext + SetLimit(concurrency):
// fail-fast and --ignore-errors semantics fall out of that plus storeImage
// returning nil (after warning) when ignoreErrors is set, so g.Wait() never
// observes an error in that mode. Otherwise a failing job cancels the
// group's derived context, which every other in-flight storeImage call
// observes via content.OCI.WriteBlob's context-aware writes; g.Wait()
// returns that one real error, not an aggregate.
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

	if concurrency < 1 {
		concurrency = 1
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

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	for _, j := range remoteJobs {
		j := j
		g.Go(func() error {
			// Began must be called from inside the goroutine, after
			// g.Go's semaphore acquisition, not from the outer loop:
			// g.Go blocks the outer loop until a concurrency slot is
			// free, so this is the earliest point the job has actually
			// started. Calling Began any earlier would mark a queued
			// job "in flight" while it's still waiting for a slot.
			if progress != nil {
				progress.Began(j.img.Name)
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
			err := storeImage(jctx, s, j.img, j.platform, j.excludeExtras, rso, ro, j.rewrite)
			if progress != nil {
				progress.Finished(j.img.Name)
			}
			return err
		})
	}
	return g.Wait()
}

// fileJob is the fully-resolved set of inputs needed to store a single
// file; see resolveFileJobs. Unlike imageJob, there's no verification pass.
type fileJob struct {
	file v1.File
}

// resolveFileJobs converts every v1.File in files into a fileJob. It is
// pure.
func resolveFileJobs(files []v1.File) []fileJob {
	jobs := make([]fileJob, 0, len(files))
	for _, f := range files {
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
func runFileJobs(ctx context.Context, s *store.Layout, jobs []fileJob, concurrency int, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, progress *log.Renderer) error {
	l := log.FromContext(ctx)

	if concurrency < 1 {
		concurrency = 1
	}

	// baseLogger is the logger every job's per-file logger is derived from --
	// see runImageJobs's identical baseLogger for the full rationale.
	baseLogger := l
	if progress != nil && len(jobs) > 0 {
		baseLogger = log.NewLogger(progress)
		progress.Start()
		defer progress.Stop()
	}

	cache := file.NewLayerCache()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	for _, j := range jobs {
		j := j
		g.Go(func() error {
			name := fileJobName(j.file)
			// Began must be called after g.Go's semaphore acquisition;
			// see runImageJobs.
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
