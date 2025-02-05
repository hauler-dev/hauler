package imagetxt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"

	artifact "hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/artifacts/image"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
)

type ImageTxt struct {
	Ref            string
	IncludeSources map[string]bool
	ExcludeSources map[string]bool

	lock     *sync.Mutex
	client   *getter.Client
	computed bool
	contents map[string]artifact.OCI
}

var _ artifact.OCICollection = (*ImageTxt)(nil)

type Option interface {
	Apply(*ImageTxt) error
}

type withIncludeSources []string

func (o withIncludeSources) Apply(it *ImageTxt) error {
	if it.IncludeSources == nil {
		it.IncludeSources = make(map[string]bool)
	}
	for _, s := range o {
		it.IncludeSources[s] = true
	}
	return nil
}

func WithIncludeSources(include ...string) Option {
	return withIncludeSources(include)
}

type withExcludeSources []string

func (o withExcludeSources) Apply(it *ImageTxt) error {
	if it.ExcludeSources == nil {
		it.ExcludeSources = make(map[string]bool)
	}
	for _, s := range o {
		it.ExcludeSources[s] = true
	}
	return nil
}

func WithExcludeSources(exclude ...string) Option {
	return withExcludeSources(exclude)
}

func New(ref string, opts ...Option) (*ImageTxt, error) {
	it := &ImageTxt{
		Ref: ref,

		client: getter.NewClient(getter.ClientOptions{}),
		lock:   &sync.Mutex{},
	}

	for i, o := range opts {
		if err := o.Apply(it); err != nil {
			return nil, fmt.Errorf("invalid option %d: %v", i, err)
		}
	}

	return it, nil
}

func (it *ImageTxt) Contents() (map[string]artifact.OCI, error) {
	it.lock.Lock()
	defer it.lock.Unlock()
	if !it.computed {
		if err := it.compute(); err != nil {
			return nil, fmt.Errorf("compute OCI layout: %v", err)
		}
		it.computed = true
	}
	return it.contents, nil
}

func (it *ImageTxt) compute() error {
	// TODO - pass in logger from context
	l := log.NewLogger(os.Stdout)

	it.contents = make(map[string]artifact.OCI)

	ctx := context.TODO()

	rc, err := it.client.ContentFrom(ctx, it.Ref)
	if err != nil {
		return fmt.Errorf("fetch image.txt ref %s: %w", it.Ref, err)
	}
	defer rc.Close()

	entries, err := splitImagesTxt(rc)
	if err != nil {
		return fmt.Errorf("parse image.txt ref %s: %v", it.Ref, err)
	}

	foundSources := make(map[string]bool)
	for _, e := range entries {
		for s := range e.Sources {
			foundSources[s] = true
		}
	}

	var pullAll bool
	targetSources := make(map[string]bool)

	if len(foundSources) == 0 || (len(it.IncludeSources) == 0 && len(it.ExcludeSources) == 0) {
		// pull all found images
		pullAll = true

		if len(foundSources) == 0 {
			l.Infof("image txt file appears to have no sources; pulling all found images")
			if len(it.IncludeSources) != 0 || len(it.ExcludeSources) != 0 {
				l.Warnf("ImageTxt provided include or exclude sources; ignoring")
			}
		} else if len(it.IncludeSources) == 0 && len(it.ExcludeSources) == 0 {
			l.Infof("image-sources txt file not filtered; pulling all found images")
		}
	} else {
		// determine sources to pull
		if len(it.IncludeSources) != 0 && len(it.ExcludeSources) != 0 {
			l.Warnf("ImageTxt provided include and exclude sources; using only include sources")
		}

		if len(it.IncludeSources) != 0 {
			targetSources = it.IncludeSources
		} else {
			for s := range foundSources {
				targetSources[s] = true
			}
			for s := range it.ExcludeSources {
				delete(targetSources, s)
			}
		}
		var targetSourcesArr []string
		for s := range targetSources {
			targetSourcesArr = append(targetSourcesArr, s)
		}
		l.Infof("pulling images covering sources %s", strings.Join(targetSourcesArr, ", "))
	}

	for _, e := range entries {
		var matchesSourceFilter bool
		if pullAll {
			l.Infof("pulling image %s", e.Reference)
		} else {
			for s := range e.Sources {
				if targetSources[s] {
					matchesSourceFilter = true
					l.Infof("pulling image %s (matched source %s)", e.Reference, s)
					break
				}
			}
		}

		if pullAll || matchesSourceFilter {
			curImage, err := image.NewImage(e.Reference.String())
			if err != nil {
				return fmt.Errorf("pull image %s: %v", e.Reference, err)
			}
			it.contents[e.Reference.String()] = curImage
		}
	}

	return nil
}

type imageTxtEntry struct {
	Reference name.Reference
	Sources   map[string]bool
}

func splitImagesTxt(r io.Reader) ([]imageTxtEntry, error) {
	var entries []imageTxtEntry
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		curEntry := imageTxtEntry{
			Sources: make(map[string]bool),
		}

		lineContent := scanner.Text()
		if lineContent == "" || strings.HasPrefix(lineContent, "#") {
			// skip past empty and commented lines
			continue
		}
		splitContent := strings.Split(lineContent, " ")
		if len(splitContent) > 2 {
			return nil, fmt.Errorf(
				"invalid image.txt format: must contain only an image reference and sources separated by space; invalid line: %q",
				lineContent)
		}

		curRef, err := name.ParseReference(splitContent[0])
		if err != nil {
			return nil, fmt.Errorf("invalid reference %s: %v", splitContent[0], err)
		}
		curEntry.Reference = curRef

		if len(splitContent) == 2 {
			for _, source := range strings.Split(splitContent[1], ",") {
				curEntry.Sources[source] = true
			}
		}

		entries = append(entries, curEntry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan contents: %v", err)
	}

	return entries, nil
}
