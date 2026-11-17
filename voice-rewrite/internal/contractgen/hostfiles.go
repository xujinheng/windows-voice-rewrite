package contractgen

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"voicerewrite/internal/contract"
	coreconfig "voicerewrite/internal/core/config"
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
	for _, field := range []string{"Scheme", "Host", "User", "RawQuery", "Fragment", "Opaque"} {
		if !mentions(validate, field) {
			return "", fmt.Errorf("ValidateBaseURL missing %s", field)
		}
	}
	if !callsFunc(validate, "BaseURLEscapesOK") {
		return "", fmt.Errorf("ValidateBaseURL missing BaseURLEscapesOK")
	}
	percent, exception, threshold, separators, schemeMark, err := escapeRule(config)
	if err != nil {
		return "", err
	}
	if err := requireCanonicalEndpoints(config); err != nil {
		return "", err
	}
	if err := requireStoredCredential(config); err != nil {
		return "", err
	}
	schemes := schemeLits(validate)
	if len(schemes) == 0 {
		return "", fmt.Errorf("no url schemes")
	}
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/config, meeting, shortcut, jsonl, and logfile"))
	b.WriteString("import Foundation\n\npublic enum ContractLimits {\n")
	for _, item := range want {
		value, ok := values[item.goName]
		if !ok {
			return "", fmt.Errorf("missing %s", item.goName)
		}
		b.WriteString("    public static let " + item.swift + " = " + value + "\n")
	}
	if text, ok := stringConsts(config)["CredentialTooLong"]; ok {
		b.WriteString("    public static let credentialTooLong = " + strconv.Quote(text) + "\n")
	} else {
		return "", fmt.Errorf("missing CredentialTooLong")
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
	// These values come from the canonical Go Meeting configuration, never
	// from a second hand-maintained list in the native settings UI.
	for _, item := range []struct{ name, value string }{
		{"meetingASRMultipart", coreconfig.MeetingASRMultipart},
		{"meetingASRLlamaPrefix", coreconfig.MeetingASRLlamaPrefix},
		{"defaultMeetingASRMode", coreconfig.Defaults().MeetingASRMode},
		{"meetingASRMultipartTitle", coreconfig.MeetingASRModeTitle(coreconfig.MeetingASRMultipart)},
		{"meetingASRLlamaPrefixTitle", coreconfig.MeetingASRModeTitle(coreconfig.MeetingASRLlamaPrefix)},
	} {
		b.WriteString("    public static let " + item.name + " = " + strconv.Quote(item.value) + "\n")
	}
	b.WriteString("    public static let meetingASRModes = [meetingASRMultipart, meetingASRLlamaPrefix]\n")
	// Meeting view actions are the exact Windows HUD button IDs and share the
	// same Go ApplyCommand transitions through voicebridge on macOS.
	commands, err := parseFile(filepath.Join(moduleRoot, "internal", "core", "meeting", "command.go"))
	if err != nil {
		return "", err
	}
	buttons := map[string]string{}
	for _, spec := range constSpecs(commands) {
		buttons[spec.name] = spec.value
	}
	for _, item := range []struct{ goName, swift string }{
		{"ButtonCaptions", "meetingButtonCaptions"},
		{"ButtonExpression", "meetingButtonExpression"},
	} {
		value, ok := buttons[item.goName]
		if !ok {
			return "", fmt.Errorf("missing meeting control %s", item.goName)
		}
		b.WriteString("    public static let " + item.swift + " = " + value + "\n")
	}
	hud, err := parseFile(filepath.Join(moduleRoot, "internal", "core", "meeting", "hud.go"))
	if err != nil {
		return "", err
	}
	labels := stringConsts(hud)
	for _, item := range []struct{ goName, swift string }{
		{"CaptionFocusTitle", "meetingCaptionsFocusTitle"},
		{"CaptionFullTitle", "meetingCaptionsFullTitle"},
		{"ExpressionExpandTitle", "meetingExpressionExpandTitle"},
		{"ExpressionCollapseTitle", "meetingExpressionCollapseTitle"},
	} {
		value, ok := labels[item.goName]
		if !ok {
			return "", fmt.Errorf("missing meeting title %s", item.goName)
		}
		b.WriteString("    public static let " + item.swift + " = " + strconv.Quote(value) + "\n")
	}
	shortcut, err := parseFile(filepath.Join(moduleRoot, "internal", "core", "shortcut", "shortcut.go"))
	if err != nil {
		return "", err
	}
	shortcutValues := stringConsts(shortcut)
	for _, item := range []struct{ goName, swift string }{
		{"MacDictationShortcut", "macDictationShortcut"},
		{"MacRewriteShortcut", "macRewriteShortcut"},
		{"MacTextEditShortcut", "macTextEditShortcut"},
	} {
		value, ok := shortcutValues[item.goName]
		if !ok {
			return "", fmt.Errorf("missing shortcut default %s", item.goName)
		}
		b.WriteString("    public static let " + item.swift + " = " + strconv.Quote(value) + "\n")
	}
	for _, item := range []struct{ path, goName, swift string }{
		{"jsonl/limits.go", "Version", "protocolVersion"},
		{"jsonl/limits.go", "MaxRequestBytes", "maxRequestBytes"},
		{"jsonl/limits.go", "MaxIDBytes", "maxRequestIDBytes"},
		{"jsonl/limits.go", "MaxSeenIDs", "maxSeenRequestIDs"},
		{"logfile/rotate.go", "MaxBytes", "logMaxBytes"},
		{"logfile/rotate.go", "Backups", "logBackups"},
	} {
		value, err := constValue(filepath.Join(moduleRoot, "internal", "core", item.path), item.goName)
		if err != nil {
			return "", err
		}
		b.WriteString("    public static let " + item.swift + " = " + value + "\n")
	}
	limitsFile, err := parseFile(filepath.Join(moduleRoot, "internal", "core", "jsonl", "limits.go"))
	if err != nil {
		return "", err
	}
	hello, ok := stringConsts(limitsFile)["HelloName"]
	if !ok || hello == "" {
		return "", fmt.Errorf("missing HelloName")
	}
	b.WriteString("    public static let helperName = " + strconv.Quote(hello) + "\n\n")
	b.WriteString("    public static func storedCredentialTooLong(_ value: String) -> Bool {\n")
	b.WriteString("        value.utf8.count > maxCredentialBytes\n")
	b.WriteString("    }\n\n")
	b.WriteString("    public static func canonicalEndpoints(asrBaseURL: String, asrModel: String, llmBaseURL: String, textModel: String) -> CanonicalEndpointFields? {\n")
	b.WriteString("        let asrURL = trimEndpoint(asrBaseURL)\n")
	b.WriteString("        let llmURL = trimEndpoint(llmBaseURL)\n")
	b.WriteString("        var asr = asrModel.trimmingCharacters(in: .whitespacesAndNewlines)\n")
	b.WriteString("        var text = textModel.trimmingCharacters(in: .whitespacesAndNewlines)\n")
	b.WriteString("        if asrURL.utf8.count > maxEndpointBytes || llmURL.utf8.count > maxEndpointBytes || asr.utf8.count > maxModelIDBytes || text.utf8.count > maxModelIDBytes {\n")
	b.WriteString("            return nil\n")
	b.WriteString("        }\n")
	b.WriteString("        if !MacSettings.acceptsBaseURL(asrURL) || !MacSettings.acceptsBaseURL(llmURL) {\n")
	b.WriteString("            return nil\n")
	b.WriteString("        }\n")
	b.WriteString("        if asr.isEmpty { asr = defaultASRModel }\n")
	b.WriteString("        if text.isEmpty { text = defaultTextModel }\n")
	b.WriteString("        return CanonicalEndpointFields(asrBaseURL: asrURL, asrModel: asr, llmBaseURL: llmURL, textModel: text)\n")
	b.WriteString("    }\n\n")
	b.WriteString("    private static func trimEndpoint(_ value: String) -> String {\n")
	b.WriteString("        var text = value.trimmingCharacters(in: .whitespacesAndNewlines)\n")
	b.WriteString("        while text.hasSuffix(\"/\") { text.removeLast() }\n")
	b.WriteString("        return text\n")
	b.WriteString("    }\n\n")
	b.WriteString(swiftEscapeCheck(percent, exception, threshold, separators, schemeMark))
	b.WriteString("}\n\npublic struct CanonicalEndpointFields: Equatable {\n")
	b.WriteString("    public var asrBaseURL: String\n")
	b.WriteString("    public var asrModel: String\n")
	b.WriteString("    public var llmBaseURL: String\n")
	b.WriteString("    public var textModel: String\n")
	b.WriteString("}\n\nextension MacSettings {\n")
	b.WriteString("    public static func acceptsBaseURL(_ value: String) -> Bool {\n")
	b.WriteString("        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)\n")
	b.WriteString("        guard ContractLimits.baseURLEscapesOK(trimmed),\n")
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
	if mentions(validate, "Opaque") {
		b.WriteString(",\n              trimmed.contains(\"://\")")
	}
	b.WriteString(",\n              url.url != nil else {\n            return false\n        }\n        return true\n    }\n}\n")
	return b.String(), nil
}

