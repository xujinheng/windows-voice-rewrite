// Package indicator is the shared recording-pill model used by both desktop hosts.
// Hosts own the window, the microphone, and the pixels. Phases, geometry, labels,
// opacity, and waveform motion stay here so the two hosts do not drift.
package indicator

import (
	"fmt"
	"math"
	"time"
)

// Phase values are stable. Windows posts them as the status-window message parameter.
type Phase int

const (
	Recording  Phase = 1
	Processing Phase = 2
	Success    Phase = 3
	Error      Phase = 4
	Rewriting  Phase = 5
	Applying   Phase = 6
	Answer     Phase = 7
	Selection  Phase = 8
	Fallback   Phase = 9
)

const (
	Height              = 50
	RecordingWidth      = 232
	WorkingWidth        = 252
	CancelWidth         = 36
	WaveBarWidth        = 3
	ClockX              = 145
	ClockWidth          = 50
	CancelGlyphInset    = 19
	IconCenterY         = 25
	WorkingTextX        = 53
	WorkingTextRightGap = 38
	IntroDuration       = 170 * time.Millisecond
	PhaseEaseDuration   = 160 * time.Millisecond
	TerminalDuration    = 1750 * time.Millisecond
	OutroDuration       = 240 * time.Millisecond
	MicrophoneLevelGain = 5.0
	SpinnerDots         = 8
	SpinnerCenterX      = 30
	SpinnerRadius       = 8
	SpinnerDotSize      = 4
	WaveMinHeight       = 4
	WaveHeightRange     = 14
	WaveLevelExponent   = 0.7
	WaveEnvelopeBase    = 0.44
	WaveEnvelopeRange   = 0.56
	WaveBarPhase        = 0.75
	WaveTickPhase       = 0.24
	LevelPastWeight     = 0.72
	LevelCurrentWeight  = 0.28
	SpinnerTickPhase    = 0.2
	SpinnerBarPhase     = math.Pi / 4
	SpinnerBaseAlpha    = 65
	SpinnerAlphaStep    = 25
)

func CancelHit(x, width int) bool { return x >= width-CancelWidth && x < width }

// PhysicalSize keeps layout in logical 96-DPI units and scales only at the window edge.
func PhysicalSize(logicalWidth, logicalHeight int, scale float64) (int, int) {
	if scale <= 0 {
		scale = 1
	}
	return int(math.Round(float64(logicalWidth) * scale)), int(math.Round(float64(logicalHeight) * scale))
}

func DurationText(started, now time.Time) string {
	sec := int(now.Sub(started).Seconds())
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

func WaveformX(i int) int { return 52 + i*8 }

// SpinnerDot is one bead of the progress wheel. The point is the bead center.
func SpinnerDot(index int, tick uint32) (x, y int, alpha uint8) {
	angle := float64(tick)*SpinnerTickPhase + float64(index)*SpinnerBarPhase
	x = int(math.Round(float64(SpinnerCenterX) + float64(SpinnerRadius)*math.Cos(angle)))
	y = int(math.Round(float64(IconCenterY) + float64(SpinnerRadius)*math.Sin(angle)))
	value := SpinnerBaseAlpha + index*SpinnerAlphaStep
	if value > 255 {
		value = 255
	}
	return x, y, uint8(value)
}

func WaveHeight(i int, level float64, tick uint32) int {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	envelope := WaveEnvelopeBase + WaveEnvelopeRange*math.Abs(math.Sin(float64(i)*WaveBarPhase+float64(tick)*WaveTickPhase))
	h := int(math.Round(WaveMinHeight + WaveHeightRange*math.Pow(level, WaveLevelExponent)*envelope))
	if h < WaveMinHeight {
		h = WaveMinHeight
	}
	if h > WaveMinHeight+WaveHeightRange {
		h = WaveMinHeight+WaveHeightRange
	}
	return h
}

// StatusOpacity shows a side-button preview on the first frame. A confirmed
// recording keeps the short fade-in.
func StatusOpacity(started, changed, now time.Time, phase Phase, pending bool) uint8 {
	if pending {
		return 255
	}
	return Opacity(started, changed, now, phase)
}

func Opacity(started, changed, now time.Time, phase Phase) uint8 {
	if now.Before(started) {
		return 0
	}
	age := now.Sub(started)
	if age < IntroDuration {
		return uint8(math.Round(255 * float64(age) / float64(IntroDuration)))
	}
	if Terminal(phase) {
		age = now.Sub(changed)
		if age >= TerminalDuration {
			return 0
		}
		if age > TerminalDuration-OutroDuration {
			left := TerminalDuration - age
			return uint8(math.Round(255 * float64(left) / float64(OutroDuration)))
		}
	}
	if elapsed := now.Sub(changed); elapsed >= 0 && elapsed < PhaseEaseDuration {
		return uint8(225 + math.Round(30*float64(elapsed)/float64(PhaseEaseDuration)))
	}
	return 255
}

func SmoothedLevel(previous, incoming float64) float64 {
	if incoming < 0 {
		incoming = 0
	}
	if incoming > 1 {
		incoming = 1
	}
	if previous < 0 {
		previous = 0
	}
	if previous > 1 {
		previous = 1
	}
	return previous*LevelPastWeight + incoming*LevelCurrentWeight
}

// NormalizeMicrophoneLevel applies the same gain the Windows capture meter uses
// before the pill smoother. Hosts that already measured raw RMS pass it here.
func NormalizeMicrophoneLevel(raw float64) float64 {
	if raw < 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 0
	}
	if raw*MicrophoneLevelGain > 1 {
		return 1
	}
	return raw * MicrophoneLevelGain
}

