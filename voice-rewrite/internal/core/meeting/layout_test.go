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

func TestReadingLayoutMatchesTheHudRows(t *testing.T) {
	full := LayoutReading(1000, 900, 1, DefaultPaneWeights, false, false)
	if full.Status != (Box{16, 8, 968, 40}) || len(full.Toolbar) != 6 || full.Toolbar[0] != (Box{16, 54, 96, 32}) {
		t.Fatalf("status %+v toolbar %+v", full.Status, full.Toolbar)
	}
	if full.Speaker != (Box{16, 94, 704, 24}) || full.Live != (Box{720, 94, 264, 24}) || full.Chinese.Y != 121 {
		t.Fatalf("speaker %+v live %+v chinese %+v", full.Speaker, full.Live, full.Chinese)
	}
	if !full.Reply || !full.Expression || !full.SplitterOn[0] || !full.SplitterOn[3] || !full.SplitterOn[4] {
		t.Fatal("full reading")
	}
	captions := LayoutReading(1000, 900, 1, DefaultPaneWeights, true, false)
	if captions.Reply || captions.Expression || captions.SplitterOn[2] {
		t.Fatal("captions only")
	}
	expanded := LayoutReading(1000, 900, 1, DefaultPaneWeights, false, true)
	if expanded.Reply || !expanded.Expression || !expanded.SplitterOn[2] || expanded.SplitterOn[3] || !expanded.SplitterOn[4] {
		t.Fatal("expanded english")
	}
	scaled := LayoutReading(1000, 900, 2, DefaultPaneWeights, false, false)
	if scaled.Status.X != 32 || scaled.Status.Y != 16 || scaled.Status.H != 80 || scaled.Chinese.Y != 242 || scaled.Toolbar[0].W != 192 {
		t.Fatalf("scaled %+v chinese %d toolbar %d", scaled.Status, scaled.Chinese.Y, scaled.Toolbar[0].W)
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

func TestTranscriptAndExpressionSteps(t *testing.T) {
	start := time.Unix(0, 0)
	newTurn, paint := TranscriptStep(2, 1, 1, 1, true, start, start)
	if !newTurn || !paint {
		t.Fatal("new turn paints immediately")
	}
	_, paint = TranscriptStep(2, 2, 1, 1, true, start, start.Add(time.Millisecond))
	if paint {
		t.Fatal("live transcript waits for the interval")
	}
	copy, captions, expanded, fresh, paint := ExpressionStep(true, "hello", "", true, false, start, start)
	if copy != "hello" || captions || !expanded || !fresh || !paint {
		t.Fatalf("fresh expression %q %v %v %v %v", copy, captions, expanded, fresh, paint)
	}
	copy, _, _, fresh, paint = ExpressionStep(false, "hello", "old", false, false, start, start)
	if copy != "" || fresh || paint {
		t.Fatalf("incomplete waits %q %v %v", copy, fresh, paint)
	}
	_, _, _, _, paint = ExpressionStep(false, "hello", "old", false, false, start, start.Add(TextRefresh))
	if !paint {
		t.Fatal("incomplete paints on the interval")
	}
}

func TestSplitterHitUsesTheOpenBand(t *testing.T) {
	var y [5]int32
	var shown [5]bool
	y[1], shown[1] = 100, true
	if MinClientWidth != 600 || MinClientHeight != 720 || LogicalDrag(110, 100, 2) != 5 || LogicalDrag(101, 100, 2) != 0 || LogicalDrag(90, 100, 1) != -10 {
		t.Fatal("meeting drag")
	}
	if LabelFont() != (FontFace{14, 600}) {
		t.Fatal("label font")
	}
	if MeetingTextFace(MeetingExpression) != FaceEnglish || MeetingTextFace(MeetingEnglish) != FaceEnglish || MeetingTextFace(MeetingChinese) != FaceChinese || MeetingTextFace(MeetingCaptions) != FaceBody || MeetingTextFace(MeetingProfileTerms) != FaceBody {
		t.Fatal("text face")
	}
	if ReplaceReadingFonts(1, 1, 0) || ReplaceReadingFonts(0, 1, 1) || ReplaceReadingFonts(1, 0, 1) || !ReplaceReadingFonts(1, 2, 3) {
		t.Fatal("replace fonts")
	}
	body, english, chinese := ReadingFonts(16)
	if body != (FontFace{16, 400}) || english != (FontFace{17, 450}) || chinese != (FontFace{22, 600}) {
		t.Fatal("reading fonts")
	}
	rails := SplitterRails(400, 100, 22, 1)
	if rails[0] != (Box{378, 98, 44, 1}) || rails[1] != (Box{378, 102, 44, 1}) {
		t.Fatalf("rails %+v", rails)
	}
	if SplitterHit(96, y, shown, 4, true) != -1 || SplitterHit(96, y, shown, 4, false) != 1 || SplitterHit(104, y, shown, 4, false) != -1 || SplitterHit(80, y, shown, 4, false) != -1 {
		t.Fatal("splitter band")
	}
	if TranscriptAtBottom(10, 0, 1) || !TranscriptAtBottom(10, 8, 1) || !TranscriptAtBottom(4, 0, 3) {
		t.Fatal("transcript bottom")
	}
}

func TestStreamingTextPaintsOnItsInterval(t *testing.T) {
	start := time.Unix(0, 0)
	if TextRefresh != 280*time.Millisecond || !TextDue(start, start.Add(TextRefresh), false) || TextDue(start, start.Add(TextRefresh-time.Millisecond), false) || !TextDue(start, start, true) {
		t.Fatal(TextRefresh)
	}
}
