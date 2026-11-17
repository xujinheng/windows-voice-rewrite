package contractgen

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

func Generate(moduleRoot string) (map[string]string, error) {
	repo := filepath.Dir(moduleRoot)
	core := filepath.Join(moduleRoot, "internal", "core")
	out := map[string]string{}
	indicator, err := emitIndicator(filepath.Join(core, "indicator"))
	if err != nil {
		return nil, fmt.Errorf("indicator: %w", err)
	}
	out[filepath.Join(repo, "macos/VoiceRewrite/UI/IndicatorMath.swift")] = indicator
	copyFile, err := emitCopy(core)
	if err != nil {
		return nil, fmt.Errorf("copy: %w", err)
	}
	out[filepath.Join(repo, "macos/VoiceRewrite/UI/ResultCopy.swift")] = copyFile
	desktop, err := emitDesktop(core)
	if err != nil {
		return nil, fmt.Errorf("desktop: %w", err)
	}
	out[filepath.Join(repo, "macos/VoiceRewrite/UI/DesktopWorkArea.swift")] = desktop
	history, err := emitHistory(filepath.Join(core, "history", "history.go"))
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	out[filepath.Join(repo, "macos/VoiceRewrite/App/SessionHistory.swift")] = history
	limits, err := emitLimits(moduleRoot)
	if err != nil {
		return nil, fmt.Errorf("limits: %w", err)
	}
	out[filepath.Join(repo, "macos/Modules/Storage/Sources/VoiceRewriteStorage/ContractLimits.swift")] = limits
	selection, err := emitSelection(filepath.Join(core, "textedit", "rewrite.go"), filepath.Join(core, "config", "config.go"))
	if err != nil {
		return nil, fmt.Errorf("selection: %w", err)
	}
	out[filepath.Join(repo, "macos/Modules/Interaction/Sources/VoiceRewriteInteraction/SelectionLimit.swift")] = selection
	hold, err := emitSideHold(filepath.Join(core, "input", "hold.go"))
	if err != nil {
		return nil, fmt.Errorf("hold: %w", err)
	}
	out[filepath.Join(repo, "macos/Modules/Interaction/Sources/VoiceRewriteInteraction/SideHold.swift")] = hold
	paste, err := emitPasteTiming(filepath.Join(core, "clipboard", "paste_receipt.go"))
	if err != nil {
		return nil, fmt.Errorf("paste timing: %w", err)
	}
	out[filepath.Join(repo, "macos/Modules/Interaction/Sources/VoiceRewriteInteraction/PasteTiming.swift")] = paste
	delivery, err := emitDelivery(filepath.Join(core, "delivery", "delivery.go"))
	if err != nil {
		return nil, fmt.Errorf("delivery: %w", err)
	}
	out[filepath.Join(repo, "macos/Modules/Interaction/Sources/VoiceRewriteInteraction/DeliveryState.swift")] = delivery
	logPath := filepath.Join(repo, "macos/VoiceRewrite/App/DiagnosticLog.swift")
	logText, err := emitDiagnosticLog(logPath, filepath.Join(core, "logfile", "rotate.go"))
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	out[logPath] = logText
	settings, err := emitSettings()
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	out[filepath.Join(repo, "macos/VoiceRewrite/UI/SettingsView.swift")] = settings
	vectors, err := emitEndpointVectors()
	if err != nil {
		return nil, fmt.Errorf("endpoints: %w", err)
	}
	out[filepath.Join(repo, "macos/Modules/Storage/Tests/VoiceRewriteStorageTests/CanonicalEndpointVectors.swift")] = vectors
	return out, nil
}

