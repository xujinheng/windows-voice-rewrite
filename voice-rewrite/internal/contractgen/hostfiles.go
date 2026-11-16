package contractgen

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"voicerewrite/internal/contract"
)

func emitDesktop(core string) (string, error) {
	geometry, err := parseFile(filepath.Join(core, "desktop", "geometry.go"))
	if err != nil {
		return "", err
	}
	layout, err := parseFile(filepath.Join(core, "popup", "layout.go"))
	if err != nil {
		return "", err
	}
	clampConv := baseConv()
	clampConv.dropRecv = "work"
	clampConv.rename = map[string]string{"w": "width", "h": "height"}
	clamp, err := emitMethod(clampConv, geometry, "Clamp", "func clamp(x: Int32, y: Int32, width: Int32, height: Int32) -> (Int32, Int32)")
	if err != nil {
		return "", err
	}
	containsConv := baseConv()
	containsConv.dropRecv = "r"
	contains, err := emitMethod(containsConv, layout, "Contains", "func contains(x: Int32, y: Int32) -> Bool")
	if err != nil {
		return "", err
	}
	click, err := emitStatic(baseConv(), layout, "ClickDismisses", "Bool")
	if err != nil {
		return "", err
	}
	height, err := emitCardHeight(layout)
	if err != nil {
		return "", err
	}
	chrome, err := emitChrome(layout)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/desktop and voice-rewrite/internal/core/popup"))
	b.WriteString("import Foundation\n\nstruct DesktopWorkArea {\n")
	b.WriteString("    var left, top, right, bottom: Int32\n\n")
	b.WriteString(clamp)
	b.WriteString("}\n\nstruct ResultRect {\n")
	b.WriteString("    var x, y, w, h: Int32\n")
	b.WriteString("    var hit: ResultHitRect { ResultHitRect(left: x, top: y, right: x + w, bottom: y + h) }\n}\n\n")
	b.WriteString(chrome)
	b.WriteString("\nenum ResultCardMetrics {\n")
	b.WriteString(height)
	b.WriteString("\n")
	b.WriteString(click)
	b.WriteString("}\n\nstruct ResultHitRect {\n    var left, top, right, bottom: Int32\n")
	b.WriteString(contains)
	b.WriteString("}\n")
	return b.String(), nil
}

func emitMethod(c *conv, file *ast.File, name, signature string) (string, error) {
	fn := c.fn(name, file)
	if fn == nil {
		return "", fmt.Errorf("missing %s", name)
	}
	c.prepare(fn)
	body, err := c.block(fn.Body, 2)
	if err != nil {
		return "", err
	}
	return "    " + signature + " {\n" + c.paramCopies(fn, 2) + body + "    }\n", nil
}

func emitCardHeight(file *ast.File) (string, error) {
	fn := findFunc(file, "SizeForText")
	if fn == nil {
		return "", fmt.Errorf("SizeForText")
	}
	var width, height string
	var branches []string
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 2 {
			return true
		}
		if lit, ok := as.Rhs[0].(*ast.BasicLit); ok {
			width = lit.Value
		}
		if lit, ok := as.Rhs[1].(*ast.BasicLit); ok {
			height = lit.Value
		}
		return true
	})
	ast.Inspect(fn, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		for _, clause := range sw.Body.List {
			cc := clause.(*ast.CaseClause)
			if len(cc.List) != 1 || len(cc.Body) != 1 {
				continue
			}
			cond, _, err := baseConv().expr(cc.List[0])
			if err != nil {
				return true
			}
			as, ok := cc.Body[0].(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 {
				continue
			}
			lit, ok := as.Rhs[0].(*ast.BasicLit)
			if !ok {
				continue
			}
			branches = append(branches, "        if "+strings.ReplaceAll(cond, "runes", "unicodeScalars")+" { return "+lit.Value+" }")
		}
		return false
	})
	if width == "" || height == "" || len(branches) != 3 {
		return "", fmt.Errorf("card size %s %s %d", width, height, len(branches))
	}
	var b strings.Builder
	b.WriteString("    static let width: CGFloat = " + width + "\n\n")
	b.WriteString("    static func height(unicodeScalars: Int) -> CGFloat {\n")
	for _, branch := range branches {
		b.WriteString(branch + "\n")
	}
	b.WriteString("        return " + height + "\n    }\n")
	return b.String(), nil
}

