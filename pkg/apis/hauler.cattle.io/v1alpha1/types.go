package v1alpha1

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type EnvConfig struct {
	metav1.TypeMeta

	// IgnoreProxy instructs Hauler to ignore proxy configuration, even if
	// specified in a configuration source (CLI flag, environment variable, or
	// config file).
	IgnoreProxy bool `json:"ignoreProxy,omitempty"`

	// HTTPProxy represents the value of the HTTP_PROXY or
	// http_proxy environment variable. It will be used as the proxy
	// URL for HTTP requests and HTTPS requests unless overridden by
	// HTTPSProxy or NoProxy.
	HTTPProxy string `json:"httpProxy,omitempty"`

	// HTTPSProxy represents the HTTPS_PROXY or https_proxy
	// environment variable. It will be used as the proxy URL for
	// HTTPS requests unless overridden by NoProxy.
	HTTPSProxy string `json:"httpsProxy,omitempty"`

	// NoProxy represents the NO_PROXY or no_proxy environment
	// variable. It specifies a string that contains comma-separated values
	// specifying hosts that should be excluded from proxying. Each value is
	// represented by an IP address prefix (1.2.3.4), an IP address prefix in
	// CIDR notation (1.2.3.4/8), a domain name, or a special DNS label (*).
	// An IP address prefix and domain name can also include a literal port
	// number (1.2.3.4:80).
	// A domain name matches that name and all subdomains. A domain name with
	// a leading "." matches subdomains only. For example "foo.com" matches
	// "foo.com" and "bar.foo.com"; ".y.com" matches "x.y.com" but not "y.com".
	// A single asterisk (*) indicates that no proxying should be done.
	// A best effort is made to parse the string and errors are
	// ignored.
	NoProxy string `json:"noProxy,omitempty"`

	// TrustedCAFiles is a list of files to add to hauler's pool of trusted
	// Certificate Authorities. Relative paths set in a config file are resolved
	// relative to the file's location; relative paths passed as a struct are
	// resolved relative to the process's current working directory.
	TrustedCAFiles []string `json:"trustedCAFiles,omitempty"`

	// TrustedCAFiles is a list of base64-encoded certificates to add to
	// hauler's pool of trusted Certificate Authorities. The standard base64
	// encoding (base64.StdEncoding) must be used, as opposed to the URL-safe
	// encoding (base64.URLEncoding).
	TrustedCACerts []string `json:"trustedCACerts,omitempty"`
}

