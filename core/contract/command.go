package contract

const (
	SaveButton        = 9101
	CloseButton       = 9102
	AdvancedButton    = 9103
	ProbeASRButton    = 9104
	ProbeLLMButton    = 9105
	DiscoverASRButton = 9150
	DiscoverLLMButton = 9151
	NavSpeech         = 9120
	NavAI             = 9121
	NavInput          = 9122
	CaptureRewrite    = 9140
	CaptureDictation  = 9141
	// EndpointEdit is the first settings text field. The next fields use the
	// following ids in catalog order.
	EndpointEdit = 9130
)

const WaitingForShortcut = "等待按键…"

type Action int

const (
	ActionNone Action = iota
	ActionPage
	ActionCapture
	ActionSave
	ActionClose
	ActionAdvanced
	ActionProbeASR
	ActionProbeLLM
	ActionDiscoverASR
	ActionDiscoverLLM
)

// InputIndex is the Windows settings text-field position for this catalog id.
// Only text and secret fields visible on Windows are counted, in catalog order.
func InputIndex(id string) (int, bool) {
	index := 0
	for _, item := range Controls() {
		if !item.VisibleOn(Windows) || (item.Kind != Text && item.Kind != Secret) {
			continue
		}
		if item.ID == id {
			return index, true
		}
		index++
	}
	return 0, false
}

// ProbeInputs identifies the URL, key and model fields for one provider.
// The URL field is also the probe's stale-state owner.
func ProbeInputs(kind string) (url, key, model string, ok bool) {
	switch kind {
	case "ASR":
		return "asr_base_url", "asr_api_key", "asr_model", true
	case "LLM":
		return "llm_base_url", "llm_api_key", "text_model", true
	default:
		return "", "", "", false
	}
}

// ProbeSlot is the connection check cleared when this field changes.
// Slots follow the first appearance of each stale-probe name in the catalog.
func ProbeSlot(id string) (int, bool) {
	seen := map[string]int{}
	for _, item := range Controls() {
		if item.StaleProbe == "" {
			continue
		}
		slot, ok := seen[item.StaleProbe]
		if !ok {
			slot = len(seen)
			seen[item.StaleProbe] = slot
		}
		if item.ID == id {
			return slot, true
		}
	}
	return 0, false
}

// Command is the settings action for a Windows control id.
// Page actions also return the zero-based page index.
func Command(id int) (Action, int) {
	switch id {
	case NavSpeech, NavAI, NavInput:
		return ActionPage, id - NavSpeech
	case CaptureRewrite, CaptureDictation:
		return ActionCapture, 0
	case SaveButton:
		return ActionSave, 0
	case CloseButton:
		return ActionClose, 0
	case AdvancedButton:
		return ActionAdvanced, 0
	case ProbeASRButton:
		return ActionProbeASR, 0
	case ProbeLLMButton:
		return ActionProbeLLM, 0
	case DiscoverASRButton:
		return ActionDiscoverASR, 0
	case DiscoverLLMButton:
		return ActionDiscoverLLM, 0
	default:
		return ActionNone, 0
	}
}

// ButtonLabel is the owner-draw caption for a settings button.
// The save button is the primary action. A capture button shows the waiting
// caption while that control is listening for a key.
func ButtonLabel(id int, capturing bool) (string, bool) {
	switch id {
	case SaveButton:
		return controlLabel("save"), true
	case CloseButton:
		return controlLabel("close"), false
	case AdvancedButton:
		return controlLabel("advanced"), false
	case ProbeASRButton:
		return controlLabel("probe_asr"), false
	case ProbeLLMButton:
		return controlLabel("probe_llm"), false
	case DiscoverASRButton:
		return controlLabel("discover_asr_models"), false
	case DiscoverLLMButton:
		return controlLabel("discover_llm_models"), false
	case CaptureRewrite, CaptureDictation:
		if capturing {
			return WaitingForShortcut, false
		}
		return controlLabel("capture_rewrite"), false
	default:
		return "", false
	}
}

func controlLabel(id string) string {
	control, ok := Find(id)
	if !ok {
		return ""
	}
	return control.Label
}
