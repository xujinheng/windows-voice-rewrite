package meeting

import (
	"testing"
	"time"
)

func TestReplyAndRefineStayOnTheirOwnSentences(t *testing.T) {
	if ReplyTimeout != 30*time.Second || ReplyTimeout == ExpressionTimeout {
		t.Fatal(ReplyTimeout)
	}
	if InferenceStage("caption", false) != StageTranslating || InferenceStage("replies", false) != StageReplies || InferenceStage("replies", true) != StageExpressing {
		t.Fatal("stage order")
	}
	if status, start := ReplyRequest(true, true); start || status != "" {
		t.Fatal("paused replies")
	}
	if status, start := ReplyRequest(false, false); start || status != RepliesWait {
		t.Fatal(status, start)
	}
	if _, start := ReplyRequest(false, true); !start {
		t.Fatal("start")
	}
	got := ReplyPrior([]string{" earlier ", " same "}, "same")
	if len(got) != 1 || got[0] != " earlier " {
		t.Fatal(got)
	}
	if !RefineMayRun("en", false) || RefineMayRun("", false) || RefineMayRun("en", true) {
		t.Fatal("refine")
	}
	if CapturePausedStatus == "采集已暂停 · 不再接收音频" || RepliesPreparing == RepliesFailed || RefineRunning == RefineFailed {
		t.Fatal("sentences collapsed")
	}
}
