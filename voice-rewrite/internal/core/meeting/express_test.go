package meeting

import (
	"errors"
	"testing"
	"time"
)

func TestMeetingExpressionStopsStaySeparate(t *testing.T) {
	if ExpressionLabel("compose") != ExpressComposing || ExpressionLabel("translate") != ExpressTranslating {
		t.Fatal(ExpressionLabel("compose"), ExpressionLabel(""))
	}
	if ExpressionTimeout != 50*time.Second || PriorTurns != 5 {
		t.Fatal(ExpressionTimeout, PriorTurns)
	}
	label, detail, stop := ExpressCapture(errors.New("mic"), true)
	if !stop || label != ExpressMicFailed || detail != "" {
		t.Fatalf("mic wins %s %s", label, detail)
	}
	label, detail, stop = ExpressCapture(nil, true)
	if !stop || label != ExpressTooShort || detail != ExpressTooShortDetail || detail == "语音太短，无法转写" || detail == "录音太短（至少约 0.1 秒）；请再试一次" {
		t.Fatal(label, detail)
	}
	if _, _, stop = ExpressCapture(nil, false); stop {
		t.Fatal("usable capture")
	}
	label, detail, points, stop := ExpressTranscript(errors.New("asr"), "  hi ")
	if !stop || label != ExpressASRFailed || points != "" {
		t.Fatal(label, points)
	}
	label, detail, points, stop = ExpressTranscript(nil, " \n ")
	if !stop || label != ExpressEmpty || detail != ExpressEmptyDetail {
		t.Fatal(label, detail)
	}
	_, _, points, stop = ExpressTranscript(nil, "  你好 ")
	if stop || points != "你好" {
		t.Fatal(points, stop)
	}
	got := RememberPrior([]string{"1", "2", "3", "4", "5"}, "6")
	if len(got) != 5 || got[0] != "2" || got[4] != "6" {
		t.Fatal(got)
	}
	if !MeetingSourceLess("Zoom.exe", "b", "chrome.exe", "a") || !MeetingSourceLess("zoom.EXE", "z", "Notepad.exe", "a") {
		t.Fatal("zoom first")
	}
	if !MeetingSourceLess("a.exe", "alpha", "b.exe", "Beta") || MeetingSourceLess("a.exe", "Same", "b.exe", "same") {
		t.Fatal("title order")
	}
	if ListeningStage == ListeningPrompt || CaptionRecognizing == ExpressRecognizing || CaptionRecognizing == "正在识别语音" {
		t.Fatal("stage sentences collapsed")
	}
	if MeetingSaveUnavailable == "保存失败：" || UnknownMeetingASRMode == "" {
		t.Fatal("save")
	}
	if ListeningPrompt == "正在聆听" {
		t.Fatal("listening prompt collapsed")
	}
}
