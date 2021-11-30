package imagetxt

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/artifact/local"
	"github.com/rancherfederal/hauler/pkg/content/image"
	"github.com/rancherfederal/hauler/pkg/log"

	"github.com/google/go-containerregistry/pkg/name"
)

type ImageTxt struct {
	Ref            string
	IncludeSources map[string]bool
	ExcludeSources map[string]bool

	getter   local.Opener
	lock     *sync.Mutex
	computed bool
	contents map[name.Reference]artifact.OCI
}

var _ artifact.Collection = (*ImageTxt)(nil)

type Option interface {
	Apply(*ImageTxt) error
}

type withRef string

func (o withRef) Apply(it *ImageTxt) error {
	ref := string(o)

	if strings.HasPrefix(ref, "http") || strings.HasPrefix(ref, "https") {
		it.getter = local.RemoteOpener(ref)
	} else {
		it.getter = local.LocalOpener(ref)
	}
	return nil
}

func WithRef(ref string) Option {
	return withRef(ref)
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

func New(opts ...Option) (*ImageTxt, error) {
	it := &ImageTxt{
		lock: &sync.Mutex{},
	}

	for i, o := range opts {
		if err := o.Apply(it); err != nil {
			return nil, fmt.Errorf("invalid option %d: %v", i, err)
		}
	}

	return it, nil
}

func (it *ImageTxt) Contents() (map[name.Reference]artifact.OCI, error) {
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

	it.contents = make(map[name.Reference]artifact.OCI)

	r, err := it.getter()
	if err != nil {
		return fmt.Errorf("fetch image.txt ref %s: %v", it.Ref, err)
	}
	defer r.Close()

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r); err != nil {
		return fmt.Errorf("read image.txt ref %s: %v", it.Ref, err)
	}

	entries, err := splitImagesTxt(buf)
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
	var targetSources map[string]bool

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
			it.contents[e.Reference] = curImage
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
