package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"voicerewrite/internal/contract"
)

func TestWindowsRecordingDurationValidation(t *testing.T) {
	for _, good := range []string{"1", "60", "120", "180", " 180 "} {
		v, err := uxRecordingSeconds(good)
		if err != nil || v < 1 || v > 180 {
			t.Fatalf("valid duration %q: %d %v", good, v, err)
		}
	}
	for _, bad := range []string{"", "0", "181", "-1", "abc", "60.5"} {
		if _, err := uxRecordingSeconds(bad); err == nil {
			t.Fatalf("accepted invalid duration %q", bad)
		}
	}
}

func TestSettingsBuilderCoversCatalog(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "settings_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	placed := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, item := range clause.List {
			lit, ok := item.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING {
				text, err := strconv.Unquote(lit.Value)
				if err == nil {
					placed[text] = true
				}
			}
		}
		return true
	})
	for _, page := range append(contract.Pages(), contract.Page{ID: "save"}) {
		for _, control := range contract.PageControls(page.ID, contract.Windows) {
			if !placed[control.ID] {
				t.Fatalf("Windows settings builder has no placement for %s", control.ID)
			}
		}
	}
}