func emitChrome(file *ast.File) (string, error) {
	var b strings.Builder
	b.WriteString("enum ResultCardChrome {\n")
	for _, item := range []struct{ goName, swift string }{
		{"Editor", "editor"},
		{"CloseButton", "closeButton"},
		{"CopyButton", "copyButton"},
		{"Note", "note"},
	} {
		fn := findFunc(file, item.goName)
		if fn == nil {
			return "", fmt.Errorf("missing %s", item.goName)
		}
		ret, ok := fn.Body.List[len(fn.Body.List)-1].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return "", fmt.Errorf("%s return", item.goName)
		}
		lit, ok := ret.Results[0].(*ast.CompositeLit)
		if !ok || len(lit.Elts) != 4 {
			return "", fmt.Errorf("%s rect", item.goName)
		}
		parts := make([]string, 4)
		for i, elt := range lit.Elts {
			text, err := frameExpr(elt)
			if err != nil {
				return "", err
			}
			parts[i] = text
		}
		params := "width: Int32"
		if usesIdent(fn, "clientH") {
			params += ", height: Int32"
		}
		b.WriteString("    static func " + item.swift + "(" + params + ") -> ResultRect {\n")
		b.WriteString("        ResultRect(x: " + parts[0] + ", y: " + parts[1] + ", w: " + parts[2] + ", h: " + parts[3] + ")\n")
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func usesIdent(fn *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && id.Name == name {
			found = true
		}
		return true
	})
	return found
}

func frameExpr(e ast.Expr) (string, error) {
	switch n := e.(type) {
	case *ast.BasicLit:
		return n.Value, nil
	case *ast.Ident:
		switch n.Name {
		case "clientW":
			return "width", nil
		case "clientH":
			return "height", nil
		default:
			return "", fmt.Errorf("frame ident %s", n.Name)
		}
	case *ast.CallExpr:
		id, ok := n.Fun.(*ast.Ident)
		if !ok || id.Name != "scaled" {
			return "", fmt.Errorf("frame call")
		}
		return frameExpr(n.Args[0])
	case *ast.BinaryExpr:
		left, err := frameExpr(n.X)
		if err != nil {
			return "", err
		}
		right, err := frameExpr(n.Y)
		if err != nil {
			return "", err
		}
		return "(" + left + " " + n.Op.String() + " " + right + ")", nil
	case *ast.ParenExpr:
		return frameExpr(n.X)
	default:
		return "", fmt.Errorf("frame %T", e)
	}
}

