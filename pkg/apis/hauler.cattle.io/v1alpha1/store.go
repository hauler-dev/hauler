package v1alpha1

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	godigest "github.com/opencontainers/go-digest"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/provenance"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Store struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec StoreSpec `json:"spec"`
}

var refCleaner = regexp.MustCompile(`[!@#$%^&*()+={}'":;<>|\\]`)

type Getter interface {
	// Get opens an io.ReadCloser for the thing we're getting
	Get() (io.ReadCloser, error)
}

type StoreSpec struct {
	// Files represent an array of File's that live on disk
	Files []*File `json:"files"`

	// Https represent an array of Http's from a remote source that will be fetched over http(s)://
	Https []*Http `json:"https"`

	// Repos represent an array of Git's using the git:// protocol
	Repos []*Git `json:"repos"`

	// Charts represent an array of Helm Chart's that are stored as oci (tarballs)
	Charts []*Chart `json:"charts"`

	// Images represent an array of Image's that are sourced from remote repositories
	Images []*Image `json:"images"`
}

type File struct {
	Path      string `json:"path"`
	Checksum  string `json:"checksum,omitempty"`
	Canonical string `json:"canonical,omitempty"`
}

func (f *File) ToRef() name.Reference {
	cleanName := cleanFilepath(f.Canonical)
	ref, err := name.ParseReference(path.Join("hauler", cleanName))
	if err != nil {
		fmt.Println(err)
	}
	return ref
}

func (f *File) IsLocked() bool {
	return f.Checksum != "" && f.Canonical != ""
}

type Http struct {
	Name     string `json:"name"`
	Url      string `json:"url"`
	Checksum string `json:"checksum,omitempty"`
}

func (h Http) Get() (io.ReadCloser, error) {
	resp, err := http.Get(h.Url)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func (h *Http) Lock() error {
	body, err := h.Get()
	if err != nil {
		return err
	}
	defer body.Close()

	d, err := godigest.FromReader(body)

	h.Checksum = d.Encoded()

	return nil
}

type Git struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Commit string `json:"commit,omitempty"`
}

func (g *Git) Lock() error {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{g.Repo},
	})

	refs, err := rem.List(&git.ListOptions{})
	if err != nil {
		return err
	}

	// TODO: This is wrong
	g.Commit = refs[0].Hash().String()

	return nil
}

type Image struct {
	Reference string `json:"ref"`
	Canonical string `json:"canonical,omitempty"`
}

func (i *Image) Lock() error {
	ref, err := name.ParseReference(i.Reference)
	if err != nil {
		return err
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}

	h, _ := img.Digest()
	i.Canonical = fmt.Sprintf("%s@%s", ref.Context().Name(), h.String())

	return nil
}

type Chart struct {
	Name     string `json:"name"`
	Repo     string `json:"repo"`
	RepoURL  string `json:"repoUrl"`
	Version  string `json:"version"`
	Checksum string `json:"checksum,omitempty"`
}

func (c *Chart) ToRef() name.Reference {
	repo := path.Join(c.Repo, c.Name)
	ref, _ := name.ParseReference(fmt.Sprintf("%s:%s", repo, c.Version))
	return ref
}

func (c *Chart) Lock() error {
	settings := cli.New()

	cpo := action.ChartPathOptions{
		RepoURL: c.Repo,
	}

	cp, err := cpo.LocateChart(c.Name, settings)
	if err != nil {
		return err
	}

	// TODO: This is just one step of validating
	d, err := provenance.DigestFile(cp)
	if err != nil {
		return err
	}

	c.Checksum = d

	return nil
}

func cleanFilepath(filename string) string {
	base := filename[1+len(filepath.Dir(filename)) : len(filename)-len(filepath.Ext(filename))]
	return strings.ToLower(refCleaner.ReplaceAllString(base, "-"))
}
