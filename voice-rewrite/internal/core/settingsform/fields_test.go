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