func constValue(path, name string) (string, error) {
	file, err := parseFile(path)
	if err != nil {
		return "", err
	}
	for _, spec := range constSpecs(file) {
		if spec.name == name {
			return spec.value, nil
		}
	}
	return "", fmt.Errorf("missing %s in %s", name, path)
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

func emitSelection(path, configPath string) (string, error) {
	file, err := parseFile(path)
	if err != nil {
		return "", err
	}
	config, err := parseFile(configPath)
	if err != nil {
		return "", err
	}
	numbers := map[string]string{}
	for _, spec := range constSpecs(config) {
		numbers[spec.name] = spec.value
	}
	for _, spec := range constSpecs(file) {
		numbers[spec.name] = spec.value
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
				if value := numberValue(as.Rhs[0], numbers); value != "" {
					limit = value
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

func numberValue(expr ast.Expr, numbers map[string]string) string {
	switch n := expr.(type) {
	case *ast.BasicLit:
		if n.Kind == token.INT {
			return n.Value
		}
	case *ast.Ident:
		return numbers[n.Name]
	case *ast.SelectorExpr:
		return numbers[n.Sel.Name]
	}
	return ""
}

func settingsMaps() (bindings, actions, status map[string]string) {
	bindings = map[string]string{
		"asr_base_url":       "model.settings.asrBaseURL",
		"asr_model":          "model.settings.asrModel",
		"asr_api_key":        "model.asrKeyInput",
		"llm_base_url":       "model.settings.llmBaseURL",
		"text_model":         "model.settings.textModel",
		"llm_api_key":        "model.llmKeyInput",
		"max_record_seconds": "model.settings.maxRecordSeconds",
		"microphone":         "model.settings.microphoneUID",
		"rewrite_shortcut":   "model.settings.rewriteShortcut",
		"dictation_shortcut": "model.settings.dictationShortcut",
	}
	actions = map[string]string{
		"probe_asr": "model.checkASRService()",
		"probe_llm": "model.checkLLMService()",
		"save":      "model.saveSettings()",
	}
	status = map[string]string{
		"probe_asr_status": "model.asrProbeStatus",
		"probe_llm_status": "model.llmProbeStatus",
	}
	return bindings, actions, status
}

func emitSettings() (string, error) {
	bindings, actions, status := settingsMaps()
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/contract"))
	b.WriteString("import SwiftUI\nimport AppKit\nimport VoiceRewriteStorage\nimport Microphone\n\nstruct SettingsView: View {\n    @ObservedObject var model: WorkspaceModel\n    @State private var selectedPage = \"speech\"\n    @State private var showAdvanced = false\n    @State private var captureTarget: String?\n    @State private var captureStatus = \"\"\n    @State private var captureMonitor: Any?\n\n    var body: some View {\n        VStack(spacing: 0) {\n            HStack(spacing: 0) {\n                VStack(alignment: .leading, spacing: 12) {\n                    Text(\"VoiceRewrite\").font(.title3.bold())\n                    Text(\"设置分类\").font(.caption).foregroundStyle(.secondary)\n")
	for _, page := range contract.Pages() {
		b.WriteString("                    Button(" + strconv.Quote(page.Title) + ") { selectPage(" + strconv.Quote(page.ID) + ") }\n")
		b.WriteString("                        .buttonStyle(.plain)\n")
		b.WriteString("                        .font(.system(size: 13, weight: selectedPage == " + strconv.Quote(page.ID) + " ? .semibold : .regular))\n")
		b.WriteString("                        .padding(.vertical, 9)\n                        .padding(.horizontal, 12)\n                        .frame(maxWidth: .infinity, alignment: .leading)\n")
		b.WriteString("                        .background(selectedPage == " + strconv.Quote(page.ID) + " ? Color.accentColor.opacity(0.12) : Color.clear)\n")
		b.WriteString("                        .clipShape(RoundedRectangle(cornerRadius: 9))\n")
		b.WriteString("                        .accessibilityIdentifier(" + strconv.Quote("nav_" + page.ID) + ")\n")
	}
	b.WriteString("                    Spacer(minLength: 0)\n                }\n                .frame(width: 160, alignment: .leading)\n                .padding(16)\n                .frame(maxHeight: .infinity)\n                .background(Color(nsColor: .controlBackgroundColor))\n                Divider()\n                VStack(alignment: .leading, spacing: 0) {\n                    Group {\n")
	for i, page := range contract.Pages() {
		if i == 0 {
			b.WriteString("                        if selectedPage == ")
		} else {
			b.WriteString("                        else if selectedPage == ")
		}
		b.WriteString(strconv.Quote(page.ID) + " {\n")
		b.WriteString("                            VStack(alignment: .leading, spacing: 4) {\n")
		b.WriteString("                                Text(" + strconv.Quote(page.Title) + ").font(.title2.bold())\n")
		b.WriteString("                                Text(" + strconv.Quote(page.Subtitle) + ").font(.subheadline).foregroundStyle(.secondary)\n")
		b.WriteString("                            }\n")
		b.WriteString("                        } \n")
	}
	b.WriteString("                    }\n                    .frame(maxWidth: .infinity, alignment: .leading)\n                    .padding(.horizontal, 24)\n                    .padding(.top, 22)\n                    .padding(.bottom, 14)\n                    Divider()\n                    ScrollView {\n                        VStack(alignment: .leading, spacing: 14) {\n")
	for i, page := range contract.Pages() {
		if i == 0 {
			b.WriteString("                            if selectedPage == ")
		} else {
			b.WriteString("                            else if selectedPage == ")
		}
		b.WriteString(strconv.Quote(page.ID) + " {\n")
		b.WriteString("                                Text(" + strconv.Quote(page.Section) + ").font(.headline)\n")
		for _, control := range contract.PageControls(page.ID, contract.MacOS) {
			if strings.HasPrefix(control.ID, "capture_") || control.ID == "max_record_seconds" || control.ID == "record_note" || control.ID == "meeting_asr_mode" {
				continue
			}
			if control.Kind == contract.Shortcut {
				b.WriteString("                                HStack(spacing: 12) {\n")
			}
			line, err := renderControl(control, bindings, actions, status)
			if err != nil {
				return "", err
			}
			b.WriteString(line)
			if control.Kind == contract.Shortcut {
				captureID := "capture_dictation"
				if control.ID == "rewrite_shortcut" {
					captureID = "capture_rewrite"
				}
				capture, ok := contract.Find(captureID)
				if !ok || !capture.VisibleOn(contract.MacOS) {
					return "", fmt.Errorf("missing capture %s", captureID)
				}
				line, err := renderControl(capture, bindings, actions, status)
				if err != nil {
					return "", err
				}
				b.WriteString(line)
				b.WriteString("                                }\n")
			}
		}
		if page.ID == "input" {
		b.WriteString("                                if !captureStatus.isEmpty {\n                                    Text(captureStatus).font(.footnote).foregroundStyle(.secondary)\n                                        .accessibilityIdentifier(\"capture_status\")\n                                }\n                                if showAdvanced {\n                                    Divider()\n                                    Text(\"高级配置\").font(.headline)\n")
			for _, id := range []string{"max_record_seconds", "record_note", "meeting_asr_mode"} {
				control, ok := contract.Find(id)
				if !ok || !control.VisibleOn(contract.MacOS) {
					return "", fmt.Errorf("advanced control %s", id)
				}
				line, err := renderControl(control, bindings, actions, status)
				if err != nil {
					return "", err
				}
				b.WriteString(line)
			}
			b.WriteString("                                }\n")
		}
		b.WriteString("                            } \n")
	}
	b.WriteString("                        }\n                        .frame(maxWidth: .infinity, alignment: .leading)\n                        .padding(24)\n                    }\n                }\n                .frame(maxWidth: .infinity, maxHeight: .infinity)\n            }\n            Divider()\n            HStack(spacing: 12) {\n                Button(\"保存设置\") { model.saveSettings() }\n                    .buttonStyle(.borderedProminent)\n                    .accessibilityIdentifier(\"save\")\n                Button(\"高级配置\") {\n                    selectPage(\"input\")\n                    showAdvanced.toggle()\n                }\n                .accessibilityIdentifier(\"advanced\")\n                Spacer()\n                Button(\"关闭\") { NSApp.keyWindow?.close() }\n                    .accessibilityIdentifier(\"close\")\n            }\n            .padding(.horizontal, 20)\n            .padding(.top, 12)\n            Text(model.settingsStatus)\n                .font(.footnote)\n                .foregroundStyle(.secondary)\n                .textSelection(.enabled)\n                .frame(maxWidth: .infinity, alignment: .leading)\n                .padding(.horizontal, 20)\n")
	save, ok := contract.Find("save")
	if !ok {
		return "", fmt.Errorf("missing save")
	}
	advanced, ok := contract.Find("advanced")
	if !ok {
		return "", fmt.Errorf("missing advanced")
	}
	closeButton, ok := contract.Find("close")
	if !ok {
		return "", fmt.Errorf("missing close")
	}
	if save.Label != "保存设置" || advanced.Label != "高级配置" || closeButton.Label != "关闭" {
		return "", fmt.Errorf("footer labels out of sync")
	}
	b.WriteString("            Text(\"API Key 为可选项；需要认证时先保存再检测。连接检测共用 Windows 的 Go Core 规则：ASR 发送生成的静音 WAV，LLM 查询 /models，不发送用户录音或文本。密钥仅保存在 Keychain。\")\n                .font(.footnote).foregroundStyle(.secondary)\n                .accessibilityIdentifier(\"save_note\")\n")
	b.WriteString("                .padding(.horizontal, 20)\n                .padding(.bottom, 12)\n        }\n        .frame(minWidth: 540, minHeight: 480)\n")
	for _, control := range contract.Controls() {
		if control.StaleProbe == "" || !control.VisibleOn(contract.MacOS) {
			continue
		}
		binding, ok := bindings[control.ID]
		if !ok || binding == "" {
			return "", fmt.Errorf("stale probe %s has no binding", control.ID)
		}
		if control.ID == "asr_base_url" || control.ID == "asr_api_key" || control.ID == "llm_base_url" || control.ID == "llm_api_key" {
			b.WriteString("        .onChange(of: " + binding + ") { _, _ in\n")
			b.WriteString("            model.markConnectionProbeStale(." + control.StaleProbe + ")\n")
			b.WriteString("            model.invalidateDiscoveredModels(." + control.StaleProbe + ")\n")
			b.WriteString("        }\n")
		} else {
			b.WriteString("        .onChange(of: " + binding + ") { _, _ in model.markConnectionProbeStale(." + control.StaleProbe + ") }\n")
		}
	}
	b.WriteString("        .onAppear { model.refreshMicrophoneDevices() }\n        .onDisappear { stopCapture() }\n    }\n\n    private func selectPage(_ id: String) {\n        stopCapture()\n        selectedPage = id\n    }\n\n    private func beginCapture(_ id: String) {\n        stopCapture()\n        captureTarget = id\n        captureStatus = \"正在监听按键… 按 Esc 取消\"\n        captureMonitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown, .otherMouseDown]) { event in\n            if event.type == .otherMouseDown {\n                if event.buttonNumber == 3 { acceptCapture(\"Mouse4\"); return nil }\n                if event.buttonNumber == 4 { acceptCapture(\"Mouse5\"); return nil }\n                return event\n            }\n            if event.keyCode == 53 {\n                stopCapture()\n                captureStatus = \"已取消快捷键录入\"\n                return nil\n            }\n            guard let candidate = Self.shortcutFromKey(event) else {\n                captureStatus = \"请使用 Ctrl/Option 加字母数字，或 F1–F20\"\n                return nil\n            }\n            acceptCapture(candidate)\n            return nil\n        }\n    }\n\n    private func acceptCapture(_ candidate: String) {\n        let other = captureTarget == \"dictation_shortcut\"\n            ? model.settings.rewriteShortcut : model.settings.dictationShortcut\n        if candidate.caseInsensitiveCompare(other) == .orderedSame {\n            captureStatus = \"听写与语音指令不能绑定同一个按键\"\n            return\n        }\n        switch captureTarget {\n        case \"dictation_shortcut\": model.settings.dictationShortcut = candidate\n        case \"rewrite_shortcut\": model.settings.rewriteShortcut = candidate\n        default: return\n        }\n        stopCapture()\n        captureStatus = \"已录入 \\(candidate)，保存设置后生效\"\n    }\n\n    private func stopCapture() {\n        if let captureMonitor { NSEvent.removeMonitor(captureMonitor) }\n        captureMonitor = nil\n        captureTarget = nil\n    }\n\n    private static func shortcutFromKey(_ event: NSEvent) -> String? {\n        let keys: [UInt16: String] = [\n            122: \"F1\", 120: \"F2\", 99: \"F3\", 118: \"F4\", 96: \"F5\",\n            97: \"F6\", 98: \"F7\", 100: \"F8\", 101: \"F9\", 109: \"F10\",\n            103: \"F11\", 111: \"F12\", 105: \"F13\", 107: \"F14\",\n            113: \"F15\", 106: \"F16\", 64: \"F17\", 79: \"F18\",\n            80: \"F19\", 90: \"F20\"\n        ]\n        let flags = event.modifierFlags\n        guard !flags.contains(.command) else { return nil }\n        let key: String\n        if let function = keys[event.keyCode] {\n            key = function\n        } else {\n            let char = event.charactersIgnoringModifiers?.uppercased() ?? \"\"\n            guard char.count == 1, let scalar = char.unicodeScalars.first,\n                  (65...90).contains(scalar.value) || (48...57).contains(scalar.value) else {\n                return nil\n            }\n            key = char\n        }\n        var modifiers: [String] = []\n        if flags.contains(.control) { modifiers.append(\"Ctrl\") }\n        if flags.contains(.option) { modifiers.append(\"Alt\") }\n        if flags.contains(.shift) { modifiers.append(\"Shift\") }\n        if key.count == 1 && !modifiers.contains(\"Ctrl\") && !modifiers.contains(\"Alt\") {\n            return nil\n        }\n        return (modifiers + [key]).joined(separator: \"+\")\n    }\n}\n")
	return b.String(), nil
}

func renderControl(control contract.Control, bindings, actions, status map[string]string) (string, error) {
	id := ".accessibilityIdentifier(" + strconv.Quote(control.ID) + ")"
	switch control.Kind {
	case contract.Text, contract.Shortcut:
		binding, err := requiredBinding(control, bindings)
		if err != nil {
			return "", err
		}
		line := "                TextField(" + strconv.Quote(control.Label) + ", text: $" + binding + ")\n                    " + id + "\n"
		if control.ID == "asr_model" || control.ID == "text_model" {
			line += renderModelDiscovery(control.ID)
		}
		return line, nil
	case contract.Choice:
		if control.ID == "meeting_asr_mode" {
			return "                Picker(\"会议语音识别方式\", selection: $model.settings.meetingASRMode) {\n                    Text(ContractLimits.meetingASRMultipartTitle).tag(ContractLimits.meetingASRMultipart)\n                    Text(ContractLimits.meetingASRLlamaPrefixTitle).tag(ContractLimits.meetingASRLlamaPrefix)\n                }\n                .accessibilityIdentifier(\"meeting_asr_mode\")\n", nil
		}
		if control.ID != "microphone" {
			return "", fmt.Errorf("choice %s has no renderer", control.ID)
		}
		if _, err := requiredBinding(control, bindings); err != nil {
			return "", err
		}
		return `                HStack {
                    Picker("麦克风输入设备", selection: $model.settings.microphoneUID) {
                        Text("系统默认麦克风").tag("")
                        ForEach(model.microphoneDevices) { device in
                            Text(device.name).tag(device.id)
                        }
                    }
                    .accessibilityIdentifier("microphone_device")
                    Button("刷新设备") { model.refreshMicrophoneDevices() }
                }
                if !model.settings.microphoneUID.isEmpty &&
                   MicrophoneRecorder.selectionUnavailable(model.settings.microphoneUID,
                       in: model.microphoneDevices) {
                    Text("已保存的麦克风不在当前设备列表中，录音会提示设备不可用。")
                        .font(.footnote).foregroundStyle(.orange)
                }
`, nil
	case contract.Secret:
		binding, err := requiredBinding(control, bindings)
		if err != nil {
			return "", err
		}
		return "                SecureField(" + strconv.Quote(control.Hint) + ", text: $" + binding + ")\n                    .accessibilityLabel(" + strconv.Quote(control.Label) + ")\n                    " + id + "\n", nil
	case contract.Stepper:
		if control.ID != "max_record_seconds" {
			return "", fmt.Errorf("stepper %s has no renderer", control.ID)
		}
		if _, err := requiredBinding(control, bindings); err != nil {
			return "", err
		}
		return "                Stepper(\"" + control.Label + "：\\(model.settings.maxRecordSeconds) 秒\",\n                        value: $model.settings.maxRecordSeconds, in: MacSettings.recordSeconds)\n                    " + id + "\n", nil
	case contract.Button:
		if control.ID == "capture_rewrite" || control.ID == "capture_dictation" {
			target := "dictation_shortcut"
			if control.ID == "capture_rewrite" {
				target = "rewrite_shortcut"
			}
			return "                Button(captureTarget == " + strconv.Quote(target) + " ? " + strconv.Quote(contract.WaitingForShortcut) + " : " + strconv.Quote(control.Label) + ") { beginCapture(" + strconv.Quote(target) + ") }\n                    " + id + "\n", nil
		}
		action, ok := actions[control.ID]
		if !ok || action == "" {
			return "", fmt.Errorf("button %s has no action", control.ID)
		}
		disabled := ""
		if strings.HasPrefix(control.ID, "probe_") {
			disabled = "\n                    .disabled(model.isCheckingServices)"
		}
		return "                Button(" + strconv.Quote(control.Label) + ") { " + action + " }" + disabled + "\n                    " + id + "\n", nil
	case contract.Status:
		label, ok := status[control.ID]
		if !ok || label == "" {
			return "", fmt.Errorf("status %s has no binding", control.ID)
		}
		return "                Text(" + label + ").font(.footnote).foregroundStyle(.secondary)\n                    .textSelection(.enabled)\n                    " + id + "\n", nil
	case contract.Note:
		return "                Text(" + strconv.Quote(control.Label) + ")\n                    .font(.footnote).foregroundStyle(.secondary)\n                    " + id + "\n", nil
	default:
		return "", fmt.Errorf("unsupported kind %q for %s", control.Kind, control.ID)
	}
}

// These choices are generated beside the canonical settings controls rather
// than maintained as a separate Swift form or duplicate UI state machine.
func renderModelDiscovery(id string) string {
	operation, models, busy, status, setting, prefix := "", "", "", "", "", ""
	switch id {
	case "asr_model":
		operation, models, busy, status, setting, prefix = "transcribe", "asrAvailableModels", "isDiscoveringASRModels", "asrModelDiscoveryStatus", "asrModel", "asr"
	case "text_model":
		operation, models, busy, status, setting, prefix = "selectedTask", "llmAvailableModels", "isDiscoveringLLMModels", "llmModelDiscoveryStatus", "textModel", "llm"
	}
	return fmt.Sprintf(`                HStack {
                    Button("发现模型") { model.discoverModels(for: .%s) }
                        .disabled(model.%s)
                        .accessibilityIdentifier("discover_%s_models")
                    Menu("选择发现的模型") {
                        ForEach(model.%s, id: \.self) { id in
                            Button(id) { model.settings.%s = id }
                        }
                    }
                    .disabled(model.%s.isEmpty)
                    .accessibilityIdentifier("select_%s_model")
                }
                if !model.%s.isEmpty {
                    Text(model.%s).font(.footnote).foregroundStyle(.secondary)
                        .accessibilityIdentifier("%s_models_status")
                }
`, operation, busy, prefix, models, setting, models, prefix, status, status, prefix)
}

func requiredBinding(control contract.Control, bindings map[string]string) (string, error) {
	binding, ok := bindings[control.ID]
	if !ok || binding == "" {
		return "", fmt.Errorf("control %s has no binding", control.ID)
	}
	return binding, nil
}

func escapeRule(file *ast.File) (percent, exception string, threshold int, separators, schemeMark string, err error) {
	if err = requireEscapeClosure(file); err != nil {
		return
	}
	hex := findFunc(file, "percentHexOK")
	host := findFunc(file, "baseURLHost")
	if hex == nil || host == nil {
		err = fmt.Errorf("escape functions")
		return
	}
	var ints []string
	ast.Inspect(hex, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if ok {
			switch lit.Kind {
			case token.CHAR:
				if lit.Value == "'%'" {
					percent = "%"
				}
			case token.STRING:
				text, quoteErr := strconv.Unquote(lit.Value)
				if quoteErr == nil && strings.Contains(text, "%") {
					exception = text
				}
			}
		}
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || bin.Op != token.LSS {
			return true
		}
		call, ok := bin.X.(*ast.CallExpr)
		bound, ok2 := bin.Y.(*ast.BasicLit)
		if !ok || !ok2 || bound.Kind != token.INT || bound.Value == "0" {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if ok && id.Name == "hexNibble" {
			ints = append(ints, bound.Value)
		}
		return true
	})
	if len(ints) != 1 {
		err = fmt.Errorf("escape threshold %v", ints)
		return
	}
	threshold, err = strconv.Atoi(ints[0])
	if err != nil || percent == "" || exception == "" {
		err = fmt.Errorf("escape literals")
		return
	}
	separators, err = selectorString(host, "IndexAny", 1)
	if err != nil {
		return
	}
	schemeMark, err = selectorString(host, "Index", 1)
	return
}

func selectorString(fn *ast.FuncDecl, name string, arg int) (string, error) {
	var text string
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) <= arg {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		if value, ok := stringLit(call.Args[arg]); ok {
			text = value
			found = true
		}
		return true
	})
	if !found {
		return "", fmt.Errorf("%s argument", name)
	}
	return text, nil
}

func requireEscapeClosure(file *ast.File) error {
	local := map[string]bool{"percentHexOK": true, "baseURLHost": true, "hexNibble": true}
	allowedIdent := map[string]bool{"percentHexOK": true, "baseURLHost": true, "hexNibble": true, "int": true, "len": true}
	allowedSel := map[string]bool{"Index": true, "IndexAny": true, "LastIndex": true}
	seen := map[string]bool{}
	var walk func(string) error
	walk = func(name string) error {
		if seen[name] {
			return nil
		}
		seen[name] = true
		fn := findFunc(file, name)
		if fn == nil {
			return fmt.Errorf("missing %s", name)
		}
		var bad string
		var next []string
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				if !allowedIdent[fun.Name] {
					bad = fun.Name
				}
				if local[fun.Name] {
					next = append(next, fun.Name)
				}
			case *ast.SelectorExpr:
				if !allowedSel[fun.Sel.Name] {
					bad = fun.Sel.Name
				}
			default:
				bad = "call"
			}
			return true
		})
		if bad != "" {
			return fmt.Errorf("untranslated %s in %s", bad, name)
		}
		for _, name := range next {
			if err := walk(name); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("BaseURLEscapesOK"); err != nil {
		return err
	}
	nibble := findFunc(file, "hexNibble")
	if nibble == nil {
		return fmt.Errorf("hexNibble")
	}
	want := map[string]bool{"'0'": false, "'9'": false, "'a'": false, "'f'": false, "'A'": false, "'F'": false}
	ast.Inspect(nibble, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if ok && lit.Kind == token.CHAR {
			if _, known := want[lit.Value]; known {
				want[lit.Value] = true
			}
		}
		return true
	})
	for lit, seenLit := range want {
		if !seenLit {
			return fmt.Errorf("hexNibble missing %s", lit)
		}
	}
	return nil
}

