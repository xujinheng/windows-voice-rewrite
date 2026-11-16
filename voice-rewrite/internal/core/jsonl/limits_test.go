package jsonl

import "testing"

func TestProtocolLimits(t *testing.T) {
	if Version != 1 || MaxRequestBytes != 8<<20 || MaxIDBytes != 128 || MaxSeenIDs != 65536 {
		t.Fatalf("limits version=%d bytes=%d id=%d seen=%d", Version, MaxRequestBytes, MaxIDBytes, MaxSeenIDs)
	}
}
