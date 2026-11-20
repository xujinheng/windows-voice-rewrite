package audio

import "testing"

func TestRMSLevel(t *testing.T) {
	if RMSLevel(nil) != 0 || RMSLevel(make([]byte, 6400)) != 0 {
		t.Fatal("silence must be 0")
	}
	loud := make([]byte, 6400)
	for i := 0; i < len(loud); i += 2 {
		loud[i] = 0xff
		loud[i+1] = 0x7f
	}
	if got := RMSLevel(loud); got < 990 || got > 1000 {
		t.Fatalf("loud=%d", got)
	}
}