type PackageConfig struct {
	metav1.TypeMeta
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`

	Packages []Package `json:"packages,omitempty"`
}

type PackageType string

const (
	PackageTypeUnknown         PackageType = ""
	PackageTypeK3s             PackageType = "K3s"
	PackageTypeContainerImages PackageType = "ContainerImages"
	PackageTypeGitRepository   PackageType = "GitRepository"
	PackageTypeFileTree        PackageType = "FileTree"
)

var packageDefTypes = map[PackageType]bool{
	PackageTypeK3s:             true,
	PackageTypeContainerImages: true,
	PackageTypeGitRepository:   true,
	PackageTypeFileTree:        true,
}

func (t *PackageType) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		var ute *json.UnmarshalTypeError
		if errors.As(err, &ute) {
			return fmt.Errorf("PackageType must be a string: %w", ute)
		}
		return err
	}

	pdt := PackageType(s)
	if packageDefTypes[pdt] {
		*t = pdt
	} else {
		*t = PackageTypeUnknown
	}

	return nil
}

func (t PackageType) MarshalJSON() ([]byte, error) {
	var s string
	if packageDefTypes[t] {
		s = string(t)
	} else {
		s = string(PackageTypeUnknown)
	}

	return json.Marshal(s)
}

// TODO - refactor Package: easier unmarshal/marshal, split API server out of CLI

// Package defines a group of artifacts to collect into a package for future
// deployment. When converting from YAML or JSON, accepted fields depend on
// Type; for each PackageType, only the fields available in the corresponding
// definition (PackageK3s, PackageContainerImages, etc.) will be parsed and stored. Use
// GetDef to get the definition stored in a Package.
type Package struct {
	Name string      `json:"name,omitempty"`
	Type PackageType `json:"type,omitempty"`

	k3s             *PackageK3s
	containerImages *PackageContainerImages
	gitRepository   *PackageGitRepository
	fileTree        *PackageFileTree
}

func (pd Package) String() string {
	defStr := "nil"
	switch pd.Type {
	case PackageTypeK3s:
		if pd.k3s != nil {
			defStr = pd.k3s.String()
		}
	case PackageTypeContainerImages:
		if pd.containerImages != nil {
			defStr = pd.containerImages.String()
		}
	case PackageTypeGitRepository:
		if pd.gitRepository != nil {
			defStr = pd.gitRepository.String()
		}
	case PackageTypeFileTree:
		if pd.fileTree != nil {
			defStr = pd.fileTree.String()
		}
	default:
		// unknown type, nothing to do
	}
	return `Package{Name: "` + pd.Name + `", Type: "` + string(pd.Type) + `", Def: ` + defStr + `}`
}

// GetDef returns the definition stored in the Package based on Type.
func (pd Package) GetDef() interface{} {
	switch pd.Type {
	default:
		return nil
	case PackageTypeK3s:
		if pd.k3s == nil {
			return PackageK3s{}
		}
		return *pd.k3s
	case PackageTypeContainerImages:
		if pd.containerImages == nil {
			return PackageContainerImages{}
		}
		return *pd.containerImages
	case PackageTypeGitRepository:
		if pd.gitRepository == nil {
			return PackageGitRepository{}
		}
		return *pd.gitRepository
	case PackageTypeFileTree:
		if pd.fileTree == nil {
			return PackageFileTree{}
		}
		return *pd.fileTree
	}
}

// SetK3s stores a PackageK3s into the Package, altering Type as necessary.
func (pd *Package) SetK3s(d PackageK3s) {
	pd.Type = PackageTypeK3s
	pd.k3s = new(PackageK3s)
	*pd.k3s = d

	pd.containerImages = nil
	pd.gitRepository = nil
	pd.fileTree = nil
}

// SetContainerImages stores a PackageContainerImages into the Package, altering Type as necessary.
func (pd *Package) SetContainerImages(d PackageContainerImages) {
	pd.Type = PackageTypeContainerImages
	pd.containerImages = new(PackageContainerImages)
	*pd.containerImages = d

	pd.k3s = nil
	pd.gitRepository = nil
	pd.fileTree = nil
}

// SetGitRepository stores a PackageGitRepository into the Package, altering Type as necessary.
func (pd *Package) SetGitRepository(d PackageGitRepository) {
	pd.Type = PackageTypeK3s
	pd.gitRepository = new(PackageGitRepository)
	*pd.gitRepository = d

	pd.k3s = nil
	pd.containerImages = nil
	pd.fileTree = nil
}

// SetFileTree stores a PackageFileTree into the Package, altering Type as necessary.
func (pd *Package) SetFileTree(d PackageFileTree) {
	pd.Type = PackageTypeK3s
	pd.fileTree = new(PackageFileTree)
	*pd.fileTree = d

	pd.k3s = nil
	pd.containerImages = nil
	pd.gitRepository = nil
}

// TODO - refactor UnmarshalJSON
type internalPackageDef Package

func (pd *Package) UnmarshalJSON(b []byte) error {
	ipd := new(internalPackageDef)
	if err := json.Unmarshal(b, &ipd); err != nil {
		return fmt.Errorf("invalid Package name or type: %w", err)
	}
	*pd = Package(*ipd)

	var err error
	switch pd.Type {
	default:
		// unknown type, nothing to do
		return nil
	case PackageTypeK3s:
		pd.k3s = new(PackageK3s)
		err = json.Unmarshal(b, &pd.k3s)
	case PackageTypeContainerImages:
		pd.containerImages = new(PackageContainerImages)
		err = json.Unmarshal(b, &pd.containerImages)
	case PackageTypeGitRepository:
		pd.gitRepository = new(PackageGitRepository)
		err = json.Unmarshal(b, &pd.gitRepository)
	case PackageTypeFileTree:
		pd.fileTree = new(PackageFileTree)
		err = json.Unmarshal(b, &pd.fileTree)
	}

	if err != nil {
		return fmt.Errorf("invalid %s configuration: %w", pd.Type, err)
	}

	return nil
}

func (pd Package) MarshalJSON() ([]byte, error) {
	ipd := internalPackageDef(pd)
	baseBytes, err := json.Marshal(ipd)
	if err != nil {
		return nil, fmt.Errorf("marshal Package: %w", err)
	}

	var defBytes []byte
	switch pd.Type {
	default:
		// unknown type, nothing to do
		return baseBytes, nil
	case PackageTypeK3s:
		defBytes, err = json.Marshal(pd.k3s)
	case PackageTypeContainerImages:
		defBytes, err = json.Marshal(pd.containerImages)
	case PackageTypeGitRepository:
		defBytes, err = json.Marshal(pd.gitRepository)
	case PackageTypeFileTree:
		defBytes, err = json.Marshal(pd.fileTree)
	}

	if err != nil {
		return nil, fmt.Errorf("marshal %s Package fields: %w", pd.Type, err)
	}

	if string(defBytes) != "null" && len(defBytes) > 2 {
		baseBytes[len(baseBytes)-1] = ','
		baseBytes = append(baseBytes, defBytes[1:]...)
	}

	return baseBytes, nil
}

// PackageK3s is a configuration for packaging and deploying k3s. Currently always
// sources artifacts from GitHub.
type PackageK3s struct {
	// Release configures the GitHub release used for installation, defaulting to
	// the stable channel. NOTE: Prefer setting an explicit version for stability
	// and to allow repeatable deployments.
	Release string `json:"release,omitempty"`

	// InstallScriptRef configures the git reference (commit ID, branch name, or
	// tag) used to download the k3s installation bash script, defaulting to
	// "master". NOTE: Prefer setting an explicit, stable reference (commit ID or
	// tag) for stability and to allow repeatable deployments.
	InstallScriptRef string `json:"installScriptRef,omitempty"`
}

func (d PackageK3s) String() string {
	b := new(strings.Builder)
	b.WriteString("PackageK3s{")

	var fields []string
	if d.Release != "" {
		fields = append(fields, fmt.Sprintf("Release: %q", d.Release))
	}
	if d.InstallScriptRef != "" {
		fields = append(fields, fmt.Sprintf("InstallScriptRef: %q", d.InstallScriptRef))
	}

	if len(fields) != 0 {
		b.WriteString(strings.Join(fields, ", "))
	}

	b.WriteString("}")

	return b.String()
}

// PackageContainerImages is a configuration for packaging and deploying container
// images, either from a list of images or an image archive.
type PackageContainerImages struct {
	// ImageLists takes an array of URLs (http, https, and file are supported)
	// pointing to files listing container image tags; each file should have one
	// image reference per line. Image references will be defaulted according to
	// docker naming conventions. NOTE: Prefer fully specifying tags, including
	// usual defaults, to reduce confusion when using the deployed private
	// registry.
	ImageLists []string `json:"imageLists,omitempty"`

	// ImageArchives takes an array of URLs (http, https, and file are supported)
	// pointing to archives of container images. Archive must be a tar or gzipped
	// tar (.tar, .tar.gz, .tgz extensions). Archive contents must be an OCI
	// compliant image bundle.
	ImageArchives []string `json:"imageArchives"`
}

func (d PackageContainerImages) String() string {
	b := new(strings.Builder)
	b.WriteString("PackageContainerImages{")

	var fields []string
	if len(d.ImageLists) != 0 {
		fields = append(fields, fmt.Sprintf("ImageLists: %q", d.ImageLists))
	}
	if len(d.ImageArchives) != 0 {
		fields = append(fields, fmt.Sprintf("ImageArchives: %q", d.ImageArchives))
	}

	if len(fields) != 0 {
		b.WriteString(strings.Join(fields, ", "))
	}

	b.WriteString("}")

	return b.String()
}

// PackageGitRepository is a configuration for packaging and deploying a git
// repository.
type PackageGitRepository struct {
	// Repository configures the target git repository to serve. http, https, ssh,
	// and file URLs are supported.
	Repository string `json:"repository"`

	// HTTPSUsernameEnvVar configures the environment variable used to provide the
	// username for authenticating against an https git repository.
	HTTPSUsernameEnvVar string `json:"httpsUsernameEnvVar,omitempty"`

	// HTTPSPasswordEnvVar configures the environment variable used to provide the
	// password for authenticating against an https git repository.
	HTTPSPasswordEnvVar string `json:"httpsPasswordEnvVar,omitempty"`

	// SSHPrivateKeyPath configures the file used to provide the identity fot
	// authenticating against an ssh git repository.
	SSHPrivateKeyPath string `json:"sshPrivateKeyPath,omitempty"`
}

func (d PackageGitRepository) String() string {
	b := new(strings.Builder)
	b.WriteString("PackageGitRepository{")

	var fields []string
	if d.Repository != "" {
		fields = append(fields, fmt.Sprintf("Repository: %q", d.Repository))
	}
	if d.HTTPSUsernameEnvVar != "" {
		fields = append(fields, fmt.Sprintf("HTTPSUsernameEnvVar: %q", d.HTTPSUsernameEnvVar))
	}
	if d.HTTPSPasswordEnvVar != "" {
		fields = append(fields, fmt.Sprintf("HTTPSPasswordEnvVar: %q", d.HTTPSPasswordEnvVar))
	}
	if d.SSHPrivateKeyPath != "" {
		fields = append(fields, fmt.Sprintf("SSHPrivateKeyPath: %q", d.SSHPrivateKeyPath))
	}

	if len(fields) != 0 {
		b.WriteString(strings.Join(fields, ", "))
	}

	b.WriteString("}")

	return b.String()
}

// PackageFileTree is a configuration for packaging and deploying a file and
// directory hierarchy.
type PackageFileTree struct {
	// SourceBasePath points to the folder containing the first level of files to
	// be served in this tree. file URLs are currently supported.
	// For example, if file /home/user/source/shallow-a.txt and
	// /home/user/source/hauler-dir/nested-b.txt exist, a SourceBasePath
	// "file:///home/user/source" will collect the shallow-a.txt file and
	// hauler-dir directory.
	SourceBasePath string `json:"sourceBasePath"`

	// ServingBasePath configures the base path this tree will be served under.
	// For example, if files /home/user/source/shallow-a.txt and
	// /home/user/source/hauler-dir/nested-b.txt exist, a SourceBasePath of
	// "file:///home/user/source" and a ServingBasePath of "/example-base" will
	// result in /example-base/shallow-a.txt and
	// /example-base/hauler-dir/nested-b.txt being accessible files in the
	// deployed file server.
	ServingBasePath string `json:"servingBasePath"`
}

func (d PackageFileTree) String() string {
	b := new(strings.Builder)
	b.WriteString("PackageFileTree{")

	var fields []string
	if d.SourceBasePath != "" {
		fields = append(fields, fmt.Sprintf("SourceBasePath: %q", d.SourceBasePath))
	}
	if d.ServingBasePath != "" {
		fields = append(fields, fmt.Sprintf("ServingBasePath: %q", d.ServingBasePath))
	}

	if len(fields) != 0 {
		b.WriteString(strings.Join(fields, ", "))
	}

	b.WriteString("}")

	return b.String()
}