func swiftEscapeCheck(percent, exception string, threshold int, separators, schemeMark string) string {
	var b strings.Builder
	b.WriteString("    public static func baseURLEscapesOK(_ value: String) -> Bool {\n")
	b.WriteString("        let bytes = Array(value.utf8)\n")
	b.WriteString("        if !percentBytesOK(bytes, hostRule: false) { return false }\n")
	b.WriteString("        return percentBytesOK(Array(baseURLHost(value).utf8), hostRule: true)\n")
	b.WriteString("    }\n\n")
	b.WriteString("    private static func baseURLHost(_ value: String) -> String {\n")
	b.WriteString("        guard let marker = value.range(of: " + strconv.Quote(schemeMark) + ") else { return \"\" }\n")
	b.WriteString("        var rest = String(value[marker.upperBound...])\n")
	b.WriteString("        if let cut = rest.firstIndex(where: { " + strconv.Quote(separators) + ".contains($0) }) {\n")
	b.WriteString("            rest = String(rest[..<cut])\n")
	b.WriteString("        }\n")
	b.WriteString("        if let at = rest.lastIndex(of: \"@\") {\n")
	b.WriteString("            rest = String(rest[rest.index(after: at)...])\n")
	b.WriteString("        }\n")
	b.WriteString("        return rest\n")
	b.WriteString("    }\n\n")
	b.WriteString("    private static func percentBytesOK(_ bytes: [UInt8], hostRule: Bool) -> Bool {\n")
	b.WriteString("        var index = 0\n")
	b.WriteString("        while index < bytes.count {\n")
	b.WriteString("            if bytes[index] != UInt8(ascii: " + strconv.Quote(percent) + ") {\n")
	b.WriteString("                index += 1\n")
	b.WriteString("                continue\n")
	b.WriteString("            }\n")
	b.WriteString("            if index + 2 >= bytes.count || hexNibble(bytes[index + 1]) < 0 || hexNibble(bytes[index + 2]) < 0 {\n")
	b.WriteString("                return false\n")
	b.WriteString("            }\n")
	b.WriteString("            if hostRule && String(bytes: bytes[index...index + 2], encoding: .utf8) != " + strconv.Quote(exception) + " && hexNibble(bytes[index + 1]) < " + strconv.Itoa(threshold) + " {\n")
	b.WriteString("                return false\n")
	b.WriteString("            }\n")
	b.WriteString("            index += 3\n")
	b.WriteString("        }\n")
	b.WriteString("        return true\n")
	b.WriteString("    }\n\n")
	b.WriteString("    private static func hexNibble(_ byte: UInt8) -> Int {\n")
	b.WriteString("        switch byte {\n")
	b.WriteString("        case UInt8(ascii: \"0\")...UInt8(ascii: \"9\"):\n")
	b.WriteString("            return Int(byte - UInt8(ascii: \"0\"))\n")
	b.WriteString("        case UInt8(ascii: \"a\")...UInt8(ascii: \"f\"):\n")
	b.WriteString("            return Int(byte - UInt8(ascii: \"a\")) + 10\n")
	b.WriteString("        case UInt8(ascii: \"A\")...UInt8(ascii: \"F\"):\n")
	b.WriteString("            return Int(byte - UInt8(ascii: \"A\")) + 10\n")
	b.WriteString("        default:\n")
	b.WriteString("            return -1\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n")
	return b.String()
}

