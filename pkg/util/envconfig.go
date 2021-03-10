package util

import (
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"

	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"

	"golang.org/x/net/http/httpproxy"
)

func ProxyFromEnvConfig(config v1alpha1.EnvConfig) func(*http.Request) (*url.URL, error) {
	proxyConfig := &httpproxy.Config{}

	// if not ignoring proxy (read: if using proxy), fill in proxy config fields
	if !config.IgnoreProxy {
		proxyConfig.HTTPProxy = config.HTTPProxy
		proxyConfig.HTTPSProxy = config.HTTPSProxy
		proxyConfig.NoProxy = config.NoProxy
	}
	proxyFunc := proxyConfig.ProxyFunc()

	return func(r *http.Request) (*url.URL, error) {
		return proxyFunc(r.URL)
	}
}

func TLSFromEnvConfig(config v1alpha1.EnvConfig) *tls.Config {
	if len(config.TrustedCAFiles) == 0 && len(config.TrustedCACerts) == 0 {
		// no extra root CAs to trust, use default TLS config
		return nil
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Printf("WARNING: error loading system cert pool: %v", err)
		rootCAs = new(x509.CertPool)
	}

	// trust all provided CA files
	for _, fileName := range config.TrustedCAFiles {
		fileContents, err := ioutil.ReadFile(fileName)
		if err != nil {
			log.Printf("WARNING: unable to add trusted certificate: read cert file %q: %v", fileName, err)
			continue
		}

		cert, err := x509.ParseCertificate(fileContents)
		if err != nil {
			log.Printf("WARNING: unable to add trusted certificate: parse cert file %q: %v", fileName, err)
			continue
		}
		rootCAs.AddCert(cert)
	}

	// trust all provided CA certs
	for i, certBase64 := range config.TrustedCACerts {
		certBase64Buf := bytes.NewBufferString(certBase64)
		certContents, err := ioutil.ReadAll(base64.NewDecoder(base64.StdEncoding, certBase64Buf))
		if err != nil {
			log.Printf("WARNING: unable to add trusted certificate: base64 decode cert %d: %v", i, err)
			continue
		}

		cert, err := x509.ParseCertificate(certContents)
		if err != nil {
			log.Printf("WARNING: unable to add trusted certificate: parse cert %d: %v", i, err)
			continue
		}
		rootCAs.AddCert(cert)
	}

	// trust all root CAs (system and additional) when making HTTPS requests
	tlsConfig := &tls.Config{
		RootCAs: rootCAs,
	}
	return tlsConfig
}