func emitPasteTiming(path string) (string, error) {
	file, err := parseFile(path)
	if err != nil {
		return "", err
	}
	values := map[string]string{}
	for _, spec := range constSpecs(file) {
		values[spec.name] = spec.value
	}
	names := []string{"PasteReceiptQuietMilliseconds", "PasteReceiptMinimumMilliseconds", "PasteReceiptMaxMilliseconds", "PasteReceiptPollMilliseconds"}
	for _, name := range names {
		if values[name] == "" {
			return "", fmt.Errorf("missing %s", name)
		}
	}
	return sourceNote("voice-rewrite/internal/core/clipboard") +
		"import Foundation\n\npublic enum PasteTiming {\n" +
		"    public static let quietMilliseconds = " + values[names[0]] + "\n" +
		"    public static let minimumMilliseconds = " + values[names[1]] + "\n" +
		"    public static let maxMilliseconds = " + values[names[2]] + "\n" +
		"    public static let pollMilliseconds = " + values[names[3]] + "\n" +
		"    public static func restoreReady(elapsed: TimeInterval, sinceLastRead: TimeInterval?, hasRead: Bool) -> Bool {\n" +
		"        elapsed >= Double(maxMilliseconds) / 1000 ||\n" +
		"            (elapsed >= Double(minimumMilliseconds) / 1000 && hasRead &&\n" +
		"             (sinceLastRead ?? 0) >= Double(quietMilliseconds) / 1000)\n" +
		"    }\n}\n", nil
}

func emitDelivery(path string) (string, error) {
	file, err := parseFile(path)
	if err != nil {
		return "", err
	}
	values := map[string]string{}
	for _, spec := range constSpecs(file) {
		values[spec.name] = spec.value
	}
	names := []string{"Blocked", "SentUnverified", "Confirmed", "Uncertain"}
	for _, name := range names {
		if values[name] == "" {
			return "", fmt.Errorf("missing delivery state %s", name)
		}
	}
	return sourceNote("voice-rewrite/internal/core/delivery") +
		"import Foundation\\n\\npublic enum DeliveryState: Int, Sendable {\\n" +
		"    case blocked = " + values[names[0]] + "\\n" +
		"    case sentUnverified = " + values[names[1]] + "\\n" +
		"    case confirmed = " + values[names[2]] + "\\n" +
		"    case uncertain = " + values[names[3]] + "\\n" +
		"    public var requiresManualResult: Bool { self == .blocked || self == .uncertain }\\n" +
		"}\\n", nil
}

func emitSideHold(path string) (string, error) {
	file, err := parseFile(path)
	if err != nil {
		return "", err
	}
	var value string
	for _, spec := range constSpecs(file) {
		if spec.name == "MacSideHold" {
			value = spec.value
		}
	}
	if value == "" {
		return "", fmt.Errorf("MacSideHold")
	}
	return sourceNote("voice-rewrite/internal/core/input") +
		"import Foundation\n\npublic enum SideHold {\n    public static let threshold: TimeInterval = " + value + "\n}\n", nil
}

func sourceNote(path string) string {
	return "// Source: " + path + "\n"
}

