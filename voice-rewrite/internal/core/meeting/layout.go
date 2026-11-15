// Package meeting contains platform-neutral reading and layout rules.
package meeting

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

func VisiblePanes(captionsOnly, expanded bool) [PaneCount]bool {
	return [PaneCount]bool{
		true, true, true, !captionsOnly && !expanded, !captionsOnly, !captionsOnly,
	}
}

// A divider always resizes its two neighboring visible surfaces; the reply
// surface is skipped when expanded English temporarily hides the reply tools.
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

