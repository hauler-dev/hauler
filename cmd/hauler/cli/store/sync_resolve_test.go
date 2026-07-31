package store

import (
	"strings"
	"testing"

	"github.com/mitchellh/go-homedir"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// --------------------------------------------------------------------------
// resolveImageJobs tests
//
// resolveImageJobs is a pure function (no ctx, no I/O) so these tests exercise
// the ~150 lines of precedence-resolution logic directly, without a store or
// network access.
// --------------------------------------------------------------------------

func TestResolveImageJobs_RegistryRelocation(t *testing.T) {
	tests := []struct {
		name        string
		imageName   string
		local       bool
		cliRegistry string
		annotation  string
		wantName    string
	}{
		{
			name:        "CLI flag wins over annotation",
			imageName:   "rancher/rancher:v2.9",
			cliRegistry: "cli-registry.io",
			annotation:  "annotation-registry.io",
			wantName:    "cli-registry.io/rancher/rancher:v2.9",
		},
		{
			name:       "annotation used when no CLI flag",
			imageName:  "rancher/rancher:v2.9",
			annotation: "annotation-registry.io",
			wantName:   "annotation-registry.io/rancher/rancher:v2.9",
		},
		{
			name:        "relocation skipped when ref already carries a registry",
			imageName:   "ghcr.io/rancher/rancher:v2.9",
			cliRegistry: "cli-registry.io",
			wantName:    "ghcr.io/rancher/rancher:v2.9",
		},
		{
			name:        "relocation skipped entirely when Local is true",
			imageName:   "rancher/rancher:v2.9",
			local:       true,
			cliRegistry: "cli-registry.io",
			wantName:    "rancher/rancher:v2.9",
		},
		{
			name:      "no registry flag or annotation leaves name unchanged",
			imageName: "rancher/rancher:v2.9",
			wantName:  "rancher/rancher:v2.9",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Registry: tc.cliRegistry}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationRegistry] = tc.annotation
			}
			images := []v1.Image{{Name: tc.imageName, Local: tc.local}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if got := jobs[0].img.Name; got != tc.wantName {
				t.Errorf("got name %q, want %q", got, tc.wantName)
			}
		})
	}
}

func TestResolveImageJobs_LocalWithVerificationOptions_ReturnsError(t *testing.T) {
	tests := []struct {
		name  string
		o     *flags.SyncOpts
		a     map[string]string
		image v1.Image
	}{
		{
			name:  "key via CLI",
			o:     &flags.SyncOpts{Key: "/some/key.pub"},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "key via annotation",
			o:     &flags.SyncOpts{},
			a:     map[string]string{consts.ImageAnnotationKey: "/some/key.pub"},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "key via per-image",
			o:     &flags.SyncOpts{},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true, Key: "/some/key.pub"},
		},
		{
			name:  "identity via CLI",
			o:     &flags.SyncOpts{CertIdentity: "someone@example.com"},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "identity via annotation",
			o:     &flags.SyncOpts{},
			a:     map[string]string{consts.ImageAnnotationCertIdentity: "someone@example.com"},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "identity via per-image",
			o:     &flags.SyncOpts{},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true, CertIdentity: "someone@example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jobs, err := resolveImageJobs(tc.o, tc.a, []v1.Image{tc.image})
			if err == nil {
				t.Fatalf("expected error, got nil (jobs=%v)", jobs)
			}
			if !strings.Contains(err.Error(), "--local cannot be combined with cosign verification options") {
				t.Errorf("unexpected error message: %v", err)
			}
			if len(jobs) != 0 {
				t.Errorf("expected no jobs appended, got %d", len(jobs))
			}
		})
	}
}

func TestResolveImageJobs_KeyPrecedence(t *testing.T) {
	homeKey, err := homedir.Expand("~/mykey.pub")
	if err != nil {
		t.Fatalf("homedir.Expand: %v", err)
	}

	tests := []struct {
		name       string
		cliKey     string
		annotation string
		imageKey   string
		wantKey    string
	}{
		{
			name:    "CLI only",
			cliKey:  "/cli/key.pub",
			wantKey: "/cli/key.pub",
		},
		{
			// Annotation only applies when the CLI key is unset — it does not
			// override an explicitly-set CLI key.
			name:       "annotation used when CLI key unset, expanded via homedir",
			annotation: "~/mykey.pub",
			wantKey:    homeKey,
		},
		{
			name:       "per-image overrides annotation and CLI, expanded via homedir",
			cliKey:     "/cli/key.pub",
			annotation: "/annotation/key.pub",
			imageKey:   "~/mykey.pub",
			wantKey:    homeKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Key: tc.cliKey}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationKey] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Key: tc.imageKey}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			job := jobs[0]
			if !job.needsPubKey {
				t.Fatalf("expected needsPubKey=true")
			}
			if job.key != tc.wantKey {
				t.Errorf("got key %q, want %q", job.key, tc.wantKey)
			}
		})
	}
}

