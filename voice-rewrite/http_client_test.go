package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"voicerewrite/internal/core/provider"
)

func TestProviderRefusesRedirectsToUnconfiguredHosts(t *testing.T) {
	var leaked bool
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer dest.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	response, err := provider.RequestClient(time.Second).Post(source.URL, "text/plain", strings.NewReader("private transcript"))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if leaked || response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatal("redirected private request")
	}
}

func TestServerURLsRejectSecretBearingComponents(t *testing.T) {
	for _, bad := range []string{"http://a:pass@127.0.0.1:8080/v1", "http://localhost:1234/v1?api_key=secret", "http://localhost/v1#fragment", "file:///tmp/voice"} {
		if provider.ValidateBaseURL(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := provider.ValidateBaseURL("http://127.0.0.1:8080/v1"); err != nil {
		t.Fatal(err)
	}
}
func TestHTTPErrorDoesNotEchoProviderBody(t *testing.T) {
	if strings.Contains(provider.HTTPError("ASR", 415).Error(), "secret") {
		t.Fatal("leaked error body")
	}
}