func requireCanonicalEndpoints(file *ast.File) error {
	fn := findFunc(file, "CanonicalEndpoints")
	if fn == nil || !callsSelector(fn, "TrimSpace") || !callsSelector(fn, "TrimRight") || !callsFunc(fn, "ValidateBaseURL") {
		return fmt.Errorf("CanonicalEndpoints shape")
	}
	count := 0
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if ok && id.Name == "ValidateBaseURL" {
			count++
		}
		return true
	})
	if count < 2 || !identMentions(fn, "MaxEndpointBytes") || !identMentions(fn, "MaxModelIDBytes") || !mentions(fn, "ASRModel") || !mentions(fn, "TextModel") {
		return fmt.Errorf("CanonicalEndpoints shape")
	}
	return nil
}

func requireStoredCredential(file *ast.File) error {
	fn := findFunc(file, "StoredCredentialTooLong")
	if fn == nil {
		return fmt.Errorf("StoredCredentialTooLong")
	}
	ok := false
	ast.Inspect(fn, func(n ast.Node) bool {
		ret, isRet := n.(*ast.ReturnStmt)
		if !isRet || len(ret.Results) != 1 {
			return true
		}
		bin, isBin := ret.Results[0].(*ast.BinaryExpr)
		if !isBin || bin.Op != token.GTR {
			return true
		}
		call, isCall := bin.X.(*ast.CallExpr)
		lim, isLim := bin.Y.(*ast.Ident)
		if !isCall || !isLim || lim.Name != "MaxCredentialBytes" || len(call.Args) != 1 {
			return true
		}
		id, isID := call.Fun.(*ast.Ident)
		if isID && id.Name == "len" {
			ok = true
		}
		return true
	})
	if !ok {
		return fmt.Errorf("StoredCredentialTooLong shape")
	}
	return nil
}