func emitIndicator(dir string) (string, error) {
	file, err := parseFile(filepath.Join(dir, "indicator.go"))
	if err != nil {
		return "", err
	}
	colors, err := parseFile(filepath.Join(dir, "color.go"))
	if err != nil {
		return "", err
	}
	c := baseConv()
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/indicator"))
	b.WriteString("import Foundation\n\n")
	b.WriteString("struct IndicatorRGB: Equatable {\n    let r, g, b: UInt8\n}\n\n")
	phases, err := phaseConsts(file)
	if err != nil {
		return "", err
	}
	b.WriteString("enum IndicatorPhase: Int {\n")
	for _, phase := range phases {
		c.phases[phase.name] = true
		b.WriteString(fmt.Sprintf("    case %s = %d\n", lowerCamel(phase.name), phase.value))
	}
	b.WriteString("}\n\nenum IndicatorMath {\n")
	for _, spec := range constSpecs(file) {
		if c.phases[spec.name] {
			continue
		}
		swift := indicatorSwiftName(spec.name)
		c.names[spec.name] = swift
		c.constKind[spec.name] = spec.kind
		typ := "Int"
		if spec.kind == kindDouble {
			typ = "TimeInterval"
			if spec.name == "MicrophoneLevelGain" {
				typ = "Double"
			}
		}
		b.WriteString(fmt.Sprintf("    static let %s: %s = %s\n", swift, typ, spec.value))
	}
	b.WriteByte('\n')
	for _, color := range colorVars(colors) {
		c.names[color.name] = lowerCamel(color.name)
		b.WriteString(fmt.Sprintf("    static let %s = IndicatorRGB(r: %s, g: %s, b: %s)\n", lowerCamel(color.name), color.r, color.g, color.blue))
	}
	b.WriteByte('\n')
	order := []struct{ name, result string }{
		{"Width", "Int"},
		{"Terminal", "Bool"},
		{"CancelChoice", "Int"},
		{"Label", "String"},
		{"Accent", "IndicatorRGB"},
		{"TerminalMark", "IndicatorRGB"},
		{"SpinnerDot", "(x: Int, y: Int, alpha: UInt8)"},
		{"WaveformX", "Int"},
		{"WaveHeight", "Int"},
		{"DurationText", "String"},
		{"SmoothedLevel", "Double"},
		{"NormalizeMicrophoneLevel", "Double"},
		{"Opacity", "UInt8"},
		{"StatusOpacity", "UInt8"},
	}
	for _, fn := range order {
		text, err := emitStaticFiles(c, []*ast.File{file, colors}, fn.name, fn.result)
		if err != nil {
			return "", fmt.Errorf("%s: %w", fn.name, err)
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func emitStaticFiles(c *conv, files []*ast.File, name, result string) (string, error) {
	for _, file := range files {
		if c.fn(name, file) != nil {
			return emitStatic(c, file, name, result)
		}
	}
	return "", fmt.Errorf("missing %s", name)
}

func emitStatic(c *conv, file *ast.File, name, result string) (string, error) {
	fn := c.fn(name, file)
	if fn == nil {
		return "", fmt.Errorf("missing %s", name)
	}
	c.prepare(fn)
	body, err := c.block(fn.Body, 2)
	if err != nil {
		return "", err
	}
	return "    " + signatures[name].decl(result) + " {\n" + c.paramCopies(fn, 2) + body + "    }\n", nil
}

func baseConv() *conv {
	c := &conv{
		names:     map[string]string{},
		phases:    map[string]bool{},
		constKind: map[string]kind{},
	}
	for goName, sig := range signatures {
		c.names[goName] = sig.name
	}
	return c
}

type numberConst struct {
	name, value string
	kind        kind
}

func constSpecs(file *ast.File) []numberConst {
	var out []numberConst
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if text, ok := constNumber(vs.Values[i]); ok {
					k := kindInt
					if strings.Contains(text, ".") {
						k = kindDouble
					}
					out = append(out, numberConst{name: name.Name, value: text, kind: k})
				}
			}
		}
	}
	return out
}

func indicatorSwiftName(goName string) string {
	switch goName {
	case "IntroDuration":
		return "intro"
	case "PhaseEaseDuration":
		return "phaseEase"
	case "OutroDuration":
		return "outro"
	case "MicrophoneLevelGain":
		return "microphoneGain"
	default:
		return lowerCamel(goName)
	}
}

type phaseDef struct {
	name  string
	value int
}

func phaseConsts(file *ast.File) ([]phaseDef, error) {
	var out []phaseDef
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		current := ""
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if vs.Type != nil {
				id, ok := vs.Type.(*ast.Ident)
				if ok {
					current = id.Name
				}
			}
			if current != "Phase" || len(vs.Values) == 0 {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					return nil, fmt.Errorf("phase %s", name.Name)
				}
				value, err := strconv.Atoi(lit.Value)
				if err != nil {
					return nil, err
				}
				out = append(out, phaseDef{name: name.Name, value: value})
			}
		}
	}
	return out, nil
}

type rgbVar struct{ name, r, g, blue string }

func colorVars(file *ast.File) []rgbVar {
	var out []rgbVar
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok || len(lit.Elts) != 3 {
					continue
				}
				out = append(out, rgbVar{name: name.Name, r: lit.Elts[0].(*ast.BasicLit).Value, g: lit.Elts[1].(*ast.BasicLit).Value, blue: lit.Elts[2].(*ast.BasicLit).Value})
			}
		}
	}
	return out
}