func emitLimits(moduleRoot string) (string, error) {
	config, err := parseFile(filepath.Join(moduleRoot, "internal", "core", "config", "config.go"))
	if err != nil {
		return "", err
	}
	want := []struct{ goName, swift string }{
		{"RecordSecondsMin", "recordSecondsMin"},
		{"RecordSecondsMax", "recordSecondsMax"},
		{"DefaultRecordSeconds", "defaultRecordSeconds"},
		{"MaxEndpointBytes", "maxEndpointBytes"},
		{"MaxModelIDBytes", "maxModelIDBytes"},
		{"MaxCredentialBytes", "maxCredentialBytes"},
	}
	values := map[string]string{}
	for _, spec := range constSpecs(config) {
		values[spec.name] = spec.value
	}
	validate := findFunc(config, "ValidateBaseURL")
	if validate == nil {
		return "", fmt.Errorf("ValidateBaseURL")
	}
	schemes := schemeLits(validate)
	if len(schemes) == 0 {
		return "", fmt.Errorf("no url schemes")
	}
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/config"))
	b.WriteString("import Foundation\n\npublic enum ContractLimits {\n")
	for _, item := range want {
		value, ok := values[item.goName]
		if !ok {
			return "", fmt.Errorf("missing %s", item.goName)
		}
		b.WriteString("    public static let " + item.swift + " = " + value + "\n")
	}
	defaults, err := defaultStrings(config)
	if err != nil {
		return "", err
	}
	for _, item := range []struct{ goName, swift string }{
		{"ASRBaseURL", "defaultASRBaseURL"},
		{"LLMBaseURL", "defaultLLMBaseURL"},
		{"ASRModel", "defaultASRModel"},
		{"TextModel", "defaultTextModel"},
	} {
		value, ok := defaults[item.goName]
		if !ok {
			return "", fmt.Errorf("missing default %s", item.goName)
		}
		b.WriteString("    public static let " + item.swift + " = " + strconv.Quote(value) + "\n")
	}
	b.WriteString("}\n\nextension MacSettings {\n")
	b.WriteString("    public static func acceptsBaseURL(_ value: String) -> Bool {\n")
	b.WriteString("        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)\n")
	b.WriteString("        guard trimmed.contains(\"://\"),\n")
	b.WriteString("              let url = URLComponents(string: trimmed),\n")
	b.WriteString("              let scheme = url.scheme?.lowercased(),\n")
	checks := make([]string, len(schemes))
	for i, scheme := range schemes {
		checks[i] = "scheme == " + strconv.Quote(scheme)
	}
	b.WriteString("              " + strings.Join(checks, " || ") + ",\n")
	b.WriteString("              let host = url.host, !host.isEmpty")
	if mentions(validate, "User") {
		b.WriteString(",\n              url.user == nil, url.password == nil")
	}
	if mentions(validate, "RawQuery") {
		b.WriteString(",\n              (url.percentEncodedQuery ?? \"\").isEmpty")
	}
	if mentions(validate, "Fragment") {
		b.WriteString(",\n              (url.fragment ?? \"\").isEmpty")
	}
	b.WriteString(",\n              url.url != nil else {\n            return false\n        }\n        return true\n    }\n}\n")
	return b.String(), nil
}

func defaultStrings(file *ast.File) (map[string]string, error) {
	fn := findFunc(file, "Defaults")
	if fn == nil {
		return nil, fmt.Errorf("Defaults")
	}
	out := map[string]string{}
	ast.Inspect(fn, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			if text, ok := stringLit(kv.Value); ok {
				out[key.Name] = text
			}
		}
		return true
	})
	return out, nil
}

func schemeLits(fn *ast.FuncDecl) []string {
	var out []string
	ast.Inspect(fn, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || (bin.Op != token.NEQ && bin.Op != token.EQL) {
			return true
		}
		if sel, ok := bin.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Scheme" {
			if text, ok := stringLit(bin.Y); ok {
				out = append(out, text)
			}
		}
		return true
	})
	return out
}

func mentions(fn *ast.FuncDecl, field string) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == field {
			found = true
		}
		return true
	})
	return found
}

func emitSelection(path string) (string, error) {
	file, err := parseFile(path)
	if err != nil {
		return "", err
	}
	fn := findFunc(file, "SelectedLengthError")
	if fn == nil {
		return "", fmt.Errorf("SelectedLengthError")
	}
	limit := ""
	format := ""
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name == "limit" {
				if lit, ok := as.Rhs[0].(*ast.BasicLit); ok {
					limit = lit.Value
				}
			}
		}
		call, ok := n.(*ast.CallExpr)
		if ok {
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "Errorf" && len(call.Args) > 0 {
				if text, ok := stringLit(call.Args[0]); ok {
					format = text
				}
			}
		}
		return true
	})
	if limit == "" || !strings.Contains(format, "%d") {
		return "", fmt.Errorf("selection limit %q %q", limit, format)
	}
	sentence := strings.ReplaceAll(format, "%d", "\\(bounded)")
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/textedit"))
	b.WriteString("import Foundation\n\npublic enum SelectionLimit {\n")
	b.WriteString("    public static let defaultMaxScalars = " + limit + "\n\n")
	b.WriteString("    public static func tooLongMessage(limit: Int = defaultMaxScalars, selected: String) -> String? {\n")
	b.WriteString("        let bounded = limit > 0 ? limit : defaultMaxScalars\n")
	b.WriteString("        guard selected.unicodeScalars.count > bounded else { return nil }\n")
	b.WriteString("        return \"" + sentence + "\"\n")
	b.WriteString("    }\n}\n")
	return b.String(), nil
}

