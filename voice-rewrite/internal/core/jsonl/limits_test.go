package jsonl

import "testing"

func TestProtocolLimits(t *testing.T) {
	if Version != 1 || MaxRequestBytes != 8<<20 || MaxIDBytes != 128 {
		t.Fatalf("limits version=%d bytes=%d id=%d", Version, MaxRequestBytes, MaxIDBytes)
	}
}
