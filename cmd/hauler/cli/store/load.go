package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/archives"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/getter"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/store"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// matches the old <base>_NNN.<ext> chunk naming of haul_0.tar.zst
// used only to print a hint when a load fails on a file shaped like a stale chunk
var legacyChunkRe = regexp.MustCompile(`_\d+\.`)

// extracts the contents of an archived oci layout to an existing oci layout
func LoadCmd(ctx context.Context, o *flags.LoadOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	tempOverride := rso.TempOverride

	if tempOverride == "" {
		tempOverride = os.Getenv(consts.HaulerTempDir)
	}

	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// kept separate from tempDir since that gets wiped after every haul
	// below — remote chunks need to stick around until the whole set lands
	stageDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)

	fileNames, remoteOrigin, err := stageRemoteChunks(ctx, o.FileName, stageDir)
	if err != nil {
		return err
	}

	l.Debugf("using temporary directory at [%s]", tempDir)

	for _, fileName := range fileNames {
		resolved := resolveHaulPath(fileName)
		wasRemote := strings.HasPrefix(fileName, "http://") || strings.HasPrefix(fileName, "https://") || remoteOrigin[fileName]
		l.Infof("loading haul [%s] to [%s]", resolved, o.StoreDir)
		err := unarchiveLayoutTo(ctx, resolved, o.StoreDir, tempDir, ro, wasRemote)
		if err != nil {
			return err
		}
		clearDir(tempDir)
	}

	return nil
}

// stageRemoteChunks pre-downloads any remote URL that looks chunk-shaped
// into stageDir before the main load loop starts. That's what makes
// `store load -f url1 -f url2 ...` work for a remote chunk set — they all
// land on disk together, so JoinChunks can just find them once it runs.
// Non-chunk URLs are left alone and go through unarchiveLayoutTo's normal
// single-file download instead.
//
// Only one path per chunk set gets added to the returned list, so the main
// loop doesn't process the same haul twice. remoteOrigin tracks which of
// those returned paths actually came from a download, since the caller
// needs that later to pick the right wording for a failure hint.
func stageRemoteChunks(ctx context.Context, fileNames []string, stageDir string) ([]string, map[string]bool, error) {
	added := map[string]bool{}
	remoteOrigin := map[string]bool{}
	var result []string

	for _, fn := range fileNames {
		if !strings.HasPrefix(fn, "http://") && !strings.HasPrefix(fn, "https://") {
			result = append(result, fn)
			continue
		}

		parsedURL, err := url.Parse(fn)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := archives.ChunkGroupKey(filepath.Base(parsedURL.Path)); !ok {
			result = append(result, fn)
			continue
		}

		local, err := downloadHaul(ctx, fn, stageDir)
		if err != nil {
			return nil, nil, err
		}
		remoteOrigin[local] = true

		key, _ := archives.ChunkGroupKey(filepath.Base(local))
		if !added[key] {
			result = append(result, local)
			added[key] = true
		}
	}

	return result, remoteOrigin, nil
}

// downloadHaul fetches urlStr into destDir, using the server-provided
// filename when available, and returns the local path it was saved to.
func downloadHaul(ctx context.Context, urlStr, destDir string) (string, error) {
	h := getter.NewHttp()
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	rc, err := h.Open(ctx, parsedURL)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	fileName := h.Name(parsedURL)
	if fileName == "" {
		fileName = filepath.Base(parsedURL.Path)
	}
	localPath := filepath.Join(destDir, fileName)

	out, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err = io.Copy(out, rc); err != nil {
		return "", err
	}
	return localPath, nil
}