func Width(phase Phase) int {
	if phase == Recording {
		return RecordingWidth
	}
	return WorkingWidth
}

func Allowed(phase Phase) bool {
	switch phase {
	case Recording, Processing, Success, Error, Rewriting, Applying, Answer, Selection, Fallback:
		return true
	default:
		return false
	}
}

const (
	CancelIgnore  = 0
	CancelPending = 1
	CancelDismiss = 2
	CancelWork    = 3
)

// HostSpec exposes the Windows recording pill's presentation model once to
// native hosts. Swift does not need a per-frame bridge or a second palette.
func HostSpec() map[string]any {
	rgb := func(c RGB) []int { return []int{int(c.R), int(c.G), int(c.B)} }
	return map[string]any{
		"recordingWidth": RecordingWidth, "workingWidth": WorkingWidth, "height": Height,
		"cancelWidth": CancelWidth, "cancelInset": CancelGlyphInset,
		"clockX": ClockX, "clockWidth": ClockWidth, "iconY": IconCenterY,
		"waveStartX": WaveformX(0), "waveStep": WaveformX(1) - WaveformX(0),
		"waveBarWidth": WaveBarWidth, "waveBars": 9,
		"textX": WorkingTextX, "textRightGap": WorkingTextRightGap,
		"spinnerDots": SpinnerDots, "spinnerX": SpinnerCenterX,
		"spinnerRadius": SpinnerRadius, "spinnerDotSize": SpinnerDotSize,
		"terminalMS": int(TerminalDuration / time.Millisecond),
		"levelGain":  MicrophoneLevelGain,
		"waveShape": []float64{WaveMinHeight, WaveHeightRange, WaveLevelExponent, WaveEnvelopeBase, WaveEnvelopeRange, WaveBarPhase, WaveTickPhase},
		"levelWeights": []float64{LevelPastWeight, LevelCurrentWeight},
		"spinnerShape": []float64{SpinnerTickPhase, SpinnerBarPhase, SpinnerBaseAlpha, SpinnerAlphaStep},
		"pillColor":  rgb(Pill), "clockColor": rgb(ClockText),
		"labelColor": rgb(LabelText), "dictationAccent": rgb(DictationAccent),
		"rewriteAccent": rgb(RewriteAccent), "errorAccent": rgb(ErrorAccent),
		"sentMark": rgb(SuccessMark), "fallbackMark": rgb(FallbackMark),
		"closeColor": rgb(CloseGlyph), "closeHover": rgb(CloseHover),
		"attentionGlyph":      rgb(AttentionGlyph),
		"processingDictation": Label(Processing, "听写"),
		"processingRewrite":   Label(Processing, "改写"),
		"sentLabel":           Label(Success, "听写"), "errorLabel": Label(Error, "听写"),
		"fallbackLabel": "结果未写入",
	}
}

// PhaseUpdateApplies reports that a posted phase still belongs to the visible pill.
func PhaseUpdateApplies(epochValid, hasPill, ownerMatches bool) bool {
	return epochValid && hasPill && ownerMatches
}

// CancelChoice is the pill cancel. A pending preview wins, a stale message is ignored,
// and a finished phase only closes the pill.
func CancelChoice(pending, current, terminal bool) int {
	if pending {
		return CancelPending
	}
	if !current {
		return CancelIgnore
	}
	if terminal {
		return CancelDismiss
	}
	return CancelWork
}

func Terminal(phase Phase) bool {
	return phase == Success || phase == Error || phase == Answer || phase == Fallback
}

func Label(phase Phase, mode string) string {
	switch phase {
	case Processing:
		if mode == "改写" {
			return "识别指令…"
		}
		return "正在转写…"
	case Selection:
		return "读取选区…"
	case Rewriting:
		return "AI 处理中…"
	case Applying:
		return "正在替换…"
	case Success:
		return "已发送粘贴指令"
	case Answer:
		return "已显示结果"
	case Error:
		return "操作未完成 · 查看日志"
	case Fallback:
		return "结果已保留 · 可复制"
	default:
		return ""
	}
}
