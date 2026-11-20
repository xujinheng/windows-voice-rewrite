package meeting

import (
	"strings"
	"testing"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"
)

func TestSampleMeetingStaysADemo(t *testing.T) {
	demo := Sample()
	if demo.ASRMode != config.MeetingASRMultipart || !strings.Contains(demo.SourceName, "Zoom.exe") || demo.Stage == ListeningStage || demo.Stage == ListeningPrompt || demo.Status == "" {
		t.Fatalf("demo %q %q %q", demo.ASRMode, demo.SourceName, demo.Stage)
	}
	if len(demo.Captions) != 2 || demo.Reply.Displayed.Lines[0].Kind != "ONE" || demo.Reply.Displayed.Lines[2].Kind != "THREE" || !demo.Reply.Displayed.Lines[1].Complete {
		t.Fatal("demo replies")
	}
	if demo.Expression.Kind != "SAY" || !demo.Expression.Complete || demo.Expression.English == "" {
		t.Fatal("demo expression")
	}
	if CopyFailed == ProfileSaveFailed || CopyFailed == ProfileOpenFailed || !strings.HasSuffix(CopyFailed, ": ") || strings.Contains(CopyFailed, "：") {
		t.Fatalf("copy %q", CopyFailed)
	}
	text := AfterProfileText(true)
	if text.TryMode || text.Close || text.StatusPrefix != ProfileSaveFailed {
		t.Fatal("profile text")
	}
	ready := AfterProfileText(false)
	if !ready.TryMode || ready.Close || ready.StatusPrefix != "" {
		t.Fatal("profile text ok")
	}
	mode := AfterProfileMode(true)
	if mode.Close || mode.StatusPrefix != ProfileModeFailed || mode.StatusPrefix == ProfileSaveFailed {
		t.Fatal("profile mode")
	}
	saved := AfterProfileMode(false)
	if !saved.Close || saved.StatusPrefix != "" {
		t.Fatal("profile saved")
	}
}