func identMentions(fn *ast.FuncDecl, name string) bool {
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

func emitEndpointVectors() (string, error) {
	cases := [][4]string{
		{" http://127.0.0.1:8081/v1/ ", " qwen3-asr ", "http://127.0.0.1:1234/v1", ""},
		{"http://localhost/v1?key=1", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"http://user:pass@127.0.0.1/v1", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"http://127.0.0.1/v1#frag", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"file:///tmp/x", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"http:example.com", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"http://localhost/v1%", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"http://localhost/invalid%ZZ", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"http://ex%41mple.com/v1", "m", "http://127.0.0.1:1234/v1", "auto"},
		{"http://localhost/v1%20ok", "m", "http://127.0.0.1:1234/v1", "auto"},
	}
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/config.CanonicalEndpoints"))
	b.WriteString("import XCTest\n@testable import VoiceRewriteStorage\n\nfinal class CanonicalEndpointVectors: XCTestCase {\n")
	b.WriteString("    func testGoCanonicalEndpoints() {\n")
	for _, item := range cases {
		got, err := coreconfig.CanonicalEndpoints(item[0], item[1], item[2], item[3])
		call := fmt.Sprintf("ContractLimits.canonicalEndpoints(asrBaseURL: %s, asrModel: %s, llmBaseURL: %s, textModel: %s)",
			strconv.Quote(item[0]), strconv.Quote(item[1]), strconv.Quote(item[2]), strconv.Quote(item[3]))
		if err != nil {
			b.WriteString("        XCTAssertNil(" + call + ")\n")
			continue
		}
		b.WriteString("        XCTAssertEqual(" + call + "?.asrBaseURL, " + strconv.Quote(got.ASRBaseURL) + ")\n")
		b.WriteString("        XCTAssertEqual(" + call + "?.asrModel, " + strconv.Quote(got.ASRModel) + ")\n")
		b.WriteString("        XCTAssertEqual(" + call + "?.llmBaseURL, " + strconv.Quote(got.LLMBaseURL) + ")\n")
		b.WriteString("        XCTAssertEqual(" + call + "?.textModel, " + strconv.Quote(got.TextModel) + ")\n")
	}
	raw := strings.Repeat(" ", coreconfig.MaxCredentialBytes)
	b.WriteString("        XCTAssertEqual(ContractLimits.storedCredentialTooLong(String(repeating: \" \", count: " + strconv.Itoa(coreconfig.MaxCredentialBytes) + ")), " + swiftBool(coreconfig.StoredCredentialTooLong(raw)) + ")\n")
	b.WriteString("        XCTAssertEqual(ContractLimits.storedCredentialTooLong(String(repeating: \" \", count: " + strconv.Itoa(coreconfig.MaxCredentialBytes+1) + ")), " + swiftBool(coreconfig.StoredCredentialTooLong(raw+" ")) + ")\n")
	b.WriteString("    }\n}\n")
	return b.String(), nil
}

func swiftBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
