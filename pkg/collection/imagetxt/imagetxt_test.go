package imagetxt

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rancherfederal/hauler/pkg/artifacts"
	"github.com/rancherfederal/hauler/pkg/artifacts/image"
)

var (
	ErrRefNotFound    = errors.New("ref not found")
	ErrRefNotImage    = errors.New("ref is not image")
	ErrExtraRefsFound = errors.New("extra refs found in contents")
)

var (
	testServer *httptest.Server
)

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() {
	dir := http.Dir("./testdata/http/")
	h := http.FileServer(dir)
	testServer = httptest.NewServer(h)
}

func teardown() {
	if testServer != nil {
		testServer.Close()
	}
}

type failKind string

const (
	failKindNew      = failKind("New")
	failKindContents = failKind("Contents")
)

func checkError(checkedFailKind failKind) func(*testing.T, error, bool, failKind) {
	return func(cet *testing.T, err error, testShouldFail bool, testFailKind failKind) {
		if err != nil {
			// if error should not have happened at all OR error should have happened
			// at a different point, test failed
			if !testShouldFail || testFailKind != checkedFailKind {
				cet.Fatalf("unexpected error at %s: %v", checkedFailKind, err)
			}
			// test should fail at this point, test passed
			return
		}
		// if no error occurred but error should have happened at this point, test
		// failed
		if testShouldFail && testFailKind == checkedFailKind {
			cet.Fatalf("unexpected nil error at %s", checkedFailKind)
		}
	}
}

func TestImageTxtCollection(t *testing.T) {
	type testEntry struct {
		Name           string
		Ref            string
		IncludeSources []string
		ExcludeSources []string
		ExpectedImages []string
		ShouldFail     bool
		FailKind       failKind
	}
	tt := []testEntry{
		{
			Name: "http ref basic",
			Ref:  fmt.Sprintf("%s/images-http.txt", testServer.URL),
			ExpectedImages: []string{
				"busybox",
				"nginx:1.19",
				"rancher/hyperkube:v1.21.7-rancher1",
				"docker.io/rancher/klipper-lb:v0.3.4",
				"quay.io/jetstack/cert-manager-controller:v1.6.1",
			},
		},
		{
			Name: "http ref sources format pull all",
			Ref:  fmt.Sprintf("%s/images-src-http.txt", testServer.URL),
			ExpectedImages: []string{
				"busybox",
				"nginx:1.19",
				"rancher/hyperkube:v1.21.7-rancher1",
				"docker.io/rancher/klipper-lb:v0.3.4",
				"quay.io/jetstack/cert-manager-controller:v1.6.1",
			},
		},
		{
			Name: "http ref sources format include sources A",
			Ref:  fmt.Sprintf("%s/images-src-http.txt", testServer.URL),
			IncludeSources: []string{
				"core", "rke",
			},
			ExpectedImages: []string{
				"busybox",
				"nginx:1.19",
				"rancher/hyperkube:v1.21.7-rancher1",
			},
		},
		{
			Name: "http ref sources format include sources B",
			Ref:  fmt.Sprintf("%s/images-src-http.txt", testServer.URL),
			IncludeSources: []string{
				"nginx", "rancher", "cert-manager",
			},
			ExpectedImages: []string{
				"nginx:1.19",
				"rancher/hyperkube:v1.21.7-rancher1",
				"docker.io/rancher/klipper-lb:v0.3.4",
				"quay.io/jetstack/cert-manager-controller:v1.6.1",
			},
		},
		{
			Name: "http ref sources format exclude sources A",
			Ref:  fmt.Sprintf("%s/images-src-http.txt", testServer.URL),
			ExcludeSources: []string{
				"cert-manager",
			},
			ExpectedImages: []string{
				"busybox",
				"nginx:1.19",
				"rancher/hyperkube:v1.21.7-rancher1",
				"docker.io/rancher/klipper-lb:v0.3.4",
			},
		},
		{
			Name: "http ref sources format exclude sources B",
			Ref:  fmt.Sprintf("%s/images-src-http.txt", testServer.URL),
			ExcludeSources: []string{
				"core",
			},
			ExpectedImages: []string{
				"nginx:1.19",
				"rancher/hyperkube:v1.21.7-rancher1",
				"docker.io/rancher/klipper-lb:v0.3.4",
				"quay.io/jetstack/cert-manager-controller:v1.6.1",
			},
		},
		{
			Name: "local file ref",
			Ref:  "./testdata/images-file.txt",
			ExpectedImages: []string{
				"busybox",
				"nginx:1.19",
				"rancher/hyperkube:v1.21.7-rancher1",
				"docker.io/rancher/klipper-lb:v0.3.4",
				"quay.io/jetstack/cert-manager-controller:v1.6.1",
			},
		},
	}

	checkErrorNew := checkError(failKindNew)
	checkErrorContents := checkError(failKindContents)

	for _, curTest := range tt {
		t.Run(curTest.Name, func(innerT *testing.T) {
			curImageTxt, err := New(curTest.Ref,
				WithIncludeSources(curTest.IncludeSources...),
				WithExcludeSources(curTest.ExcludeSources...),
			)
			checkErrorNew(innerT, err, curTest.ShouldFail, curTest.FailKind)

			ociContents, err := curImageTxt.Contents()
			checkErrorContents(innerT, err, curTest.ShouldFail, curTest.FailKind)

			if err := checkImages(ociContents, curTest.ExpectedImages); err != nil {
				innerT.Fatal(err)
			}
		})
	}
}

func checkImages(content map[string]artifacts.OCI, refs []string) error {
	contentCopy := make(map[string]artifacts.OCI, len(content))
	for k, v := range content {
		contentCopy[k] = v
	}
	for _, ref := range refs {
		target, ok := content[ref]
		if !ok {
			return fmt.Errorf("ref %s: %w", ref, ErrRefNotFound)
		}
		if _, ok := target.(*image.Image); !ok {
			return fmt.Errorf("got underlying type %T: %w", target, ErrRefNotImage)
		}
		delete(contentCopy, ref)
	}

	if len(contentCopy) != 0 {
		return ErrExtraRefsFound
	}

	return nil
}
