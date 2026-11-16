// Package meeting contains platform-neutral reading and layout rules.
package meeting

import (
	"strings"
	"time"
)

// The six reading surfaces share the space left after fixed controls.
// Values are logical 96-DPI pixels, independent of the physical monitor DPI.
const (
	PaneChinese = iota
	PaneEnglish
	PaneHistory
	PaneReply
	PaneExpression
	PaneSource
	PaneCount
)

type PaneWeights [PaneCount]int32

var DefaultPaneWeights = PaneWeights{85, 50, 85, 65, 230, 40}
var PaneMinimums = [PaneCount]int32{46, 32, 34, 36, 62, 28}

// TextRefresh is how long a streaming meeting panel keeps its previous paint.
const TextRefresh = 280 * time.Millisecond

func TextDue(last, now time.Time, immediate bool) bool {
	return immediate || now.Sub(last) >= TextRefresh
}

func ValidPaneWeights(value PaneWeights) PaneWeights {
	var sum int32
	for _, weight := range value {
		if weight < 0 || weight > 10000 {
			return DefaultPaneWeights
		}
		sum += weight
	}
	if sum == 0 {
		return DefaultPaneWeights
	}
	return value
}

// KeepFinishedEnglish reports whether a completed English line should stay
// visible while a later request is running or has failed.
func KeepFinishedEnglish(complete bool, english string) bool {
	return complete && strings.TrimSpace(english) != ""
}

type Box struct{ X, Y, W, H int32 }

type ProfileMetrics struct {
	Margin, Gap, Label, Chrome, MinEdit          int32
	ModeHeight, ModeStep, NoteHeight, ButtonSize int32
	SaveWidth, CloseOffset, CloseWidth           int32
}

// ProfileLayout stacks the meeting profile controls in logical or physical pixels.
// The caller scales each metric before calling, because each edge is rounded on its own.
func ProfileLayout(width, height int32, m ProfileMetrics) []Box {
	y := m.Margin
	titleH := m.Label * 2
	boxes := []Box{{m.Margin, y, width, titleH}}
	y += titleH + m.Gap
	available := height - y - m.Margin - m.Chrome
	editH := m.MinEdit
	if half := available / 2; half > editH {
		editH = half
	}
	add := func(x, w, h int32) {
		boxes = append(boxes, Box{x, y, w, h})
	}
	add(m.Margin, width, m.Label)
	y += m.Label
	add(m.Margin, width, editH)
	y += editH + m.Gap
	add(m.Margin, width, m.Label)
	y += m.Label
	add(m.Margin, width, editH)
	y += editH + m.Gap
	add(m.Margin, width, m.Label)
	y += m.Label
	add(m.Margin, width, m.ModeHeight)
	y += m.ModeStep
	add(m.Margin, width, m.NoteHeight)
	y += m.NoteHeight + m.Gap
	add(m.Margin, width, m.Label)
	y += m.Label + m.Gap
	add(m.Margin, m.SaveWidth, m.ButtonSize)
	add(m.Margin+m.CloseOffset, m.CloseWidth, m.ButtonSize)
	return boxes
}

func NextFontSize(current, delta int32) int32 {
	return max(12, min(28, current+delta))
}

func ChromeOverhead(captionsOnly, expanded bool) int32 {
	if captionsOnly {
		return 2*8 + 24 + 8
	}
	if expanded {
		return 4*8 + 24 + 8 + 24
	}
	return 5*8 + 24 + 8 + 120 + 24
}

func TopToolbar() ([]int, []int32) {
	return []int{7, 1, 0, 2, 3, 8}, []int32{96, 92, 90, 36, 36, 92}
}

func ReplyToolbar() ([]int, []int32) {
	return []int{9, 10, 11}, []int32{112, 112, 112}
}

// ReadingLayout is the meeting HUD after the profile page is hidden.
// scale matches the host pixel round, int32(v*scale+0.5), applied to each edge alone.
type ReadingLayout struct {
	PaneHeights                                                     [PaneCount]int32
	Status, Speaker, Live                                           Box
	Toolbar                                                         []Box
	Chinese, English                                                Box
	HistoryLabel, HistoryButton, History                            Box
	Reply                                                           bool
	ReplyLabel, Request, Accept, CopyReply                          Box
	ReplyButtons                                                    []Box
	Responses, Instruction, Refine                                  Box
	Expression                                                      bool
	ExpressionLabel, Expand, CopyExpression, ExpressionBody, Source Box
	SplitterY                                                       [5]int32
	SplitterOn                                                      [5]bool
}

