// Package contract is the settings catalog both desktop hosts render.
// Native widgets and pixel placement stay in the host. Labels, order,
// which controls exist, and the Windows button command are decided here.
package contract

type Platform string

const (
	Both    Platform = ""
	Windows Platform = "windows"
	MacOS   Platform = "macos"
)

type Kind string

const (
	Text     Kind = "text"
	Secret   Kind = "secret"
	Stepper  Kind = "stepper"
	Choice   Kind = "choice"
	Shortcut Kind = "shortcut"
	Button   Kind = "button"
	Note     Kind = "note"
	Status   Kind = "status"
)

type Page struct {
	ID    string
	Title string
}

type Control struct {
	ID       string
	Page     string
	Label    string
	Hint     string
	Kind     Kind
	Platform Platform
	// Stacked controls occupy successive form rows. Actions and notes do not.
	Stacked bool
	// StaleProbe is the macOS connection check marked stale when this field changes.
	StaleProbe string
}

func Pages() []Page {
	return []Page{
		{ID: "speech", Title: "语音识别"},
		{ID: "writing", Title: "AI 写作"},
		{ID: "input", Title: "快捷键与设备"},
	}
}

func Sections() []Page {
	return append(Pages(), Page{ID: "save", Title: "安全保存"})
}

func Controls() []Control {
	return []Control{
		{ID: "asr_base_url", Page: "speech", Label: "API 地址", Kind: Text, Stacked: true, StaleProbe: "transcribe"},
		{ID: "asr_model", Page: "speech", Label: "模型 ID", Kind: Text, Stacked: true, StaleProbe: "transcribe"},
		{ID: "asr_api_key", Page: "speech", Label: "API Key", Hint: "ASR API Key（本地服务可留空；留空保留旧值）", Kind: Secret, Stacked: true, StaleProbe: "transcribe"},
		{ID: "probe_asr", Page: "speech", Label: "测试连接", Kind: Button},
		{ID: "probe_asr_status", Page: "speech", Kind: Status},

		{ID: "llm_base_url", Page: "writing", Label: "API 地址", Kind: Text, Stacked: true, StaleProbe: "selectedTask"},
		{ID: "text_model", Page: "writing", Label: "模型 ID", Kind: Text, Stacked: true, StaleProbe: "selectedTask"},
		{ID: "llm_api_key", Page: "writing", Label: "API Key", Hint: "LLM API Key（本地服务可留空；留空保留旧值）", Kind: Secret, Stacked: true, StaleProbe: "selectedTask"},
		{ID: "probe_llm", Page: "writing", Label: "测试连接", Kind: Button},
		{ID: "probe_llm_status", Page: "writing", Kind: Status},

		{ID: "max_record_seconds", Page: "input", Label: "最长录音", Kind: Stepper, Platform: MacOS, Stacked: true},
		{ID: "record_note", Page: "input", Label: "录音数据为 16 kHz 单声道 PCM16，最长 180 秒。", Kind: Note, Platform: MacOS},
		{ID: "rewrite_shortcut", Page: "input", Label: "语音指令", Kind: Shortcut, Stacked: true},
		{ID: "capture_rewrite", Page: "input", Label: "录入按键", Kind: Button, Platform: Windows},
		{ID: "dictation_shortcut", Page: "input", Label: "听写快捷键", Kind: Shortcut, Stacked: true},
		{ID: "capture_dictation", Page: "input", Label: "录入按键", Kind: Button, Platform: Windows},
		{ID: "text_edit_shortcut", Page: "input", Label: "文字指令", Kind: Text, Platform: MacOS, Stacked: true},
		{ID: "microphone", Page: "input", Label: "麦克风", Kind: Choice, Platform: Windows, Stacked: true},
		{ID: "input_note_windows", Page: "input", Label: "语音指令用于改写/解释/问答；更换按键或设备后需重启软件。", Kind: Note, Platform: Windows},
		{ID: "input_note_macos", Page: "input", Label: "与 Windows 使用同一套写法，例如 Ctrl+Alt+R、F8 或 Mouse4。Alt 在 macOS 上是 Option。未配置鼠标绑定时，Mouse4 默认听写、Mouse5 默认改写；显式配置鼠标键时仅拦截已分配的侧键。F21–F24 在 macOS 上不可用，三个功能不能绑定同一个按键。", Kind: Note, Platform: MacOS},

		{ID: "save", Page: "save", Label: "保存设置", Kind: Button},
		{ID: "advanced", Page: "save", Label: "高级配置", Kind: Button, Platform: Windows},
		{ID: "close", Page: "save", Label: "关闭", Kind: Button, Platform: Windows},
		{ID: "save_note", Page: "save", Label: "API Key 为可选项；需要认证时先保存再检测。连接检测共用 Windows 的 Go Core 规则：ASR 发送生成的静音 WAV，LLM 查询 /models，不发送用户录音或文本。密钥仅保存在 Keychain。", Kind: Note, Platform: MacOS},
	}
}

func PageControls(page string, platform Platform) []Control {
	var out []Control
	for _, control := range Controls() {
		if control.Page != page || !control.VisibleOn(platform) {
			continue
		}
		out = append(out, control)
	}
	return out
}

func Stacked(page string, platform Platform) []Control {
	var out []Control
	for _, control := range PageControls(page, platform) {
		if control.Stacked {
			out = append(out, control)
		}
	}
	return out
}

func Find(id string) (Control, bool) {
	for _, control := range Controls() {
		if control.ID == id {
			return control, true
		}
	}
	return Control{}, false
}

func (c Control) VisibleOn(platform Platform) bool {
	return c.Platform == Both || c.Platform == platform
}
