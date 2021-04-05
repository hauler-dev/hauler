package packager

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/archive"
	"github.com/rancherfederal/hauler/pkg/util"
)

type Packager struct {
	httpClient *http.Client
}

func New(config *v1alpha1.EnvConfig) *Packager {
	if config == nil {
		config = new(v1alpha1.EnvConfig)
	}

	httpTransport := http.DefaultTransport.(*http.Transport).Clone()

	httpTransport.Proxy = util.ProxyFromEnvConfig(*config)
	httpTransport.TLSClientConfig = util.TLSFromEnvConfig(*config)

	httpClient := &http.Client{
		Transport: httpTransport,
	}

	return &Packager{
		httpClient: httpClient,
	}
}

func (p *Packager) Package(
	dst io.Writer,
	pkgConfig v1alpha1.PackageConfig,
) error {
	// TODO - allow changing writer kind
	// wrap around dst
	aw, err := archive.NewWriter(dst, archive.WriterKindTar)
	if err != nil {
		return fmt.Errorf("create archive writer: %v", err)
	}
	defer aw.Close()

	var packageErrors []error
	for _, pkg := range pkgConfig.Packages {
		switch cfg := pkg.GetDef().(type) {
		case v1alpha1.PackageK3s:
			if err := p.PackageK3s(aw, pkg, cfg); err != nil {
				packageErrors = append(
					packageErrors,
					fmt.Errorf("package id %s: collect k3s: %w", pkg.Name, err),
				)
			}
		default:
			packageErrors = append(
				packageErrors,
				fmt.Errorf("package id %s: unknown package type", pkg.Name),
			)
		}
	}

	if len(packageErrors) != 0 {
		b := &strings.Builder{}
		b.WriteString("package errors:")
		for _, err := range packageErrors {
			b.WriteString("\n")
			b.WriteString(err.Error())
		}
		return errors.New(b.String())
	}

	return nil
}

const (
	k3sReleaseDownloadFmtStr   = `https://github.com/k3s-io/k3s/releases/download/%s/%s`
	k3sRawFmtStr               = `https://raw.githubusercontent.com/k3s-io/k3s/%s/%s`
	k3sDefaultInstallScriptRef = "master"
)

