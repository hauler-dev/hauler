package image

import (
	"context"
	"fmt"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	"github.com/containers/image/v5/copy"
	"os"
)

type Fetcher interface {
	Save() error
}

type client struct {
	policyContext *signature.PolicyContext
}

func NewClient() (*client, error) {
	p, err := DefaultPolicyContext()
	if err != nil {
		return nil, err
	}

	c := &client{
		policyContext: p,
	}

	return c, nil
}

func (c client) Save(ctx context.Context, src string, dest string) error {
	srcCtx := DefaultSystemContext()
	destCtx := DefaultSystemContext()

	srcRef, err := alltransports.ParseImageName(fmt.Sprintf("docker://%s", src))
	if err != nil {
		return err
	}

	destRef, err := alltransports.ParseImageName(fmt.Sprintf("oci-archive:%s", dest))
	if err != nil {
		return err
	}

	manifest, err := copy.Image(ctx, c.policyContext, destRef, srcRef, &copy.Options{
		RemoveSignatures:                      false,
		SignBy:                                "",
		ReportWriter:                          os.Stdout,
		SourceCtx:                             srcCtx,
		DestinationCtx:                        destCtx,
		ProgressInterval:                      0,
		Progress:                              nil,
		ForceManifestMIMEType:                 "",
		//ImageListSelection:                    copy.CopySpecificImages,
		Instances:                             nil,
	})
	if err != nil {
		return err
	}

	_ = manifest

	return nil
}

func DefaultSystemContext() *types.SystemContext {
	ctx := &types.SystemContext{
		RegistriesDirPath:                 "",
		SystemRegistriesConfPath:          "",
		//AuthFilePath:                      "",
		ArchitectureChoice:                "amd64",
		OSChoice:                          "linux",
		//VariantChoice:                     "amd",
		BigFilesTemporaryDir:              "",
		OCIInsecureSkipTLSVerify:          false,
		OCISharedBlobDirPath:              "",
		OCIAcceptUncompressedLayers:       false,
		DockerCertPath:                    "",
		DockerPerHostCertDirPath:          "",
		DockerInsecureSkipTLSVerify:       0,
		DockerAuthConfig:                  nil,
		DockerBearerRegistryToken:         "",
		DockerRegistryUserAgent:           "",
		DockerDisableV1Ping:               false,
		DockerDisableDestSchema1MIMETypes: false,
		DockerLogMirrorChoice:             false,
		OSTreeTmpDirPath:                  "",
		DockerDaemonCertPath:              "",
		DockerDaemonHost:                  "",
		DockerDaemonInsecureSkipTLSVerify: false,
		DirForceCompress:                  false,
		CompressionFormat:                 nil,
		CompressionLevel:                  nil,
	}

	return ctx
}

func DefaultPolicyContext() (*signature.PolicyContext, error) {
	var policy *signature.Policy

	policy, err := signature.DefaultPolicy(nil)
	if err != nil {
		return nil, err
	}

	return signature.NewPolicyContext(policy)
}