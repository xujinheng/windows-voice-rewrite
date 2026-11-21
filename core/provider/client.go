// Package provider enforces common HTTP client privacy constraints on all platforms.
package provider

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

var providerTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 32
	t.MaxIdleConnsPerHost = 8
	t.IdleConnTimeout = 90 * time.Second
	t.ResponseHeaderTimeout = 75 * time.Second
	return t
}()

// Redirecting a speech or text request to an unconfigured host can leak
// private voice recordings, text and credentials. Treat redirects as errors.
func RequestClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: providerTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func Endpoint(base, suffix string) string { return strings.TrimRight(base, "/") + suffix }

// HTTPStatusError preserves the provider status without parsing localized
// error strings. No response body (which may contain user content) is exposed.
type HTTPStatusError struct {
	Service string
	Status  int
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("%s HTTP %d", e.Service, e.Status) }

// Only a concise, non-sensitive status goes to UI or logs.
func HTTPError(service string, status int) error {
	return &HTTPStatusError{Service: service, Status: status}
}
