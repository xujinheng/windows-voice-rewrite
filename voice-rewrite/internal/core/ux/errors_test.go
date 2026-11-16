package ux

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyKnownFailures(t *testing.T) {
	for _, tt := range []struct{ input, expected string }{
		{"ASR HTTP 415: unsupported", "音频格式"},
		{"LLM HTTP 401", "API 验证失败"},
		{"Post http://127.0.0.1:8080: connect: connection refused", "无法连接"},
		{"context deadline exceeded", "响应超时"},
		{"原选区已更改", "选区"},
	} {
		message, ok := Classify(errors.New(tt.input))
		if !ok || !strings.Contains(message, tt.expected) {
			t.Errorf("%q -> %q ok=%v", tt.input, message, ok)
		}
		if strings.Contains(message, tt.input) {
			t.Errorf("classified message echoed input %q", tt.input)
		}
	}
}

func TestClassifyLeavesUnknownErrorsUnmapped(t *testing.T) {
	secret := "private-selection-and-key"
	message, ok := Classify(errors.New("LLM HTTP 500: " + secret))
	if ok || message != "" {
		t.Fatalf("unknown error classified: %q", message)
	}
	if got := Present(errors.New(secret)); got != secret {
		t.Fatalf("present = %q", got)
	}
	if Present(nil) != "" {
		t.Fatal("nil error should be empty")
	}
}