func TestResolveImageJobs_TlogPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		cliTlog    bool
		annotation string
		imageTlog  bool
		wantTlog   bool
	}{
		{
			name:     "CLI true",
			cliTlog:  true,
			wantTlog: true,
		},
		{
			name:       "annotation true overrides CLI false",
			annotation: "true",
			wantTlog:   true,
		},
		{
			name:      "per-image true overrides annotation/CLI false",
			imageTlog: true,
			wantTlog:  true,
		},
		{
			name:     "all false stays false",
			wantTlog: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Key: "/cli/key.pub", Tlog: tc.cliTlog}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationTlog] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Tlog: tc.imageTlog}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].tlog != tc.wantTlog {
				t.Errorf("got tlog %v, want %v", jobs[0].tlog, tc.wantTlog)
			}
		})
	}
}

func TestResolveImageJobs_PlatformPrecedence(t *testing.T) {
	tests := []struct {
		name          string
		cliPlatform   string
		annotation    string
		imagePlatform string
		want          string
	}{
		{name: "CLI only", cliPlatform: "linux/amd64", want: "linux/amd64"},
		// Annotation only applies when the CLI platform is unset — it does not
		// override an explicitly-set CLI platform.
		{name: "annotation used when CLI platform unset", annotation: "linux/arm64", want: "linux/arm64"},
		{name: "per-image overrides annotation and CLI", cliPlatform: "linux/amd64", annotation: "linux/arm64", imagePlatform: "linux/386", want: "linux/386"},
		{name: "none set stays empty", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Platform: tc.cliPlatform}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationPlatform] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Platform: tc.imagePlatform}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].platform != tc.want {
				t.Errorf("got platform %q, want %q", jobs[0].platform, tc.want)
			}
		})
	}
}

func TestResolveImageJobs_RewritePrecedence(t *testing.T) {
	tests := []struct {
		name         string
		imageRewrite string
		want         string
	}{
		{name: "no per-image rewrite stays empty", want: ""},
		{name: "per-image rewrite propagates", imageRewrite: "myregistry.io/rancher", want: "myregistry.io/rancher"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{}
			a := map[string]string{}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Rewrite: tc.imageRewrite}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].rewrite != tc.want {
				t.Errorf("got rewrite %q, want %q", jobs[0].rewrite, tc.want)
			}
		})
	}
}

func TestResolveImageJobs_ExcludeExtrasPrecedence(t *testing.T) {
	tests := []struct {
		name             string
		cliExcludeExtras bool
		annotation       string
		imageExclude     bool
		want             bool
	}{
		{name: "CLI true", cliExcludeExtras: true, want: true},
		{name: "annotation true overrides CLI false", annotation: "true", want: true},
		{name: "per-image true overrides annotation/CLI false", imageExclude: true, want: true},
		{name: "all false stays false", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{ExcludeExtras: tc.cliExcludeExtras}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationExcludeExtras] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", ExcludeExtras: tc.imageExclude}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].excludeExtras != tc.want {
				t.Errorf("got excludeExtras %v, want %v", jobs[0].excludeExtras, tc.want)
			}
		})
	}
}

func TestResolveImageJobs_NoOptions_MinimalJob(t *testing.T) {
	o := &flags.SyncOpts{}
	images := []v1.Image{{Name: "rancher/rancher:v2.9"}}

	jobs, err := resolveImageJobs(o, nil, images)
	if err != nil {
		t.Fatalf("resolveImageJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.img.Name != "rancher/rancher:v2.9" {
		t.Errorf("got name %q", job.img.Name)
	}
	if job.local {
		t.Errorf("expected local=false")
	}
	if job.needsPubKey || job.needsKeyless {
		t.Errorf("expected no verification needed, got needsPubKey=%v needsKeyless=%v", job.needsPubKey, job.needsKeyless)
	}
	if job.platform != "" || job.rewrite != "" || job.excludeExtras {
		t.Errorf("expected zero-value platform/rewrite/excludeExtras, got platform=%q rewrite=%q excludeExtras=%v", job.platform, job.rewrite, job.excludeExtras)
	}
}

func TestResolveImageJobs_Local_NoOptions_AppendsLocalJob(t *testing.T) {
	o := &flags.SyncOpts{}
	images := []v1.Image{{Name: "rancher/rancher:v2.9", Local: true, Rewrite: "myregistry.io/rancher"}}

	jobs, err := resolveImageJobs(o, nil, images)
	if err != nil {
		t.Fatalf("resolveImageJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if !job.local {
		t.Errorf("expected local=true")
	}
	if job.rewrite != "myregistry.io/rancher" {
		t.Errorf("got rewrite %q", job.rewrite)
	}
}
