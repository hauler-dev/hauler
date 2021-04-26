package v1alpha1

import (
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
	// Certificate Authorities. All relative paths are resolved relative to the
	// process's current working directory unless specified otherwise.
	TrustedCAFiles []string `json:"trustedCAFiles,omitempty"`

	// TrustedCAFiles is a list of base64-encoded certificates to add to
	// hauler's pool of trusted Certificate Authorities. The standard base64
	// encoding (base64.StdEncoding) must be used, as opposed to the URL-safe
	// encoding (base64.URLEncoding).
	TrustedCACerts []string `json:"trustedCACerts,omitempty"`
}

type Package struct {
	metav1.TypeMeta
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`

	Driver Driver
	Images []Image `json:"images,omitempty"`
	Manifests []GitRepository `json:"repos,omitempty"`
}

type Image string

type File struct {
	Source string
}

// GitRepository is a configuration for packaging and deploying a git
// repository.
type GitRepository struct {
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
