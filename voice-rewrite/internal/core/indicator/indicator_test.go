package indicator

import (
	"testing"
	"time"
)

func TestCancelChoicePrefersThePreview(t *testing.T) {
	if !PhaseUpdateApplies(true, true, true) || PhaseUpdateApplies(false, true, true) || PhaseUpdateApplies(true, false, true) || PhaseUpdateApplies(true, true, false) {
		t.Fatal("phase")
	}
	if CancelChoice(true, false, true) != CancelPending || CancelChoice(false, false, true) != CancelIgnore || CancelChoice(false, true, true) != CancelDismiss || CancelChoice(false, true, false) != CancelWork {
		t.Fatal("cancel")
	}
}

func TestCompactGeometry(t *testing.T) {
	if Height != 50 {
		t.Fatalf("height %d", Height)
	}
	if WaveformX(8)+WaveBarWidth+8 >= ClockX {
		t.Fatal("wave/clock collision")
	}
	if ClockX+ClockWidth+6 >= RecordingWidth-CancelGlyphInset {
		t.Fatal("clock/close collision")
	}
	if WorkingTextX+148 >= WorkingWidth-WorkingTextRightGap {
		t.Fatal("work status too narrow")
	}
	for _, dpi := range []float64{1, 1.25, 1.5, 1.75, 2} {
		for _, width := range []int{RecordingWidth, WorkingWidth} {
			w, h := PhysicalSize(width, Height, dpi)
			if w < width || h < Height {
				t.Fatalf("dpi=%f size=%d,%d", dpi, w, h)
			}
			if w != int(float64(width)*dpi+0.5) {
				t.Fatalf("bad width at scale %v: %d", dpi, w)
			}
		}
		if CancelHit(RecordingWidth-CancelWidth-1, RecordingWidth) {
			t.Fatalf("cancel hit area crosses content at %v", dpi)
		}
		if !CancelHit(RecordingWidth-CancelWidth, RecordingWidth) {
			t.Fatalf("missing hit area at %v", dpi)
		}
	}
}

func TestOpacityOnInterruption(t *testing.T) {
	start := time.Unix(100, 0)
	if got := Opacity(start, start, start, Recording); got != 0 {
		t.Fatalf("first frame alpha=%d", got)
	}
	if got := Opacity(start, start, start.Add(IntroDuration), Recording); got != 255 {
		t.Fatalf("steady alpha=%d", got)
	}
	change := start.Add(time.Second)
	if got := Opacity(start, change, change, Processing); got != 225 {
		t.Fatalf("transition alpha=%d", got)
	}
	if got := Opacity(start, change, change.Add(PhaseEaseDuration), Processing); got != 255 {
		t.Fatalf("transition end=%d", got)
	}
	end := start.Add(2 * time.Second)
	if got := Opacity(start, end, end.Add(TerminalDuration), Success); got != 0 {
		t.Fatalf("terminal alpha=%d", got)
	}
	if got := Opacity(end.Add(time.Second), end.Add(time.Second), end.Add(time.Second+IntroDuration/2), Recording); got < 100 || got > 155 {
		t.Fatalf("new session alpha=%d", got)
	}
}

func TestMicrophoneLevelSmoothing(t *testing.T) {
	level := 0.0
	for i := 0; i < 20; i++ {
		level = SmoothedLevel(level, 1)
	}
	if level < 0.95 || level > 1 {
		t.Fatalf("slow ramp %f", level)
	}
	next := SmoothedLevel(level, 0)
	if next >= level || next < 0.5 {
		t.Fatalf("unstable decay %f", next)
	}
	if NormalizeMicrophoneLevel(0.1) != 0.5 || NormalizeMicrophoneLevel(0.3) != 1 || NormalizeMicrophoneLevel(-1) != 0 {
		t.Fatal("microphone gain")
	}
}

func TestLayoutLanes(t *testing.T) {
	if WaveformX(8)+WaveBarWidth >= ClockX {
		t.Fatal("waveform overlaps timer")
	}
	if ClockX+ClockWidth >= RecordingWidth-CancelGlyphInset-6 {
		t.Fatal("timer overlaps the close icon")
	}
	if !CancelHit(RecordingWidth-10, RecordingWidth) {
		t.Fatal("cancel icon cannot be clicked")
	}
	if CancelHit(WaveformX(5), RecordingWidth) {
		t.Fatal("waveform treated as cancel")
	}
	if WorkingTextX+130 >= WorkingWidth-WorkingTextRightGap {
		t.Fatal("working label too narrow")
	}
}

func TestDuration(t *testing.T) {
	started := time.Unix(0, 0)
	for _, tt := range []struct {
		elapsed time.Duration
		want    string
	}{
		{2 * time.Second, "00:02"}, {12 * time.Second, "00:12"}, {61 * time.Second, "01:01"}, {-5 * time.Second, "00:00"},
	} {
		got := DurationText(started, started.Add(tt.elapsed))
		if got != tt.want {
			t.Errorf("got %q want %q", got, tt.want)
		}
	}
}

