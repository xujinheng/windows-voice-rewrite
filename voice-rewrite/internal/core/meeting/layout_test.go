package meeting

import (
	"testing"
	"time"
)

func TestMeetingAdjustablePaneLayout(t *testing.T) {
	for _, mode := range []struct {
		name         string
		captionsOnly bool
		expanded     bool
	}{
		{"complete", false, false},
		{"English focus", false, true},
		{"captions only", true, false},
	} {
		for _, space := range []int32{300, 440, 700} {
			heights := PaneHeights(space, DefaultPaneWeights, mode.captionsOnly, mode.expanded)
			visible := VisiblePanes(mode.captionsOnly, mode.expanded)
			var total int32
			for i, height := range heights {
				if !visible[i] && height != 0 {
					t.Fatalf("%s: hidden pane %d took %d px", mode.name, i, height)
				}
				if visible[i] && height < PaneMinimums[i] {
					t.Fatalf("%s: pane %d too short %d", mode.name, i, height)
				}
				total += height
			}
			if total != space {
				t.Fatalf("%s: %d px sum != %d available", mode.name, total, space)
			}
		}
	}
}

func TestMeetingDraggableDividersAreIndependentAndConstrained(t *testing.T) {
	for _, mode := range []struct {
		captionsOnly bool
		expanded     bool
		dividers     []int
	}{
		{false, false, []int{0, 1, 2, 3, 4}},
		{false, true, []int{0, 1, 2, 4}},
		{true, false, []int{0, 1}},
	} {
		for _, divider := range mode.dividers {
			weights := DefaultPaneWeights
			before := PaneHeights(600, weights, mode.captionsOnly, mode.expanded)
			a, b, ok := DividerPair(divider, mode.captionsOnly, mode.expanded)
			if !ok {
				t.Fatalf("divider %d unavailable", divider)
			}
			weights = DragPaneWeights(weights, before, mode.captionsOnly, mode.expanded, divider, 16)
			after := PaneHeights(600, weights, mode.captionsOnly, mode.expanded)
			if after[a] != before[a]+16 || after[b] != before[b]-16 {
				t.Fatalf("divider %d moved wrong panes: before %v after %v", divider, before, after)
			}
			for i := 0; i < PaneCount; i++ {
				if i != a && i != b && before[i] != after[i] {
					t.Fatalf("divider %d disturbed pane %d: %v -> %v", divider, i, before, after)
				}
			}
			weights = DragPaneWeights(weights, after, mode.captionsOnly, mode.expanded, divider, 10000)
			clamped := PaneHeights(600, weights, mode.captionsOnly, mode.expanded)
			if clamped[b] < PaneMinimums[b] {
				t.Fatalf("divider %d violated min height: %v", divider, clamped)
			}
		}
	}
	if _, _, ok := DividerPair(3, false, true); ok {
		t.Fatal("reply divider must hide when English is expanded")
	}
	if _, _, ok := DividerPair(2, true, false); ok {
		t.Fatal("assistant divider must hide in captions-only mode")
	}
}

func TestProfileLayoutKeepsTheStackedRows(t *testing.T) {
	boxes := ProfileLayout(400, 800, ProfileMetrics{
		Margin: 12, Gap: 8, Label: 25, Chrome: 246, MinEdit: 70,
		ModeHeight: 180, ModeStep: 34, NoteHeight: 48, ButtonSize: 32,
		SaveWidth: 130, CloseOffset: 140, CloseWidth: 96,
	})
	if len(boxes) != 11 || boxes[0] != (Box{12, 12, 400, 50}) || boxes[2].H != 236 || boxes[6].H != 180 {
		t.Fatalf("%+v", boxes)
	}
	if boxes[9] != (Box{12, 756, 130, 32}) || boxes[10] != (Box{152, 756, 96, 32}) {
		t.Fatalf("buttons %+v %+v", boxes[9], boxes[10])
	}
}

func TestMeetingChromeAndToolbarsStayPut(t *testing.T) {
	if NextFontSize(16, 2) != 18 || NextFontSize(27, 2) != 28 || NextFontSize(13, -2) != 12 {
		t.Fatal("font size")
	}
	if ChromeOverhead(false, false) != 216 || ChromeOverhead(true, false) != 48 || ChromeOverhead(false, true) != 88 {
		t.Fatalf("%d %d %d", ChromeOverhead(false, false), ChromeOverhead(true, false), ChromeOverhead(false, true))
	}
	indexes, widths := TopToolbar()
	if len(indexes) != 6 || indexes[0] != 7 || widths[0] != 96 || widths[5] != 92 {
		t.Fatal("top toolbar")
	}
	indexes, widths = ReplyToolbar()
	if len(indexes) != 3 || indexes[2] != 11 || widths[0] != 112 {
		t.Fatal("reply toolbar")
	}
}

func TestStreamingTextPaintsOnItsInterval(t *testing.T) {
	start := time.Unix(0, 0)
	if TextRefresh != 280*time.Millisecond || !TextDue(start, start.Add(TextRefresh), false) || TextDue(start, start.Add(TextRefresh-time.Millisecond), false) || !TextDue(start, start, true) {
		t.Fatal(TextRefresh)
	}
}
