package provider

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderRefusesRedirectsToUnconfiguredHosts(t *testing.T) {
	var leaked bool
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer dest.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	response, err := RequestClient(time.Second).Post(source.URL, "text/plain", strings.NewReader("private transcript"))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if leaked || response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatal("redirected private request")
	}
}

func TestHTTPErrorDoesNotEchoProviderBody(t *testing.T) {
	err := HTTPError("ASR", 415)
	var status *HTTPStatusError
	if strings.Contains(err.Error(), "secret") || err.Error() != "ASR HTTP 415" || !errors.As(err, &status) || status.Status != 415 {
		t.Fatal("status code or safe message was lost", err)
	}
}
