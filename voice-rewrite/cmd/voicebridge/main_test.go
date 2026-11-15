package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHelloProtocol(t *testing.T) {
	var output bytes.Buffer
	input := strings.NewReader("{\"v\":1,\"id\":\"mac-probe\",\"op\":\"hello\"}\n")
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version int             `json:"v"`
		ID      string          `json:"id"`
		OK      bool            `json:"ok"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.ID != "mac-probe" || !got.OK || !bytes.Contains(got.Result, []byte("\"protocol\":1")) {
		t.Fatalf("unexpected response: %s", output.String())
	}
}

func TestBridgeRejectsUnsupportedAndMalformedRequests(t *testing.T) {
	private := "private-auth-token-never-log"
	input := strings.NewReader("{\"v\":1,\"id\":\"a\",\"op\":\"unknown\"}\n" +
		"{\"v\":9,\"id\":\"b\",\"op\":\"hello\"}\n" +
		"{\"v\":1,\"id\":\"c\",\"op\":\"hello\",\"secret\":\""+private+"\"}\n" +
		"{not-json}\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d responses: %s", len(lines), output.String())
	}
	if strings.Contains(output.String(), private) {
		t.Fatal("request secret leaked into response")
	}
	for i, want := range []string{"unsupported_op", "protocol_version", "\"ok\":true", "invalid_json"} {
		if !strings.Contains(lines[i], want) {
			t.Fatalf("response %d should contain %q: %s", i, want, lines[i])
		}
	}
}
