package ux

import (
	"errors"
	"strings"
	"testing"

	"voicerewrite/internal/core/shortcut"
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

func TestClassifyForwardsOwnedSentences(t *testing.T) {
	for _, message := range []string{NoSpeech, EmptySelection, DictationEmpty, "选中文字超过 12 字限制"} {
		got, ok := Classify(errors.New(message))
		if !ok || got != message {
			t.Fatalf("%q -> %q ok=%v", message, got, ok)
		}
	}
	if _, ok := Classify(errors.New("选中文字超过 abc 字限制")); ok {
		t.Fatal("non-numeric limit forwarded")
	}
	if _, ok := Classify(errors.New(NoSpeech + " sk-secret")); ok {
		t.Fatal("provider suffix forwarded")
	}
}

func TestHostMessageKeepsCatalogSentences(t *testing.T) {
	for _, message := range []string{
		NoSpeech, DictationTooShort, RewriteTooShort, PasswordDictation,
		authFailed, "选中文字超过 9 字限制",
	} {
		if got := HostMessage("ASR 请求失败：", message); got != message {
			t.Fatalf("catalog sentence prefixed: %q", got)
		}
	}
	const prefix = "模型处理失败："
	if got := HostMessage(prefix, "speech transcription failed"); got != prefix+"speech transcription failed" {
		t.Fatalf("unclassified = %q", got)
	}
	if got := HostMessage("ASR 请求失败：", NoSpeech+" sk-secret"); got != "ASR 请求失败："+NoSpeech+" sk-secret" {
		t.Fatalf("suffixed catalog sentence forwarded: %q", got)
	}
	if got := HostMessage("快捷键无效：", shortcut.ShiftLetterMessage); got != shortcut.ShiftLetterMessage {
		t.Fatalf("shortcut sentence prefixed: %q", got)
	}
	unknown := "未知修饰键 \"Win\""
	if got := HostMessage("快捷键无效：", unknown); got != unknown {
		t.Fatalf("modifier sentence prefixed: %q", got)
	}
	if got := HostMessage("快捷键无效：", "Helper 未运行"); got != "快捷键无效：Helper 未运行" {
		t.Fatalf("transport error = %q", got)
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
