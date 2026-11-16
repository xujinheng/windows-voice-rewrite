package contractgen

type parameter struct {
	goName, external string
}

type signature struct {
	name   string
	params []parameter
	result kind
}

func p(goName, external string) parameter { return parameter{goName, external} }

var signatures = map[string]signature{
	"Width":                    {name: "width", params: []parameter{p("phase", "_")}, result: kindInt},
	"Terminal":                 {name: "terminal", params: []parameter{p("phase", "_")}, result: kindBool},
	"Label":                    {name: "label", params: []parameter{p("phase", "_"), p("mode", "")}, result: kindString},
	"Accent":                   {name: "accent", params: []parameter{p("phase", ""), p("mode", "")}, result: kindOther},
	"TerminalMark":             {name: "terminalMark", params: []parameter{p("phase", "_")}, result: kindOther},
	"Opacity":                  {name: "opacity", params: []parameter{p("started", ""), p("changed", ""), p("now", ""), p("phase", "")}, result: kindUint8},
	"StatusOpacity":            {name: "statusOpacity", params: []parameter{p("started", ""), p("changed", ""), p("now", ""), p("phase", ""), p("pending", "")}, result: kindUint8},
	"SpinnerDot":               {name: "spinnerDot", params: []parameter{p("index", ""), p("tick", "")}, result: kindOther},
	"WaveformX":                {name: "waveformX", params: []parameter{p("i", "_")}, result: kindInt},
	"WaveHeight":               {name: "waveHeight", params: []parameter{p("i", "index"), p("level", ""), p("tick", "")}, result: kindInt},
	"DurationText":             {name: "durationText", params: []parameter{p("started", ""), p("now", "")}, result: kindString},
	"NormalizeMicrophoneLevel": {name: "normalizeMicrophoneLevel", params: []parameter{p("raw", "_")}, result: kindDouble},
	"SmoothedLevel":            {name: "smoothedLevel", params: []parameter{p("previous", ""), p("incoming", "")}, result: kindDouble},
	"ResultCardTitle":          {name: "cardTitle", params: []parameter{p("intent", "")}, result: kindString},
	"HistoryTitle":             {name: "historyTitle", params: []parameter{p("intent", "")}, result: kindString},
	"HistoryRestoreTitle":      {name: "historyRestoreTitle", params: []parameter{p("storedTitle", "_")}, result: kindString},
	"Kind":                     {name: "kind", params: []parameter{p("title", "")}, result: kindString},
	"ClickDismisses":           {name: "clickDismisses", params: []parameter{p("x", ""), p("y", ""), p("window", ""), p("editor", ""), p("buttons", "")}, result: kindBool},
}

func (s signature) decl(result string) string {
	parts := make([]string, len(s.params))
	for i, param := range s.params {
		label := param.external
		if label == "" {
			label = param.goName
		}
		typ := swiftType(param.goName, s)
		if label == "_" {
			parts[i] = "_ " + param.goName + ": " + typ
		} else if label == param.goName {
			parts[i] = param.goName + ": " + typ
		} else {
			parts[i] = label + " " + param.goName + ": " + typ
		}
	}
	return "static func " + s.name + "(" + stringsJoin(parts, ", ") + ") -> " + result
}

func swiftType(goName string, sig signature) string {
	switch sig.name {
	case "width", "terminal", "label", "accent", "terminalMark", "opacity", "statusOpacity":
		if goName == "phase" {
			return "IndicatorPhase"
		}
	}
	switch goName {
	case "mode", "intent", "title", "storedTitle":
		return "String"
	case "started", "changed", "now":
		return "Date"
	case "pending":
		return "Bool"
	case "index", "i":
		return "Int"
	case "tick":
		return "UInt32"
	case "level", "previous", "incoming", "raw":
		return "Double"
	case "x", "y":
		return "Int32"
	case "window", "editor":
		return "ResultHitRect"
	case "buttons":
		return "[ResultHitRect]"
	default:
		return "Int"
	}
}

func stringsJoin(parts []string, sep string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += sep
		}
		out += part
	}
	return out
}