func emitCopy(core string) (string, error) {
	result, err := parseFile(filepath.Join(core, "ux", "result.go"))
	if err != nil {
		return "", err
	}
	errorsFile, err := parseFile(filepath.Join(core, "ux", "errors.go"))
	if err != nil {
		return "", err
	}
	probe, err := parseFile(filepath.Join(core, "serviceprobe", "probe.go"))
	if err != nil {
		return "", err
	}
	values := stringConsts(result)
	for name, value := range stringConsts(errorsFile) {
		values[name] = value
	}
	for name, value := range stringConsts(probe) {
		values[name] = value
	}
	c := baseConv()
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/ux"))
	b.WriteString("import Foundation\n\nenum ResultCopy {\n")
	for _, item := range []struct{ goName, swift string }{
		{"DictationHistoryTitle", "dictationHistory"},
		{"DictationNotInserted", "dictationNotInserted"},
		{"DictationCopyManually", "dictationCopyManually"},
		{"DictationWriteUnconfirmed", "dictationWriteUnconfirmed"},
		{"CopyAction", "copyAction"},
		{"ApplyAction", "applyAction"},
		{"CloseGlyph", "closeGlyph"},
		{"CopySucceeded", "copySucceeded"},
		{"CopyFailed", "copyFailed"},
		{"PasswordDictation", "passwordDictation"},
		{"PasswordSelection", "passwordSelection"},
		{"PasswordRewrite", "passwordRewrite"},
		{"DictationEmpty", "dictationEmpty"},
		{"NoSpeech", "noSpeech"},
		{"ViewDiagnosticLog", "viewDiagnosticLog"},
	} {
		text, ok := values[item.goName]
		if !ok {
			return "", fmt.Errorf("missing copy %s", item.goName)
		}
		b.WriteString("    static let " + item.swift + " = " + strconv.Quote(text) + "\n")
	}
	b.WriteByte('\n')
	for _, fn := range []struct{ name, result string }{
		{"ResultCardTitle", "String"},
		{"HistoryTitle", "String"},
		{"HistoryRestoreTitle", "String"},
	} {
		text, err := emitStatic(c, result, fn.name, fn.result)
		if err != nil {
			return "", err
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
	b.WriteString("enum ServiceProbeCopy {\n")
	b.WriteString("    static let idle = " + quoteConst(values, "StatusIdle") + "\n")
	b.WriteString("    static let running = " + quoteConst(values, "StatusRunning") + "\n")
	b.WriteString("    static let stale = " + quoteConst(values, "StatusStale") + "\n")
	b.WriteString("}\n\nenum SettingsCopy {\n")
	b.WriteString("    static let saveWhileBusy = " + quoteConst(values, "SaveWhileBusy") + "\n")
	b.WriteString("}\n\nenum VoicePauseCopy {\n")
	b.WriteString("    static let paused = " + quoteConst(values, "VoicePaused") + "\n")
	b.WriteString("    static let idle = " + quoteConst(values, "VoiceIdle") + "\n")
	b.WriteString("    static let pause = " + quoteConst(values, "PauseVoice") + "\n")
	b.WriteString("    static let resume = " + quoteConst(values, "ResumeVoice") + "\n")
	b.WriteString("    static let busy = " + quoteConst(values, "PauseWhileBusy") + "\n")
	b.WriteString("}\n\n")
	shortcutFile, err := parseFile(filepath.Join(core, "shortcut", "shortcut.go"))
	if err != nil {
		return "", err
	}
	failure, err := emitFailure(errorsFile, result, shortcutFile)
	if err != nil {
		return "", err
	}
	b.WriteString(failure)
	return b.String(), nil
}

func emitFailure(errorsFile, result, shortcutFile *ast.File) (string, error) {
	values := stringConsts(errorsFile)
	for name, value := range stringConsts(result) {
		values[name] = value
	}
	for name, value := range stringConsts(shortcutFile) {
		values[name] = value
	}
	sentences, err := recoverySentences(findFunc(errorsFile, "fixedRecovery"), values)
	if err != nil {
		return "", err
	}
	extra, err := recoverySentences(findFunc(errorsFile, "ownedSentence"), values)
	if err != nil {
		return "", err
	}
	shortcutSentences, err := recoverySentences(findFunc(shortcutFile, "FixedMessage"), values)
	if err != nil {
		return "", err
	}
	affixes, err := affixChecks(findFunc(shortcutFile, "FixedMessage"), values)
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(sentences)+len(extra)+len(shortcutSentences))
	for _, sentence := range append(append(sentences, extra...), shortcutSentences...) {
		if sentence == "" || seen[sentence] {
			continue
		}
		seen[sentence] = true
		ordered = append(ordered, sentence)
	}
	head, tail, err := selectionAffixes(findFunc(errorsFile, "fixedRecovery"))
	if err != nil {
		return "", err
	}
	if len(ordered) == 0 || head == "" || tail == "" || len(affixes) == 0 {
		return "", fmt.Errorf("failure catalog")
	}
	var b strings.Builder
	b.WriteString("enum FailureCopy {\n")
	b.WriteString("    static func status(prefix: String, message: String) -> String {\n")
	b.WriteString("        owned(message) ? message : prefix + message\n")
	b.WriteString("    }\n\n")
	b.WriteString("    static func owned(_ message: String) -> Bool {\n")
	b.WriteString("        switch message {\n")
	b.WriteString("        case " + quotedList(ordered) + ":\n")
	b.WriteString("            return true\n")
	b.WriteString("        default:\n")
	b.WriteString("            if " + strings.Join(affixes, " || ") + " {\n")
	b.WriteString("                return true\n")
	b.WriteString("            }\n")
	b.WriteString("            let head = " + strconv.Quote(head) + "\n")
	b.WriteString("            let tail = " + strconv.Quote(tail) + "\n")
	b.WriteString("            guard message.hasPrefix(head), message.hasSuffix(tail) else { return false }\n")
	b.WriteString("            let count = message.dropFirst(head.count).dropLast(tail.count)\n")
	b.WriteString("            return !count.isEmpty && count.utf8.allSatisfy { $0 >= 48 && $0 <= 57 }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String(), nil
}

func affixChecks(fn *ast.FuncDecl, values map[string]string) ([]string, error) {
	if fn == nil {
		return nil, fmt.Errorf("missing affix function")
	}
	var out []string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		text := ""
		switch arg := call.Args[1].(type) {
		case *ast.BasicLit:
			text, _ = stringLit(arg)
		case *ast.Ident:
			text = values[arg.Name]
		}
		if text == "" {
			return true
		}
		switch sel.Sel.Name {
		case "HasPrefix":
			out = append(out, "message.hasPrefix("+strconv.Quote(text)+")")
		case "HasSuffix":
			out = append(out, "message.hasSuffix("+strconv.Quote(text)+")")
		}
		return true
	})
	return out, nil
}