func emitSettings() string {
	bindings := map[string]string{
		"asr_base_url":       "model.settings.asrBaseURL",
		"asr_model":          "model.settings.asrModel",
		"asr_api_key":        "model.asrKeyInput",
		"llm_base_url":       "model.settings.llmBaseURL",
		"text_model":         "model.settings.textModel",
		"llm_api_key":        "model.llmKeyInput",
		"max_record_seconds": "model.settings.maxRecordSeconds",
		"rewrite_shortcut":   "model.settings.rewriteShortcut",
		"dictation_shortcut": "model.settings.dictationShortcut",
		"text_edit_shortcut": "model.settings.textEditShortcut",
	}
	actions := map[string]string{
		"probe_asr": "model.checkASRService()",
		"probe_llm": "model.checkLLMService()",
		"save":      "model.saveSettings()",
	}
	status := map[string]string{
		"probe_asr_status": "model.asrProbeStatus",
		"probe_llm_status": "model.llmProbeStatus",
	}
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/contract"))
	b.WriteString("import SwiftUI\nimport VoiceRewriteStorage\n\nstruct SettingsView: View {\n")
	b.WriteString("    @ObservedObject var model: WorkspaceModel\n\n    var body: some View {\n        Form {\n")
	for _, page := range contract.Sections() {
		controls := contract.PageControls(page.ID, contract.MacOS)
		if len(controls) == 0 {
			continue
		}
		b.WriteString("            Section(" + strconv.Quote(page.Title) + ") {\n")
		for _, control := range controls {
			if page.ID == "save" && control.Kind == contract.Note {
				b.WriteString("                Text(model.settingsStatus).foregroundStyle(.secondary)\n")
				b.WriteString("                    .font(.footnote).textSelection(.enabled)\n")
			}
			b.WriteString(renderControl(control, bindings, actions, status))
		}
		b.WriteString("            }\n")
	}
	b.WriteString("        }\n")
	for _, control := range contract.Controls() {
		if control.StaleProbe == "" || !control.VisibleOn(contract.MacOS) {
			continue
		}
		b.WriteString("        .onChange(of: " + bindings[control.ID] + ") { _, _ in model.markConnectionProbeStale(." + control.StaleProbe + ") }\n")
	}
	b.WriteString("        .formStyle(.grouped)\n    }\n}\n")
	return b.String()
}

func renderControl(control contract.Control, bindings, actions, status map[string]string) string {
	id := ".accessibilityIdentifier(" + strconv.Quote(control.ID) + ")"
	switch control.Kind {
	case contract.Text, contract.Shortcut:
		return "                TextField(" + strconv.Quote(control.Label) + ", text: $" + bindings[control.ID] + ")\n                    " + id + "\n"
	case contract.Secret:
		return "                SecureField(" + strconv.Quote(control.Hint) + ", text: $" + bindings[control.ID] + ")\n                    .accessibilityLabel(" + strconv.Quote(control.Label) + ")\n                    " + id + "\n"
	case contract.Stepper:
		return "                Stepper(\"" + control.Label + "：\\(model.settings.maxRecordSeconds) 秒\",\n                        value: $model.settings.maxRecordSeconds, in: MacSettings.recordSeconds)\n                    " + id + "\n"
	case contract.Button:
		disabled := ""
		if strings.HasPrefix(control.ID, "probe_") {
			disabled = "\n                    .disabled(model.isCheckingServices)"
		}
		return "                Button(" + strconv.Quote(control.Label) + ") { " + actions[control.ID] + " }" + disabled + "\n                    " + id + "\n"
	case contract.Status:
		return "                Text(" + status[control.ID] + ").font(.footnote).foregroundStyle(.secondary)\n                    .textSelection(.enabled)\n                    " + id + "\n"
	case contract.Note:
		return "                Text(" + strconv.Quote(control.Label) + ")\n                    .font(.footnote).foregroundStyle(.secondary)\n                    " + id + "\n"
	default:
		return ""
	}
}