// accepts an archived OCI layout, extracts the contents to an existing OCI layout, and preserves the index
func unarchiveLayoutTo(ctx context.Context, haulPath string, dest string, tempDir string, ro *flags.CliRootOpts, wasRemote bool) error {
	l := log.FromContext(ctx)

	if strings.HasPrefix(haulPath, "http://") || strings.HasPrefix(haulPath, "https://") {
		l.Debugf("detected remote archive... starting download... [%s]", haulPath)
		local, err := downloadHaul(ctx, haulPath, tempDir)
		if err != nil {
			return err
		}
		haulPath = local
	}

	// reassemble chunk files if haulPath matches the chunk naming pattern.
	// hang onto the pre-join name for the hint below — once joined, even a
	// lone unjoinable fragment looks like a normal file (it just gets
	// copied to itself), so the hint needs the name we were actually asked
	// to load, not whatever JoinChunks renamed it to.
	preChunkPath := haulPath
	joined, err := archives.JoinChunks(ctx, haulPath, tempDir)
	if err != nil {
		return err
	}
	haulPath = joined

	if err := archives.Unarchive(ctx, haulPath, tempDir); err != nil {
		if line1, line2, ok := chunkHint(preChunkPath, wasRemote); ok {
			ignoreErrors := ro.IgnoreErrors
			if !ignoreErrors && os.Getenv(consts.HaulerIgnoreErrors) == "true" {
				ignoreErrors = true
			}
			if ignoreErrors {
				l.Warnf("%s", line1)
				l.Warnf("%s", line2)
				return nil
			}
			l.Errorf("%s", line1)
			l.Errorf("%s", line2)
		}
		return err
	}

	// ensure the incoming index.json has the correct annotations.
	data, err := os.ReadFile(tempDir + "/index.json")
	if err != nil {
		return (err)
	}

	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return (err)
	}

	for i := range idx.Manifests {
		if idx.Manifests[i].Annotations == nil {
			idx.Manifests[i].Annotations = make(map[string]string)
		}
		if _, exists := idx.Manifests[i].Annotations[consts.KindAnnotationName]; !exists {
			idx.Manifests[i].Annotations[consts.KindAnnotationName] = consts.KindAnnotationImage
		}
		// Translate legacy dev.cosignproject.cosign values to dev.hauler equivalents.
		kind := idx.Manifests[i].Annotations[consts.KindAnnotationName]
		idx.Manifests[i].Annotations[consts.KindAnnotationName] = consts.NormalizeLegacyKind(kind)
		if ref, ok := idx.Manifests[i].Annotations[consts.ContainerdImageNameKey]; ok {
			if slash := strings.Index(ref, "/"); slash != -1 {
				ref = ref[slash+1:]
			}
			if idx.Manifests[i].Annotations[consts.ImageRefKey] != ref {
				idx.Manifests[i].Annotations[consts.ImageRefKey] = ref
			}
		}
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tempDir+"/index.json", out, 0644); err != nil {
		return err
	}

	s, err := store.NewLayout(tempDir)
	if err != nil {
		return err
	}

	ts, err := content.NewOCI(dest)
	if err != nil {
		return err
	}

	_, err = s.CopyAll(ctx, ts, nil)
	return err
}

// matches the <path>.NNN chunk suffix that is used to filter resolveHaulPath
// glob matches down to real chunks, so an unrelated sibling file (.sig, .bak,
// etc...) is never picked up instead of the actual first chunk
var chunkSuffixRe = regexp.MustCompile(`\.(\d{3,})$`)

// chunkHint builds a two-line hint for a load failure on a chunk-shaped
// filename, if one applies. Wording depends on where it came from: a URL
// can't just be renamed, so it points at downloading or listing every chunk
// instead; a local file gets told to rename (old naming) or go find its
// missing siblings (new naming).
func chunkHint(haulPath string, wasRemote bool) (line1, line2 string, ok bool) {
	base := filepath.Base(haulPath)
	isLegacyShaped := legacyChunkRe.MatchString(base)
	isNewShaped := chunkSuffixRe.MatchString(base)
	if !isLegacyShaped && !isNewShaped {
		return "", "", false
	}

	if wasRemote {
		return fmt.Sprintf("possibly detected an unjoined remote chunk for haul: [%s]", haulPath),
			"download every chunk locally first, or specify each chunk via its own --filename flag, and try loading it again...",
			true
	}
	if isLegacyShaped {
		return fmt.Sprintf("possibly detected an old chunk format for haul: [%s]", haulPath),
			"attempt to rename to '<base>.<ext>.NNN' and try loading it again...",
			true
	}
	return fmt.Sprintf("possibly detected a missing chunk for haul: [%s]", haulPath),
		"ensure every chunk file (.001, .002, ...) is present in the same directory and try loading it again...",
		true
}

// resolveHaulPath returns path as-is if it exists or is a URL, otherwise
// globs for chunk files matching <path>.NNN so JoinChunks can reassemble them.
func resolveHaulPath(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	matches, err := filepath.Glob(path + ".*")
	if err != nil {
		return path
	}
	for _, m := range matches {
		sub := chunkSuffixRe.FindStringSubmatch(m)
		if sub == nil {
			continue
		}
		if idx, err := strconv.Atoi(sub[1]); err == nil && idx != 0 {
			return m
		}
	}
	return path
}

func clearDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		err = os.RemoveAll(filepath.Join(path, entry.Name()))
		if err != nil {
			return err
		}
	}

	return nil
}