func (p *Packager) PackageK3s(
	writer *archive.Writer,
	pkg v1alpha1.Package,
	config v1alpha1.PackageK3s,
) error {
	log.Printf("[debug] k3s package config %s\n", pkg.Name)
	log.Printf("[debug] k3s release %s\n", config.Release)
	log.Printf("[debug] install script ref %s\n", config.InstallScriptRef)

	if config.InstallScriptRef == "" {
		config.InstallScriptRef = k3sDefaultInstallScriptRef
	}

	// TODO - refactor artifact download into function or helper

	// ===========================================================================
	// download k3s amd64 binary
	k3sBinCtx, k3sBinCancel := context.WithTimeout(context.TODO(), 20*time.Minute)
	// TODO - scope cancel call better than the end of this entire function
	defer k3sBinCancel()

	k3sBinUrlStr := fmt.Sprintf(k3sReleaseDownloadFmtStr, url.QueryEscape(config.Release), "k3s")
	k3sBinReq, err := http.NewRequestWithContext(k3sBinCtx, "GET", k3sBinUrlStr, nil)
	if err != nil {
		return fmt.Errorf("malformed http request: %v", err)
	}

	log.Printf("[debug] begin k3s binary download")

	k3sBinRes, err := p.httpClient.Do(k3sBinReq)
	if err != nil {
		return fmt.Errorf(
			"download k3s version %s binary: %v",
			config.Release, err,
		)
	}
	// TODO - scope Close call better than the end of this entire function
	defer k3sBinRes.Body.Close()

	if k3sBinRes.StatusCode != 200 {
		bodyB64 := ""
		builder := &strings.Builder{}
		enc := base64.NewEncoder(base64.StdEncoding, builder)
		if _, err := io.Copy(enc, k3sBinRes.Body); err != nil {
			log.Printf("[warn] error fetching k3s version %s binary response body: %v", config.Release, err)
		} else {
			bodyB64 = builder.String()
		}

		if k3sBinRes.StatusCode == 404 {
			return fmt.Errorf(
				"k3s version %s binary artifact not found - the version may not be a valid k3s release",
				config.Release,
			)
		}

		return fmt.Errorf(
			"downloadk3s version %s binary: bad http response: url %s, status %s, body (base64 encoded) %s",
			config.Release, k3sBinUrlStr, k3sBinRes.Status, bodyB64,
		)
	}

	var k3sBinFileSize int64
	var k3sBinReader io.Reader

	k3sBinContentLengthStr := k3sBinRes.Header.Get("content-length")
	if contentLength, err := strconv.ParseInt(k3sBinContentLengthStr, 10, 64); err == nil {
		// have content-length header with valid number; use as file length
		k3sBinFileSize = contentLength
		k3sBinReader = k3sBinRes.Body
	} else {
		// unable to use content-length header for file size; copy to Buffer instead
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, k3sBinRes.Body); err != nil {
			return fmt.Errorf(
				"failed copying k3s version %s binary into RAM from network response",
				config.Release,
			)
		}

		k3sBinFileSize = int64(buf.Len())
		k3sBinReader = buf
	}

	if err := writer.CreateFile("k3s", pkg.Name, "k3s", k3sBinFileSize); err != nil {
		return fmt.Errorf("create k3s version %s binary in destination archive: %v",
			config.Release, err,
		)
	}
	if _, err := io.Copy(writer, k3sBinReader); err != nil {
		return fmt.Errorf("write k3s version %s binary to destination archive: %v",
			config.Release, err,
		)
	}

	// ===========================================================================
	// download k3s image archive
	k3sImagesCtx, k3sImagesCancel := context.WithTimeout(context.TODO(), 20*time.Minute)
	// TODO - scope cancel call better than the end of this entire function
	defer k3sImagesCancel()

	k3sImagesUrlStr := fmt.Sprintf(k3sReleaseDownloadFmtStr, url.QueryEscape(config.Release), "k3s-airgap-images-amd64.tar")
	k3sImagesReq, err := http.NewRequestWithContext(k3sImagesCtx, "GET", k3sImagesUrlStr, nil)
	if err != nil {
		return fmt.Errorf("malformed http request: %v", err)
	}

	log.Printf("[debug] begin k3s image archive download")

	k3sImagesRes, err := p.httpClient.Do(k3sImagesReq)
	if err != nil {
		return fmt.Errorf(
			"download k3s version %s image archive: %v",
			config.Release, err,
		)
	}
	// TODO - scope Close call better than the end of this entire function
	defer k3sImagesRes.Body.Close()

	if k3sImagesRes.StatusCode != 200 {
		bodyB64 := ""
		builder := &strings.Builder{}
		enc := base64.NewEncoder(base64.StdEncoding, builder)
		if _, err := io.Copy(enc, k3sImagesRes.Body); err != nil {
			log.Printf("[warn] error fetching k3s image archive response body: %v", err)
		} else {
			bodyB64 = builder.String()
		}

		if k3sImagesRes.StatusCode == 404 {
			return fmt.Errorf(
				"k3s version %s image archive artifact not found - the version may not be a valid k3s release",
				config.Release,
			)
		}

		return fmt.Errorf(
			"download k3s version %s image archive: bad http response: url %s, status %s, body (base64 encoded) %s",
			config.Release, k3sImagesUrlStr, k3sImagesRes.Status, bodyB64,
		)
	}

	var k3sImagesFileSize int64
	var k3sImagesReader io.Reader

	k3sImagesContentLengthStr := k3sImagesRes.Header.Get("content-length")
	if contentLength, err := strconv.ParseInt(k3sImagesContentLengthStr, 10, 64); err == nil {
		// have content-length header with valid number; use as file length
		k3sImagesFileSize = contentLength
		k3sImagesReader = k3sImagesRes.Body
	} else {
		// unable to use content-length header for file size; copy to Buffer instead
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, k3sImagesRes.Body); err != nil {
			return fmt.Errorf(
				"failed copying k3s version %s image archive into RAM from network response",
				config.Release,
			)
		}

		k3sImagesFileSize = int64(buf.Len())
		k3sImagesReader = buf
	}

	if err := writer.CreateFile("k3s", pkg.Name, "k3s-airgap-images-amd64.tar", k3sImagesFileSize); err != nil {
		return fmt.Errorf("create k3s version %s image archive in destination archive: %v",
			config.Release, err,
		)
	}
	if _, err := io.Copy(writer, k3sImagesReader); err != nil {
		return fmt.Errorf("write k3s version %s image archive to destination archive: %v",
			config.Release, err,
		)
	}

	// ===========================================================================
	// download k3s install.sh script
	k3sInstallCtx, k3sInstallCancel := context.WithTimeout(context.TODO(), 20*time.Minute)
	// TODO - scope cancel call better than the end of this entire function
	defer k3sInstallCancel()

	k3sInstallUrlStr := fmt.Sprintf(k3sRawFmtStr, url.QueryEscape(config.InstallScriptRef), "install.sh")
	k3sInstallReq, err := http.NewRequestWithContext(k3sInstallCtx, "GET", k3sInstallUrlStr, nil)
	if err != nil {
		return fmt.Errorf("malformed http request: %v", err)
	}

	log.Printf("[debug] begin k3s install.sh download")

	k3sInstallRes, err := p.httpClient.Do(k3sInstallReq)
	if err != nil {
		return fmt.Errorf(
			"download k3s ref %s install.sh: %v",
			config.InstallScriptRef, err,
		)
	}
	// TODO - scope Close call better than the end of this entire function
	defer k3sInstallRes.Body.Close()

	if k3sInstallRes.StatusCode != 200 {
		bodyB64 := ""
		builder := &strings.Builder{}
		enc := base64.NewEncoder(base64.StdEncoding, builder)
		if _, err := io.Copy(enc, k3sInstallRes.Body); err != nil {
			log.Printf("[warn] error fetching ref %s install.sh response body: %v", config.InstallScriptRef, err)
		} else {
			bodyB64 = builder.String()
		}

		if k3sInstallRes.StatusCode == 404 {
			return fmt.Errorf(
				"k3s ref %s install.sh artifact not found - the version may not be a valid k3s release",
				config.Release,
			)
		}

		return fmt.Errorf(
			"download k3s binary: bad http response: url %s, status %s, body (base64 encoded) %s",
			k3sInstallUrlStr, k3sInstallRes.Status, bodyB64,
		)
	}

	var k3sInstallFileSize int64
	var k3sInstallReader io.Reader

	k3sInstallContentLengthStr := k3sInstallRes.Header.Get("content-length")
	if contentLength, err := strconv.ParseInt(k3sInstallContentLengthStr, 10, 64); err == nil {
		// have content-length header with valid number; use as file length
		k3sInstallFileSize = contentLength
		k3sInstallReader = k3sInstallRes.Body
	} else {
		// unable to use content-length header for file size; copy to Buffer instead
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, k3sInstallRes.Body); err != nil {
			return fmt.Errorf(
				"failed copying k3s ref %s install.sh into RAM from network response",
				config.Release,
			)
		}

		k3sInstallFileSize = int64(buf.Len())
		k3sInstallReader = buf
	}

	if err := writer.CreateFile("k3s", pkg.Name, "install.sh", k3sInstallFileSize); err != nil {
		return fmt.Errorf("create k3s ref %s install.sh in destination archive: %v",
			config.Release, err,
		)
	}
	if _, err := io.Copy(writer, k3sInstallReader); err != nil {
		return fmt.Errorf("write k3s ref %s install.sh to destination archive: %v",
			config.Release, err,
		)
	}

	return nil
}

func (p *Packager) PackageContainerImages(
	writer *archive.Writer,
	config v1alpha1.PackageContainerImages,
) error {
	return nil
}

func (p *Packager) PackageGitRepository(
	writer *archive.Writer,
	config v1alpha1.PackageGitRepository,
) error {
	return nil
}

func (p *Packager) PackageFileTree(
	writer *archive.Writer,
	config v1alpha1.PackageFileTree,
) error {
	return nil
}
