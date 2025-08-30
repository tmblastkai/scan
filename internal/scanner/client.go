package scanner

import (
	"crypto/tls"
	"net/http"
	"time"
)

// newHTTPClient returns an HTTP client with the provided timeout and TLS
// verification disabled for all requests. This ensures preflight checks succeed
// even when the server presents an invalid certificate or redirects from HTTP
// to HTTPS.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
}