func TestBarsRoundedAndBounded(t *testing.T) {
	for i := 0; i < 9; i++ {
		for _, l := range []float64{-1, 0, 0.1, 0.5, 1, 3} {
			h := WaveHeight(i, l, 120)
			if h < 4 || h > 18 {
				t.Fatalf("bar %d height %d", i, h)
			}
		}
	}
	if got := WaveHeight(4, -1, 0); got < 4 {
		t.Fatalf("height %d", got)
	}
	if got := WaveHeight(4, 2, 70); got > 18 {
		t.Fatalf("height %d", got)
	}
}

func TestSideButtonPreviewIsVisibleAtMouseDown(t *testing.T) {
	start := time.Unix(100, 0)
	if alpha := StatusOpacity(start, start, start, Recording, true); alpha != 255 {
		t.Fatalf("pending mouse status alpha=%d, want 255", alpha)
	}
	if alpha := StatusOpacity(start, start, start, Recording, false); alpha != 0 {
		t.Fatalf("ordinary status alpha=%d, want 0 for fade-in", alpha)
	}
}

func TestUnifiedStatusPhases(t *testing.T) {
	for _, tt := range []struct {
		phase      Phase
		mode, want string
	}{
		{Processing, "听写", "正在转写…"},
		{Processing, "改写", "识别指令…"},
		{Selection, "改写", "读取选区…"},
		{Rewriting, "改写", "AI 处理中…"},
		{Applying, "改写", "正在替换…"},
		{Success, "改写", "已发送粘贴指令"},
		{Success, "听写", "已发送粘贴指令"},
		{Answer, "改写", "已显示结果"},
		{Fallback, "听写", "结果已保留 · 可复制"},
		{Error, "改写", "操作未完成 · 查看日志"},
	} {
		if !Allowed(tt.phase) {
			t.Errorf("phase=%d should be allowed", tt.phase)
		}
		if got := Label(tt.phase, tt.mode); got != tt.want {
			t.Errorf("phase=%d mode=%s got=%q want=%q", tt.phase, tt.mode, got, tt.want)
		}
	}
	if Allowed(999) {
		t.Fatal("unknown phase accepted")
	}
	if !Terminal(Answer) || Terminal(Rewriting) {
		t.Fatal("terminal states invalid")
	}
	if Width(Recording) != RecordingWidth || Width(Rewriting) != WorkingWidth {
		t.Fatal("overlay geometry unexpectedly changed")
	}
	if Label(Recording, "听写") != "" {
		t.Fatal("recording phase uses the waveform, not a sentence")
	}
	if Accent(Error, "听写") != ErrorAccent || Accent(Recording, "改写") != RewriteAccent || Accent(Recording, "听写") != DictationAccent {
		t.Fatal("accent")
	}
	if TerminalMark(Answer) != AnswerMark || TerminalMark(Fallback) != FallbackMark || TerminalMark(Success) != SuccessMark {
		t.Fatal("terminal mark")
	}
	if Pill.Opaque() != 0xFF20242E || CheckGlyph.Opaque() != 0xFF19342A {
		t.Fatal("opaque color")
	}
	x, y, alpha := SpinnerDot(0, 0)
	if x != 38 || y != 25 || alpha != 65 {
		t.Fatalf("spinner origin %d,%d alpha %d", x, y, alpha)
	}
	x, y, alpha = SpinnerDot(1, 0)
	if x != 36 || y != 31 || alpha != 90 {
		t.Fatalf("spinner bead %d,%d alpha %d", x, y, alpha)
	}
}

func TestHostMotionParametersMatchSharedRenderer(t *testing.T) {
	spec := HostSpec()
	wave := spec["waveShape"].([]float64)
	if len(wave) != 7 || wave[0] != WaveMinHeight || wave[1] != WaveHeightRange ||
		wave[2] != WaveLevelExponent || wave[3] != WaveEnvelopeBase ||
		wave[4] != WaveEnvelopeRange || wave[5] != WaveBarPhase || wave[6] != WaveTickPhase {
		t.Fatalf("native waveform parameters drifted: %v", wave)
	}
	weights := spec["levelWeights"].([]float64)
	if len(weights) != 2 || weights[0] != LevelPastWeight || weights[1] != LevelCurrentWeight {
		t.Fatalf("native level smoother drifted: %v", weights)
	}
	spinner := spec["spinnerShape"].([]float64)
	if len(spinner) != 4 || spinner[0] != SpinnerTickPhase || spinner[1] != SpinnerBarPhase ||
		spinner[2] != SpinnerBaseAlpha || spinner[3] != SpinnerAlphaStep {
		t.Fatalf("native spinner parameters drifted: %v", spinner)
	}
}
