package ux

import "testing"

func TestCardButtonsKeepCopyPrimaryOnlyWithoutApply(t *testing.T) {
	label, primary, ok := CardButtonFace(CardCopy, false)
	if label != CopyAction || !primary || !ok {
		t.Fatalf("copy primary %q %t", label, primary)
	}
	if _, primary, _ = CardButtonFace(CardCopy, true); primary {
		t.Fatal("copy stays secondary when apply exists")
	}
	label, primary, ok = CardButtonFace(CardApply, false)
	if label != ApplyAction || !primary || !ok {
		t.Fatal("apply")
	}
	label, primary, ok = CardButtonFace(CardClose, true)
	if label != CloseGlyph || label == "关闭" || primary || !ok {
		t.Fatalf("close %q %t", label, primary)
	}
	if _, _, ok = CardButtonFace(CardButton(9), true); ok {
		t.Fatal("unknown button")
	}
	if muted, editor := CardInk(CardNote); !muted || editor {
		t.Fatal("note")
	}
	if muted, editor := CardInk(CardEditor); muted || !editor {
		t.Fatal("editor")
	}
	if muted, editor := CardInk(CardChrome); muted || editor {
		t.Fatal("title")
	}
}