func LayoutReading(clientW, clientH int32, scale float64, weights PaneWeights, captionsOnly, expanded bool) ReadingLayout {
	px := func(v int32) int32 { return int32(float64(v)*scale + 0.5) }
	var lay ReadingLayout
	margin, gap, label, button := px(16), px(8), px(24), px(32)
	width := max(px(200), clientW-2*margin)
	y := px(8)
	lay.Status = Box{margin, y, width, px(40)}
	y += px(40) + px(6)
	_, widths := TopToolbar()
	x := margin
	for _, item := range widths {
		lay.Toolbar = append(lay.Toolbar, Box{x, y, px(item), button})
		x += px(item) + gap
	}
	y += button + gap
	lay.Speaker = Box{margin, y, width - px(264), label}
	lay.Live = Box{margin + width - px(264), y, px(264), label}
	y += label + px(3)
	footer := clientH - margin
	space := int32(float64(footer-y)/scale) - ChromeOverhead(captionsOnly, expanded)
	lay.PaneHeights = PaneHeights(space, weights, captionsOnly, expanded)
	mark := func(index int) {
		lay.SplitterY[index] = y + px(4)
		lay.SplitterOn[index] = true
		y += px(8)
	}
	lay.Chinese = Box{margin, y, width, px(lay.PaneHeights[PaneChinese])}
	y += lay.Chinese.H
	mark(0)
	lay.English = Box{margin, y, width, px(lay.PaneHeights[PaneEnglish])}
	y += lay.English.H
	mark(1)
	lay.HistoryLabel = Box{margin, y, width - px(114), label}
	lay.HistoryButton = Box{margin + width - px(110), y - px(3), px(110), button}
	y += label + gap
	lay.History = Box{margin, y, width, px(lay.PaneHeights[PaneHistory])}
	y += lay.History.H
	if captionsOnly {
		return lay
	}
	mark(2)
	lay.Expression = true
	if !expanded {
		lay.Reply = true
		lay.ReplyLabel = Box{margin, y, width - px(316), button}
		lay.Request = Box{margin + width - px(310), y, px(92), button}
		lay.Accept = Box{margin + width - px(210), y, px(108), button}
		lay.CopyReply = Box{margin + width - px(94), y, px(94), button}
		y += button + gap
		x = margin
		_, widths = ReplyToolbar()
		for _, item := range widths {
			lay.ReplyButtons = append(lay.ReplyButtons, Box{x, y, px(item), button})
			x += px(item) + gap
		}
		y += button + gap
		lay.Responses = Box{margin, y, width, px(lay.PaneHeights[PaneReply])}
		y += lay.Responses.H + gap
		lay.Instruction = Box{margin, y, width - px(112) - gap, button}
		lay.Refine = Box{margin + width - px(112), y, px(112), button}
		y += button
		mark(3)
	}
	lay.ExpressionLabel = Box{margin, y, width - px(240), label}
	lay.Expand = Box{margin + width - px(226), y - px(4), px(110), button}
	lay.CopyExpression = Box{margin + width - px(112), y - px(4), px(112), button}
	y += label
	lay.ExpressionBody = Box{margin, y, width, px(lay.PaneHeights[PaneExpression])}
	y += lay.ExpressionBody.H
	mark(4)
	lay.Source = Box{margin, y, width, px(lay.PaneHeights[PaneSource])}
	return lay
}

func VisiblePanes(captionsOnly, expanded bool) [PaneCount]bool {
	return [PaneCount]bool{
		true, true, true, !captionsOnly && !expanded, !captionsOnly, !captionsOnly,
	}
}

// A divider always resizes its two neighboring visible surfaces; the reply
// surface is skipped when expanded English temporarily hides the reply tools.
// SplitterHit is the divider under a client Y, or -1 when the pointer misses every band.
// band is half the hit thickness. The top edge is included and the bottom edge is not.
func SplitterHit(y int32, splitterY [5]int32, visible [5]bool, band int32, profileOpen bool) int {
	if profileOpen {
		return -1
	}
	for index, shown := range visible {
		if shown && y >= splitterY[index]-band && y < splitterY[index]+band {
			return index
		}
	}
	return -1
}

func DividerPair(index int, captionsOnly, expanded bool) (int, int, bool) {
	switch index {
	case 0:
		return PaneChinese, PaneEnglish, true
	case 1:
		return PaneEnglish, PaneHistory, true
	case 2:
		if captionsOnly {
			return 0, 0, false
		}
		if expanded {
			return PaneHistory, PaneExpression, true
		}
		return PaneHistory, PaneReply, true
	case 3:
		return PaneReply, PaneExpression, !captionsOnly && !expanded
	case 4:
		return PaneExpression, PaneSource, !captionsOnly
	}
	return 0, 0, false
}

// Give each visible surface a minimum height, then distribute the remaining
// space by saved weights. Hidden panes retain their sizes for later reopening.
func PaneHeights(space int32, weights PaneWeights, captionsOnly, expanded bool) [PaneCount]int32 {
	visible := VisiblePanes(captionsOnly, expanded)
	weights = ValidPaneWeights(weights)
	var heights [PaneCount]int32
	var minimum, totalWeight int32
	last := -1
	for i, shown := range visible {
		if shown {
			heights[i] = PaneMinimums[i]
			minimum += PaneMinimums[i]
			totalWeight += weights[i]
			last = i
		}
	}
	extra := max(int32(0), space-minimum)
	if totalWeight == 0 {
		totalWeight = 1
	}
	remaining := extra
	for i, shown := range visible {
		if shown && i != last {
			add := int32(int64(extra) * int64(weights[i]) / int64(totalWeight))
			heights[i] += add
			remaining -= add
		}
	}
	if last >= 0 {
		heights[last] += remaining
	}
	return heights
}

// Encode a mouse drag as new distributable heights. The next layout preserves
// the dragged partition while a later window resize scales it proportionally.
func DragPaneWeights(weights PaneWeights, heights [PaneCount]int32, captionsOnly, expanded bool, divider int, delta int32) PaneWeights {
	a, b, ok := DividerPair(divider, captionsOnly, expanded)
	if !ok {
		return weights
	}
	delta = max(PaneMinimums[a]-heights[a], min(heights[b]-PaneMinimums[b], delta))
	if delta == 0 {
		return weights
	}
	heights[a] += delta
	heights[b] -= delta
	for i, shown := range VisiblePanes(captionsOnly, expanded) {
		if shown {
			weights[i] = heights[i] - PaneMinimums[i]
		}
	}
	return weights
}
