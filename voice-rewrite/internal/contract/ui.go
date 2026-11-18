// Package contract is the settings catalog both desktop hosts render.
// Native widgets and pixel placement stay in the host. Labels, order,
// which controls exist, and the Windows button command are decided here.
// A control with Platform set is missing on the other host. That is not full
// parity. Gap names the issues that still have to add it.
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
	ID       string
	Title    string
	Subtitle string
	Section  string
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
	// Gap is required when Platform is not both hosts. It cites the open issues.
	Gap string
}

func Pages() []Page {
	return []Page{
		{ID: "speech", Title: "语音识别", Subtitle: "连接语音识别服务，将录音转换为文字。", Section: "连接配置"},
		{ID: "writing", Title: "AI 写作", Subtitle: "配置用于改写、翻译与问答的文本模型。", Section: "模型配置"},
		{ID: "input", Title: "快捷键与设备", Subtitle: "设置语音快捷键、录音模式和麦克风。", Section: "输入偏好"},
	}
}

func Sections() []Page {
	return append(Pages(), Page{ID: "save", Title: "安全保存"})
}

func Controls() []Control {
	return []Control{
		{ID: "asr_base_url", Page: "speech", Label: "API 地址", Kind: Text, Stacked: true, StaleProbe: "transcribe"},
		{ID: "asr_model", Page: "speech", Label: "模型 ID", Kind: Text, Stacked: true, StaleProbe: "transcribe"},
		{ID: "discover_asr_models", Page: "speech", Label: "发现模型", Kind: Button},
		{ID: "asr_api_key", Page: "speech", Label: "API Key", Hint: "ASR API Key（本地服务可留空；留空保留旧值）", Kind: Secret, Stacked: true, StaleProbe: "transcribe"},
		{ID: "probe_asr", Page: "speech", Label: "测试连接", Kind: Button},
		{ID: "probe_asr_status", Page: "speech", Kind: Status},

		{ID: "llm_base_url", Page: "writing", Label: "API 地址", Kind: Text, Stacked: true, StaleProbe: "selectedTask"},
		{ID: "text_model", Page: "writing", Label: "模型 ID", Kind: Text, Stacked: true, StaleProbe: "selectedTask"},
		{ID: "discover_llm_models", Page: "writing", Label: "发现模型", Kind: Button},
		{ID: "llm_api_key", Page: "writing", Label: "API Key", Hint: "LLM API Key（本地服务可留空；留空保留旧值）", Kind: Secret, Stacked: true, StaleProbe: "selectedTask"},
		{ID: "probe_llm", Page: "writing", Label: "测试连接", Kind: Button},
		{ID: "probe_llm_status", Page: "writing", Kind: Status},

		{ID: "rewrite_shortcut", Page: "input", Label: "语音指令", Kind: Shortcut, Stacked: true},
		{ID: "capture_rewrite", Page: "input", Label: "录入按键", Kind: Button},
		{ID: "dictation_shortcut", Page: "input", Label: "听写快捷键", Kind: Shortcut, Stacked: true},
		{ID: "capture_dictation", Page: "input", Label: "录入按键", Kind: Button},
		{ID: "microphone", Page: "input", Label: "麦克风", Kind: Choice, Stacked: true},
		{ID: "record_mode", Page: "input", Label: "录音模式", Kind: Choice, Platform: MacOS, Gap: "#138"},
		{ID: "exit_shortcut", Page: "input", Label: "全局退出快捷键（不是语音操作）", Kind: Shortcut, Platform: MacOS, Gap: "#139"},
		{ID: "capture_exit", Page: "input", Label: "录入按键", Kind: Button, Platform: MacOS, Gap: "#139"},
		{ID: "max_record_seconds", Page: "input", Label: "最长录音", Kind: Stepper, Stacked: true, Gap: "#61 #69"},
		{ID: "record_note", Page: "input", Label: "录音数据为 16 kHz 单声道 PCM16，最长 180 秒。", Kind: Note, Platform: MacOS, Gap: "#61 #69"},
		{ID: "meeting_asr_mode", Page: "input", Label: "会议语音识别方式", Kind: Choice, Platform: MacOS, Gap: "#61 #69"},
		{ID: "system_prompt", Page: "input", Label: "System Prompt", Kind: Text, Platform: MacOS, Gap: "#145"},
		{ID: "max_response_tokens", Page: "input", Label: "响应 Token 上限", Kind: Stepper, Platform: MacOS, Gap: "#146"},
		{ID: "max_selected_characters", Page: "input", Label: "选区字符上限", Kind: Stepper, Platform: MacOS, Gap: "#147"},
		{ID: "selection_copy_attempts", Page: "input", Label: "选区复制重试次数（Windows 专用）", Kind: Note, Platform: MacOS, Gap: "#148"},
		{ID: "selection_copy_timeout_ms", Page: "input", Label: "选区复制超时由 Windows 剪贴板适配器使用；macOS 使用 AX 和原生粘贴板时序。", Kind: Note, Platform: MacOS, Gap: "#148"},
		{ID: "input_note_windows", Page: "input", Label: "语音指令用于改写/解释/问答；更换按键或设备后需重启软件。", Kind: Note, Platform: Windows, Gap: "#61 #69"},
		{ID: "input_note_macos", Page: "input", Label: "快捷键可直接录入左/右 Control、左/右 Option、左/右 Command 六个单键；单独按住 200ms 开始、松开结束，正常组合键不被拦截。Mouse4 听写、Mouse5 改写保留，亦可配置 Ctrl+Option 组合键。默认右 Command 听写、右 Option 改写，MacBook Pro/Air 内建键盘均可使用。录入时能区分左/右修饰键；外接键盘仍可自定义右 Control。退出快捷键必须使用功能键或组合键。", Kind: Note, Platform: MacOS, Gap: "#61 #69"},

		{ID: "save", Page: "save", Label: "保存设置", Kind: Button},
		{ID: "advanced", Page: "save", Label: "高级配置", Kind: Button},
		{ID: "close", Page: "save", Label: "关闭", Kind: Button},
		{ID: "save_note", Page: "save", Label: "API Key 为可选项；需要认证时先保存再检测。连接检测共用 Windows 的 Go Core 规则：ASR 发送生成的静音 WAV，LLM 查询 /models，不发送用户录音或文本。密钥仅保存在 Keychain。", Kind: Note, Platform: MacOS, Gap: "#61 #69"},
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
