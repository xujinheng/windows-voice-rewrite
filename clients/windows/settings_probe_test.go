package main

import (
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

func TestModelProbeRejectsTokenInURL(t *testing.T) {
	if coreconfig.ValidateBaseURL("http://localhost:8080/v1?api_key=secret") == nil {
		t.Fatal("API base URL must not contain query secrets")
	}
	if coreconfig.ValidateBaseURL("https://localhost:8080/v1") != nil {
		t.Fatal("valid local secure URL rejected")
	}
}
