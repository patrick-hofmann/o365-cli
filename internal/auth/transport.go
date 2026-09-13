package auth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The host supplies a public CA snapshot when platform trust services are unavailable in its sandbox.
func PodsHTTPClient() (*http.Client, error) {
	path := os.Getenv("PODS_CA_FILE")
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("Pods CA snapshot path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, errors.New("invalid Pods CA snapshot")
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read Pods CA snapshot")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("Pods CA snapshot contains no certificates")
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("authentication redirects are not permitted")
	}}, nil
}