func recoverySentences(fn *ast.FuncDecl, values map[string]string) ([]string, error) {
	if fn == nil {
		return nil, fmt.Errorf("missing recovery function")
	}
	var out []string
	var walkErr error
	ast.Inspect(fn, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, item := range clause.List {
			switch label := item.(type) {
			case *ast.Ident:
				text, ok := values[label.Name]
				if !ok {
					walkErr = fmt.Errorf("recovery name %s", label.Name)
					return false
				}
				out = append(out, text)
			case *ast.BasicLit:
				if text, ok := stringLit(label); ok {
					out = append(out, text)
				}
			}
		}
		return true
	})
	return out, walkErr
}

func selectionAffixes(fn *ast.FuncDecl) (string, string, error) {
	head, tail := "", ""
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		text, ok := stringLit(call.Args[1])
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "CutPrefix":
			head = text
		case "CutSuffix":
			tail = text
		}
		return true
	})
	if head == "" || tail == "" {
		return "", "", fmt.Errorf("selection sentence pattern")
	}
	return head, tail, nil
}

func quotedList(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Quote(value)
	}
	return strings.Join(parts, ", ")
}

func quoteConst(values map[string]string, name string) string {
	return strconv.Quote(values[name])
}

func stringConsts(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if text, ok := stringLit(vs.Values[i]); ok {
						out[name.Name] = text
					}
				}
			}
		}
	}
	return out
}

