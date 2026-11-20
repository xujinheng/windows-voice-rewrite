package speech

import (
	"context"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

func TestMultipartMeetingPromptIsOptionalAndDoesNotAffectDictation(t *testing.T) {
	cfg := coreconfig.Defaults()
	pcm := make([]byte, 640)
	ordinary, err := NewASRRequest(context.Background(), cfg, pcm)
	if err != nil {
		t.Fatal(err)
	}
	if err := ordinary.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	if _, present := ordinary.MultipartForm.Value["prompt"]; present {
		t.Fatal("ordinary dictation must not inherit Meeting context")
	}
	meeting, err := newASRRequestWithPrompt(context.Background(), cfg, pcm, "p99 = tail latency")
	if err != nil {
		t.Fatal(err)
	}
	if err := meeting.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	if got := meeting.FormValue("prompt"); got != "p99 = tail latency" {
		t.Fatalf("missing Meeting vocabulary: %q", got)
	}
}
