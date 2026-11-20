package meeting

import (
	"strings"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

func TestMeetingASRProfileHintUsesBoundedBackgroundAndTerms(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.MeetingTerms = "专有名词：Aster = 艾斯特"
	cfg.MeetingBackground = "角色：基础设施工程师"
	hint := asrProfileHint(cfg)
	if !strings.Contains(hint, "Aster") || !strings.Contains(hint, "基础设施") {
		t.Fatalf("Meeting ASR omitted relevant vocabulary or topic: %q", hint)
	}
	cfg.MeetingTerms = strings.Repeat("术", 3000)
	cfg.MeetingBackground = strings.Repeat("背景", 2000)
	if n := len([]rune(asrProfileHint(cfg))); n > 830 {
		t.Fatalf("ASR context not bounded: %d", n)
	}
}
