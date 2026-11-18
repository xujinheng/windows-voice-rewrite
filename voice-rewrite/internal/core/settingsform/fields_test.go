package settingsform

import "testing"

func TestFieldGeometryKeepsWholeFocusBorderVisible(t *testing.T) {
	for row := 0; row < 3; row++ {
		frame := FieldFrame(row)
		edit := FieldEdit(row)
		label := FieldLabel(row)
		if frame.H != 34 || edit.H != 24 {
			t.Fatalf("row %d: want 34px outline and 24px native edit, got %d and %d", row, frame.H, edit.H)
		}
		if edit.X-frame.X < 12 || frame.X+frame.W-(edit.X+edit.W) < 12 {
			t.Errorf("row %d: native edit covers rounded outline: frame=%+v edit=%+v", row, frame, edit)
		}
		if 2*edit.Y+edit.H != 2*frame.Y+frame.H {
			t.Errorf("row %d: native edit not vertically centered: frame=%+v edit=%+v", row, frame, edit)
		}
		if 2*label.Y+label.H != 2*frame.Y+frame.H {
			t.Errorf("row %d: native label not vertically centered: frame=%+v label=%+v", row, frame, label)
		}
		if row > 0 {
			previous := FieldFrame(row - 1)
			if gap := frame.Y - (previous.Y + previous.H); gap < 12 || gap > 22 {
				t.Errorf("row gap %d: %d (want 12..22)", row, gap)
			}
		}
	}
}

func TestModelDiscoveryActionFitsModelRowWithoutOverlappingText(t *testing.T) {
	frame := FieldFrame(1)
	edit := ModelEdit(1)
	button := ModelDiscoverButton(1)
	if edit.X != FieldEdit(1).X || edit.Y != FieldEdit(1).Y ||
		button.Y < frame.Y || button.Y+button.H > frame.Y+frame.H ||
		edit.X+edit.W+12 > button.X || button.X+button.W > frame.X+frame.W {
		t.Fatalf("invalid model selector geometry: frame=%+v edit=%+v action=%+v", frame, edit, button)
	}
}

func TestModelDiscoveryRejectsCancelledOrWrongPageCompletion(t *testing.T) {
	if !ModelDiscoveryMayPresent(21, 21, 1, 1) {
		t.Fatal("current model response must be selectable")
	}
	for _, test := range [][4]int{{0, 0, 1, 1}, {19, 20, 0, 0}, {21, 21, 0, 1}, {21, 21, 1, 0}} {
		if ModelDiscoveryMayPresent(uint64(test[0]), uint64(test[1]), test[2], test[3]) {
			t.Fatalf("stale or off-page model response accepted: %v", test)
		}
	}
	if MaxModelMenuChoices < 1 || MaxModelMenuChoices > 100 {
		t.Fatalf("unsafe model menu size %d", MaxModelMenuChoices)
	}
}

func TestPaintedFramesFollowThePage(t *testing.T) {
	speech := PaintedFrames("speech")
	if len(speech) != 3 || speech[0] != FieldFrame(0) || speech[2] != FieldFrame(2) {
		t.Fatalf("speech %+v", speech)
	}
	input := PaintedFrames("input")
	second := ShortcutFrame()
	second.Y += RowStep
	if len(input) != 2 || input[0] != ShortcutFrame() || input[1] != second {
		t.Fatalf("input %+v", input)
	}
}

func TestSettingsChromeBoxesStayPut(t *testing.T) {
	if ProbeStatusBox() != (Rect{352, 324, 435, 48}) || ProbeButtonBox() != (Rect{696, 130, 92, 30}) {
		t.Fatal("probe boxes moved")
	}
	if WindowsInputNote() != (Rect{226, 374, 565, 32}) || SettingsHint() != (Rect{223, 457, 560, 22}) {
		t.Fatal("note or hint moved")
	}
	if FooterButton(AnchorSave) != (Rect{220, 418, 124, 34}) || FooterButton(AnchorAdvanced) != (Rect{358, 418, 112, 34}) || FooterButton(AnchorClose) != (Rect{710, 418, 94, 34}) {
		t.Fatal("footer moved")
	}
	if NavButton(0) != (Rect{12, 110, 170, 35}) || NavButton(2) != (Rect{12, 192, 170, 35}) {
		t.Fatal("nav moved")
	}
	frame := FieldFrame(1)
	if CaptureButton(1) != (Rect{593, frame.Y, 194, 34}) || MicrophoneList(1) != (Rect{frame.X + 5, frame.Y + 4, frame.W - 10, 190}) {
		t.Fatal("input row moved")
	}
}

func TestShortcutFieldMatchesMainFieldBaseline(t *testing.T) {
	frame, edit := ShortcutFrame(), ShortcutEdit()
	normal := FieldEdit(0)
	if edit.Y != normal.Y || edit.H != normal.H {
		t.Fatalf("shortcut vertical alignment differs from model fields: %+v vs %+v", edit, normal)
	}
	if edit.X-frame.X < 12 || frame.X+frame.W-(edit.X+edit.W) < 12 {
		t.Fatalf("shortcut edit clips focus border: frame=%+v edit=%+v", frame, edit)
	}
}