func emitHistory(path string) (string, error) {
	file, err := parseFile(path)
	if err != nil {
		return "", err
	}
	limit, maxBytes := "", ""
	fallback := ""
	format := ""
	for _, spec := range constSpecs(file) {
		switch spec.name {
		case "Limit":
			limit = spec.value
		case "MaxBytes":
			maxBytes = spec.value
		}
	}
	if limit == "" || maxBytes == "" {
		return "", fmt.Errorf("history bounds")
	}
	accept := findFunc(file, "Accept")
	keep := findFunc(file, "KeepCount")
	remember := findFunc(file, "Remember")
	if accept == nil || keep == nil || remember == nil || !callsFunc(remember, "Accept") || !callsFunc(remember, "KeepCount") || !callsSelector(accept, "TrimSpace") {
		return "", fmt.Errorf("history policy")
	}
	fallback, valueOp, valueLimit, err := acceptPolicy(accept)
	if err != nil {
		return "", err
	}
	keepOp, sizeOp, err := keepPolicy(keep)
	if err != nil {
		return "", err
	}
	valueSwift, err := swiftHistoryLimit(valueLimit)
	if err != nil {
		return "", err
	}
	menu := findFunc(file, "MenuLine")
	ast.Inspect(menu, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "Format" && len(call.Args) == 1 {
			if text, ok := stringLit(call.Args[0]); ok {
				format = text
			}
		}
		return true
	})
	if fallback == "" || format != "15:04" {
		return "", fmt.Errorf("history title %q format %q", fallback, format)
	}
	c := baseConv()
	kind, err := emitStatic(c, file, "Kind", "String")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(sourceNote("voice-rewrite/internal/core/history"))
	b.WriteString("import Foundation\n\nstruct SessionHistoryEntry: Identifiable, Equatable {\n")
	b.WriteString("    let id: UInt64\n    let title: String\n    let text: String\n    let createdAt: Date\n}\n\n")
	b.WriteString("struct SessionHistory {\n")
	b.WriteString("    static let limit = " + limit + "\n")
	b.WriteString("    static let maxBytes = " + maxBytes + "\n")
	labels := stringConsts(file)
	for _, item := range []struct{ goName, swift string }{
		{"EmptySession", "emptySession"},
		{"ClearSession", "clearSession"},
	} {
		text, ok := labels[item.goName]
		if !ok {
			return "", fmt.Errorf("history label %s", item.goName)
		}
		b.WriteString("    static let " + item.swift + " = " + strconv.Quote(text) + "\n")
	}
	title, err := sessionTitle(file)
	if err != nil {
		return "", err
	}
	b.WriteString("    static var sectionTitle: String { " + title + " }\n\n")
	b.WriteString("    private(set) var entries: [SessionHistoryEntry] = []\n    private var nextID: UInt64 = 0\n\n")
	b.WriteString("    static func accept(title: String, value: String) -> String? {\n")
	b.WriteString("        if value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || value.utf8.count " + valueOp + " " + valueSwift + " {\n")
	b.WriteString("            return nil\n")
	b.WriteString("        }\n")
	b.WriteString("        if title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {\n")
	b.WriteString("            return " + strconv.Quote(fallback) + "\n")
	b.WriteString("        }\n")
	b.WriteString("        return title\n")
	b.WriteString("    }\n\n")
	b.WriteString("    static func keepCount(_ texts: [String]) -> Int {\n")
	b.WriteString("        var bytes = 0\n        var keep = 0\n")
	b.WriteString("        for text in texts {\n")
	b.WriteString("            if keep " + keepOp + " Self.limit || bytes + text.utf8.count " + sizeOp + " Self.maxBytes { return keep }\n")
	b.WriteString("            bytes += text.utf8.count\n            keep += 1\n")
	b.WriteString("        }\n")
	b.WriteString("        return keep\n")
	b.WriteString("    }\n\n")
	b.WriteString("    mutating func remember(title: String, value: String) {\n")
	b.WriteString("        guard let storedTitle = Self.accept(title: title, value: value) else { return }\n")
	b.WriteString("        nextID += 1\n")
	b.WriteString("        entries.insert(SessionHistoryEntry(\n")
	b.WriteString("            id: nextID,\n")
	b.WriteString("            title: storedTitle,\n")
	b.WriteString("            text: value,\n")
	b.WriteString("            createdAt: Date()\n")
	b.WriteString("        ), at: 0)\n")
	b.WriteString("        let keep = Self.keepCount(entries.map(\\.text))\n")
	b.WriteString("        if keep < entries.count { entries.removeSubrange(keep...) }\n")
	b.WriteString("    }\n\n")
	b.WriteString("    func entry(id: UInt64) -> SessionHistoryEntry? {\n")
	b.WriteString("        entries.first { $0.id == id }\n    }\n\n")
	b.WriteString("    mutating func clear() { entries.removeAll() }\n\n")
	b.WriteString(kind)
	b.WriteString("\n    static func menuLine(index: Int, title: String, createdAt: Date) -> String {\n")
	b.WriteString("        let clock = DateFormatter()\n")
	b.WriteString("        clock.locale = Locale(identifier: \"en_US_POSIX\")\n")
	b.WriteString("        clock.dateFormat = \"HH:mm\"\n")
	b.WriteString("        return \"\\(index + 1). \\(kind(title: title)) · \\(clock.string(from: createdAt))\"\n")
	b.WriteString("    }\n}\n")
	return b.String(), nil
}

