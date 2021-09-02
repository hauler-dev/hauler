package registry

var (
	distributionContentDigestHeader = "Docker-Content-Digest"

	distributionAPIVersionHeader = "Docker-Distribution-Api-Version"
	distributionAPIVersion       = "registry/2.0"

	// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests
	nameRegexp      = `[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*`
	referenceRegexp = `[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}`
)
