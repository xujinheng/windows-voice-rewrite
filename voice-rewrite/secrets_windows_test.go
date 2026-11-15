//go:build windows

package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDPAPISecretsRoundTrip(t *testing.T) {
	secret := "private-test-12345"
	enc, err := encryptSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, dpapiPrefix) || strings.Contains(enc, secret) {
		t.Fatal("secret not encrypted")
	}
	dec, err := decryptSecret(enc)
	if err != nil || dec != secret {
		t.Fatalf("DPAPI round trip: %v", err)
	}
}

func TestDPAPIMalformedAndUndecryptableSecrets(t *testing.T) {
	for _, bad := range []string{
		dpapiPrefix + "not_base64!",
		dpapiPrefix,
		dpapiPrefix + base64.StdEncoding.EncodeToString([]byte("not a DPAPI blob")),
	} {
		if _, err := decryptSecret(bad); err == nil {
			t.Fatal("invalid DPAPI ciphertext accepted")
		}
	}
	cfg := defaults()
	cfg.ASRAPIKey = dpapiPrefix + "malformed!"
	if _, err := decryptConfigSecrets(cfg); err == nil {
		t.Fatal("invalid API key ciphertext accepted")
	}
}

func TestDPAPIEncryptDoesNotTrustInputPrefix(t *testing.T) {
	value := dpapiPrefix + "not-encrypted"
	stored, err := encryptSecret(value)
	if err != nil || stored == value {
		t.Fatalf("unencrypted prefixed input bypassed DPAPI: %v", err)
	}
	plain, err := decryptSecret(stored)
	if err != nil || plain != value {
		t.Fatalf("DPAPI prefix roundtrip: %v", err)
	}
}

func TestConfigLoadPropagatesDPAPIErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := "{\"asr_api_key\":\"dpapi:v1:invalid!\"}"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfigFrom(path)
	if err == nil || !strings.Contains(err.Error(), "无法解锁 API 凭据") {
		t.Fatalf("expected credential decryption error, got %v", err)
	}
}