func callsFunc(fn *ast.FuncDecl, name string) bool {
	if fn == nil {
		return false
	}
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if ok && id.Name == name {
			found = true
		}
		return true
	})
	return found
}

func callsSelector(fn *ast.FuncDecl, name string) bool {
	if fn == nil {
		return false
	}
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == name {
			found = true
		}
		return true
	})
	return found
}

func acceptPolicy(fn *ast.FuncDecl) (fallback, op, limit string, err error) {
	if err = rejectUnknownCalls(fn, "len", "TrimSpace"); err != nil {
		return
	}
	if err = expectIfCount(fn, 2); err != nil {
		return
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		if expr, ok := n.(ast.Expr); ok {
			if text, ok := stringLit(expr); ok && text != "" {
				fallback = text
			}
		}
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		call, ok := bin.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		lim, ok2 := bin.Y.(*ast.Ident)
		if ok && ok2 && id.Name == "len" {
			op = bin.Op.String()
			limit = lim.Name
		}
		return true
	})
	if fallback == "" || op == "" || limit == "" {
		return "", "", "", fmt.Errorf("history accept shape")
	}
	return fallback, op, limit, nil
}

func keepPolicy(fn *ast.FuncDecl) (keepOp, sizeOp string, err error) {
	if err = rejectUnknownCalls(fn, "len"); err != nil {
		return
	}
	if err = expectIfCount(fn, 1); err != nil {
		return
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if xid, ok := bin.X.(*ast.Ident); ok && xid.Name == "keep" {
			if yid, ok := bin.Y.(*ast.Ident); ok && yid.Name == "Limit" {
				keepOp = bin.Op.String()
			}
		}
		left, ok := bin.X.(*ast.BinaryExpr)
		yid, ok2 := bin.Y.(*ast.Ident)
		if ok && ok2 && left.Op == token.ADD && yid.Name == "MaxBytes" {
			sizeOp = bin.Op.String()
		}
		return true
	})
	if keepOp == "" || sizeOp == "" {
		return "", "", fmt.Errorf("history keep shape")
	}
	return keepOp, sizeOp, nil
}

func rejectUnknownCalls(fn *ast.FuncDecl, allowed ...string) error {
	if fn == nil {
		return fmt.Errorf("missing function")
	}
	allow := map[string]bool{}
	for _, name := range allowed {
		allow[name] = true
	}
	var bad string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if !allow[fun.Name] {
				bad = fun.Name
			}
		case *ast.SelectorExpr:
			if !allow[fun.Sel.Name] {
				bad = fun.Sel.Name
			}
		default:
			bad = "call"
		}
		return true
	})
	if bad != "" {
		return fmt.Errorf("untranslated %s in %s", bad, fn.Name.Name)
	}
	return nil
}

func expectIfCount(fn *ast.FuncDecl, want int) error {
	got := 0
	ast.Inspect(fn, func(n ast.Node) bool {
		if _, ok := n.(*ast.IfStmt); ok {
			got++
		}
		return true
	})
	if fn == nil || got != want {
		name := ""
		if fn != nil {
			name = fn.Name.Name
		}
		return fmt.Errorf("%s has %d ifs", name, got)
	}
	return nil
}

func swiftHistoryLimit(name string) (string, error) {
	switch name {
	case "MaxBytes":
		return "Self.maxBytes", nil
	case "Limit":
		return "Self.limit", nil
	default:
		return "", fmt.Errorf("history limit %s", name)
	}
}

func sessionTitle(file *ast.File) (string, error) {
	fn := findFunc(file, "SectionTitle")
	if fn == nil {
		return "", fmt.Errorf("section title")
	}
	var format string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" {
			return true
		}
		text, ok := stringLit(call.Args[0])
		id, ok2 := call.Args[1].(*ast.Ident)
		if ok && ok2 && id.Name == "Limit" {
			format = text
		}
		return true
	})
	parts := strings.Split(format, "%d")
	if len(parts) != 2 || strings.Count(format, "%") != 1 {
		return "", fmt.Errorf("section title format %q", format)
	}
	return "\"" + parts[0] + "\\(limit)" + parts[1] + "\"", nil
}

func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}
