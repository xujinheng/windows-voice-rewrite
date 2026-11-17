// Source: voice-rewrite/internal/contract and voice-rewrite/internal/core/settingsform
import SwiftUI
import AppKit
import VoiceRewriteStorage
import VoiceRewriteInteraction
import Microphone

enum SettingsWindowMetrics {
    static let width: CGFloat = 860
    static let height: CGFloat = 530
    static let minimumSize = NSSize(width: 480, height: 420)
}

struct SettingsView: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: WorkspaceModel
    var onClose: (() -> Void)? = nil
    @State private var selectedPage = "speech"
    @State private var showAdvanced = false
    @State private var captureTarget: String?
    @State private var captureStatus = ""
    @State private var captureMonitor: Any?

    init(model: WorkspaceModel, onClose: (() -> Void)? = nil, initialPage: String = "speech") {
        self.model = model
        self.onClose = onClose
        self._selectedPage = State(initialValue: ["speech", "writing", "input"].contains(initialPage) ? initialPage : "speech")
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                VStack(alignment: .leading, spacing: 12) {
                    Text("VoiceRewrite").font(.title3.bold())
                    Text("设置分类").font(.caption).foregroundStyle(.secondary)
                    Button("语音识别") { selectPage("speech") }
                        .buttonStyle(.plain)
                        .font(.system(size: 13, weight: selectedPage == "speech" ? .semibold : .regular))
                        .padding(.vertical, 9)
                        .padding(.horizontal, 12)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(selectedPage == "speech" ? Color.accentColor.opacity(0.12) : Color.clear)
                        .clipShape(RoundedRectangle(cornerRadius: 9))
                        .accessibilityIdentifier("nav_speech")
                    Button("AI 写作") { selectPage("writing") }
                        .buttonStyle(.plain)
                        .font(.system(size: 13, weight: selectedPage == "writing" ? .semibold : .regular))
                        .padding(.vertical, 9)
                        .padding(.horizontal, 12)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(selectedPage == "writing" ? Color.accentColor.opacity(0.12) : Color.clear)
                        .clipShape(RoundedRectangle(cornerRadius: 9))
                        .accessibilityIdentifier("nav_writing")
                    Button("快捷键与设备") { selectPage("input") }
                        .buttonStyle(.plain)
                        .font(.system(size: 13, weight: selectedPage == "input" ? .semibold : .regular))
                        .padding(.vertical, 9)
                        .padding(.horizontal, 12)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(selectedPage == "input" ? Color.accentColor.opacity(0.12) : Color.clear)
                        .clipShape(RoundedRectangle(cornerRadius: 9))
                        .accessibilityIdentifier("nav_input")
                    Spacer(minLength: 0)
                }
                .frame(width: 160, alignment: .leading)
                .padding(16)
                .frame(maxHeight: .infinity)
                .background(Color(nsColor: .controlBackgroundColor))
                Divider()
                VStack(alignment: .leading, spacing: 0) {
                    Group {
                        if selectedPage == "speech" {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("语音识别").font(.title2.bold())
                                Text("连接语音识别服务，将录音转换为文字。").font(.subheadline).foregroundStyle(.secondary)
                            }
                        } 
                        else if selectedPage == "writing" {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("AI 写作").font(.title2.bold())
                                Text("配置用于改写、翻译与问答的文本模型。").font(.subheadline).foregroundStyle(.secondary)
                            }
                        } 
                        else if selectedPage == "input" {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("快捷键与设备").font(.title2.bold())
                                Text("设置语音快捷键、录音模式和麦克风。").font(.subheadline).foregroundStyle(.secondary)
                            }
                        } 
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 24)
                    .padding(.top, 22)
                    .padding(.bottom, 14)
                    Divider()
                    ScrollView {
                        VStack(alignment: .leading, spacing: 14) {
                            if selectedPage == "speech" {
                                Text("连接配置").font(.headline)
                HStack(spacing: 12) {
                    Text("API 地址")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    TextField("API 地址", text: $model.settings.asrBaseURL)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("asr_base_url")
                }
                HStack(spacing: 12) {
                    Text("模型 ID")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    TextField("模型 ID", text: $model.settings.asrModel)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("asr_model")
                }
                HStack {
                    Button("发现模型") { model.discoverModels(for: .transcribe) }
                        .disabled(model.isDiscoveringASRModels)
                        .accessibilityIdentifier("discover_asr_models")
                    Menu("选择发现的模型") {
                        ForEach(model.asrAvailableModels, id: \.self) { id in
                            Button(id) { model.settings.asrModel = id }
                        }
                    }
                    .disabled(model.asrAvailableModels.isEmpty)
                    .accessibilityIdentifier("select_asr_model")
                }
                if !model.asrModelDiscoveryStatus.isEmpty {
                    Text(model.asrModelDiscoveryStatus).font(.footnote).foregroundStyle(.secondary)
                        .accessibilityIdentifier("asr_models_status")
                }
                HStack(spacing: 12) {
                    Text("API Key")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    SecureField("ASR API Key（本地服务可留空；留空保留旧值）", text: $model.asrKeyInput)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityLabel("API Key")
                        .accessibilityIdentifier("asr_api_key")
                }
                Button("测试连接") { model.checkASRService() }
                    .disabled(model.isCheckingServices)
                    .accessibilityIdentifier("probe_asr")
                Text(model.asrProbeStatus).font(.footnote).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .accessibilityIdentifier("probe_asr_status")
                            } 
                            else if selectedPage == "writing" {
                                Text("模型配置").font(.headline)
                HStack(spacing: 12) {
                    Text("API 地址")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    TextField("API 地址", text: $model.settings.llmBaseURL)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("llm_base_url")
                }
                HStack(spacing: 12) {
                    Text("模型 ID")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    TextField("模型 ID", text: $model.settings.textModel)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("text_model")
                }
                HStack {
                    Button("发现模型") { model.discoverModels(for: .selectedTask) }
                        .disabled(model.isDiscoveringLLMModels)
                        .accessibilityIdentifier("discover_llm_models")
                    Menu("选择发现的模型") {
                        ForEach(model.llmAvailableModels, id: \.self) { id in
                            Button(id) { model.settings.textModel = id }
                        }
                    }
                    .disabled(model.llmAvailableModels.isEmpty)
                    .accessibilityIdentifier("select_llm_model")
                }
                if !model.llmModelDiscoveryStatus.isEmpty {
                    Text(model.llmModelDiscoveryStatus).font(.footnote).foregroundStyle(.secondary)
                        .accessibilityIdentifier("llm_models_status")
                }
                HStack(spacing: 12) {
                    Text("API Key")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    SecureField("LLM API Key（本地服务可留空；留空保留旧值）", text: $model.llmKeyInput)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityLabel("API Key")
                        .accessibilityIdentifier("llm_api_key")
                }
                Button("测试连接") { model.checkLLMService() }
                    .disabled(model.isCheckingServices)
                    .accessibilityIdentifier("probe_llm")
                Text(model.llmProbeStatus).font(.footnote).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .accessibilityIdentifier("probe_llm_status")
                            } 
                            else if selectedPage == "input" {
                                Text("输入偏好").font(.headline)
                                HStack(spacing: 12) {
                HStack(spacing: 12) {
                    Text("语音指令")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    TextField("语音指令", text: $model.settings.rewriteShortcut)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("rewrite_shortcut")
                }
                Button(captureTarget == "rewrite_shortcut" ? "等待按键…" : "录入按键") { beginCapture("rewrite_shortcut") }
                    .accessibilityIdentifier("capture_rewrite")
                                }
                                HStack(spacing: 12) {
                HStack(spacing: 12) {
                    Text("听写快捷键")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    TextField("听写快捷键", text: $model.settings.dictationShortcut)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("dictation_shortcut")
                }
                Button(captureTarget == "dictation_shortcut" ? "等待按键…" : "录入按键") { beginCapture("dictation_shortcut") }
                    .accessibilityIdentifier("capture_dictation")
                                }
                HStack {
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
                Picker("录音模式", selection: $model.settings.recordMode) {
                    Text("按住录音（Hold）").tag("hold")
                    Text("再按停止（Toggle）").tag("toggle")
                }
                .accessibilityIdentifier("record_mode")
                                HStack(spacing: 12) {
                HStack(spacing: 12) {
                    Text("全局退出快捷键（不是语音操作）")
                        .font(.system(size: 13))
                        .frame(width: 112, alignment: .leading)
                    TextField("全局退出快捷键（不是语音操作）", text: $model.settings.exitShortcut)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("exit_shortcut")
                }
                Button(captureTarget == "exit_shortcut" ? "等待按键…" : "录入按键") { beginCapture("exit_shortcut") }
                    .accessibilityIdentifier("capture_exit")
                                }
                Text("快捷键可直接录入左/右 Control、左/右 Option、左/右 Command 六个单键；单独按住 250ms 开始、松开结束，正常组合键不被拦截。Mouse4 听写、Mouse5 改写保留，亦可配置 Ctrl+Option 组合键。默认右 Control 听写、右 Option 改写；MacBook Pro/Air 内建键盘没有右 Control，可在这里改为右 Command 或左 Control。退出快捷键必须使用功能键或组合键。")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("input_note_macos")
                                HStack {
                                    Picker("听写使用键盘", selection: $model.settings.dictationKeyboardID) {
                                        Text("所有键盘（全局快捷键）").tag("")
                                        ForEach(model.keyboardDevices) { keyboard in
                                            Text(keyboard.title).tag(keyboard.id)
                                                .disabled(!keyboard.selectable)
                                        }
                                        if !model.settings.dictationKeyboardID.isEmpty &&
                                            !model.keyboardDevices.contains(where: { $0.id == model.settings.dictationKeyboardID }) {
                                            Text("已保存的键盘当前未连接").tag(model.settings.dictationKeyboardID)
                                        }
                                    }
                                    .accessibilityIdentifier("dictation_keyboard")
                                }
                                HStack {
                                    Picker("改写使用键盘", selection: $model.settings.rewriteKeyboardID) {
                                        Text("所有键盘（全局快捷键）").tag("")
                                        ForEach(model.keyboardDevices) { keyboard in
                                            Text(keyboard.title).tag(keyboard.id)
                                                .disabled(!keyboard.selectable)
                                        }
                                        if !model.settings.rewriteKeyboardID.isEmpty &&
                                            !model.keyboardDevices.contains(where: { $0.id == model.settings.rewriteKeyboardID }) {
                                            Text("已保存的键盘当前未连接").tag(model.settings.rewriteKeyboardID)
                                        }
                                    }
                                    .accessibilityIdentifier("rewrite_keyboard")
                                    Button("刷新键盘") { model.refreshKeyboardDevices() }
                                }
                                Text("选定具体键盘时仅支持单键长按 Control / Option / Command；普通组合键和鼠标侧键继续使用“所有键盘”。设备未连接时不会自动改用其他键盘。")
                                    .font(.footnote).foregroundStyle(.secondary)
                                if !model.keyboardSourceStatus.isEmpty {
                                    Text(model.keyboardSourceStatus)
                                        .font(.footnote).foregroundStyle(.secondary)
                                        .accessibilityIdentifier("keyboard_status")
                                }
                                if !captureStatus.isEmpty {
                                    Text(captureStatus).font(.footnote).foregroundStyle(.secondary)
                                        .accessibilityIdentifier("capture_status")
                                }
                                if showAdvanced {
                                    Divider()
                                    Text("高级配置").font(.headline)
                Stepper("最长录音：\(model.settings.maxRecordSeconds) 秒",
                        value: $model.settings.maxRecordSeconds, in: MacSettings.recordSeconds)
                    .accessibilityIdentifier("max_record_seconds")
                Text("录音数据为 16 kHz 单声道 PCM16，最长 180 秒。")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("record_note")
                Picker("会议语音识别方式", selection: $model.settings.meetingASRMode) {
                    Text(ContractLimits.meetingASRMultipartTitle).tag(ContractLimits.meetingASRMultipart)
                    Text(ContractLimits.meetingASRLlamaPrefixTitle).tag(ContractLimits.meetingASRLlamaPrefix)
                }
                .accessibilityIdentifier("meeting_asr_mode")
                Text("System Prompt").font(.subheadline)
                TextEditor(text: $model.settings.systemPrompt)
                    .frame(minHeight: 96)
                    .accessibilityIdentifier("system_prompt")
                Stepper("响应 Token 上限：\(model.settings.maxResponseTokens)", value: $model.settings.maxResponseTokens, in: ContractLimits.minResponseTokens...ContractLimits.maxResponseTokens)
                    .accessibilityIdentifier("max_response_tokens")
                Stepper("选区字符上限：\(model.settings.maxSelectedRunes)", value: $model.settings.maxSelectedRunes, in: 1...ContractLimits.maxSelectedRunes)
                    .accessibilityIdentifier("max_selected_characters")
                Text("选区复制重试次数（Windows 专用）")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("selection_copy_attempts")
                Text("选区复制超时由 Windows 剪贴板适配器使用；macOS 使用 AX 和原生粘贴板时序。")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("selection_copy_timeout_ms")
                                }
                            } 
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(24)
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            Divider()
            HStack(spacing: 12) {
                Button("保存设置") { model.saveSettings() }
                    .buttonStyle(.borderedProminent)
                    .accessibilityIdentifier("save")
                Button("高级配置") {
                    selectPage("input")
                    showAdvanced.toggle()
                }
                .accessibilityIdentifier("advanced")
                Spacer()
                Button("关闭") { if let onClose { onClose() } else { dismiss() } }
                    .accessibilityIdentifier("close")
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            Text(model.settingsStatus)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 20)
            Text("API Key 为可选项；需要认证时先保存再检测。连接检测共用 Windows 的 Go Core 规则：ASR 发送生成的静音 WAV，LLM 查询 /models，不发送用户录音或文本。密钥仅保存在 Keychain。")
                .font(.footnote).foregroundStyle(.secondary)
                .accessibilityIdentifier("save_note")
                .padding(.horizontal, 20)
                .padding(.bottom, 12)
        }
        .frame(minWidth: 480, minHeight: 420)
        .background(Color(nsColor: .windowBackgroundColor))
        .onChange(of: model.settings.asrBaseURL) { _, _ in
            model.markConnectionProbeStale(.transcribe)
            model.invalidateDiscoveredModels(.transcribe)
        }
        .onChange(of: model.settings.asrModel) { _, _ in model.markConnectionProbeStale(.transcribe) }
        .onChange(of: model.asrKeyInput) { _, _ in
            model.markConnectionProbeStale(.transcribe)
            model.invalidateDiscoveredModels(.transcribe)
        }
        .onChange(of: model.settings.llmBaseURL) { _, _ in
            model.markConnectionProbeStale(.selectedTask)
            model.invalidateDiscoveredModels(.selectedTask)
        }
        .onChange(of: model.settings.textModel) { _, _ in model.markConnectionProbeStale(.selectedTask) }
        .onChange(of: model.llmKeyInput) { _, _ in
            model.markConnectionProbeStale(.selectedTask)
            model.invalidateDiscoveredModels(.selectedTask)
        }
        .onAppear { model.refreshMicrophoneDevices(); model.refreshKeyboardDevices() }
        .onDisappear { stopCapture() }
    }

    private func selectPage(_ id: String) {
        stopCapture()
        selectedPage = id
    }

    private func beginCapture(_ id: String) {
        stopCapture()
        captureTarget = id
        captureStatus = "正在监听按键… 按 Esc 取消"
        model.setShortcutCaptureActive(true)
        let selectedDevice = id == "dictation_shortcut" ? model.settings.dictationKeyboardID :
            (id == "rewrite_shortcut" ? model.settings.rewriteKeyboardID : "")
        if !selectedDevice.isEmpty {
            model.captureKeyFromSelectedKeyboard(selectedDevice) { key in acceptCapture(key.rawValue) }
        }
        captureMonitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown, .otherMouseDown, .flagsChanged]) { event in
            if event.type == .keyDown && event.isARepeat { return nil }
            if event.type == .flagsChanged {
                if captureTarget == "dictation_shortcut" && !model.settings.dictationKeyboardID.isEmpty ||
                   captureTarget == "rewrite_shortcut" && !model.settings.rewriteKeyboardID.isEmpty {
                    return event // wait for the selected HID keyboard, not a global NSEvent
                }
                guard let key = MacModifierKey(keyCode: Int64(event.keyCode)) else { return event }
                let families: NSEvent.ModifierFlags = [.control, .option, .command, .shift]
                guard event.modifierFlags.intersection(families) == key.captureFlag,
                      !CGEventSource.keyState(.combinedSessionState, key: key.otherSideKeyCode) else {
                    return event // release or multi-key chord is not a bare key
                }
                if captureTarget == "exit_shortcut" {
                    captureStatus = "退出快捷键不能使用单独的 Control/Option/Command，请选择组合键或功能键"
                    return nil
                }
                acceptCapture(key.rawValue)
                return nil
            }
            if event.type == .otherMouseDown {
                if event.buttonNumber == 3 { acceptCapture("Mouse4"); return nil }
                if event.buttonNumber == 4 { acceptCapture("Mouse5"); return nil }
                return event
            }
            if event.keyCode == 53 {
                stopCapture()
                captureStatus = "已取消快捷键录入"
                return nil
            }
            guard let candidate = Self.shortcutFromKey(event) else {
                captureStatus = "可单按左/右 Control、Option、Command，或录入 Ctrl/Option 组合键、F1–F20、Mouse4/5"
                return nil
            }
            acceptCapture(candidate)
            return nil
        }
    }

    private func acceptCapture(_ candidate: String) {
        let selectedDevice = captureTarget == "dictation_shortcut" ? model.settings.dictationKeyboardID :
            (captureTarget == "rewrite_shortcut" ? model.settings.rewriteKeyboardID : "")
        if !selectedDevice.isEmpty && MacModifierKey(canonical: candidate) == nil {
            captureStatus = "指定键盘只能录入独立的 Control、Option 或 Command；组合键请使用“所有键盘”"
            return
        }
        let other = captureTarget == "dictation_shortcut"
            ? model.settings.rewriteShortcut : model.settings.dictationShortcut
        let exit = model.settings.exitShortcut
        if captureTarget == "exit_shortcut" && (candidate == model.settings.rewriteShortcut || candidate == model.settings.dictationShortcut) {
            captureStatus = "退出键不能与语音快捷键冲突"
            return
        }
        if captureTarget == "exit_shortcut" &&
           (candidate == "Mouse4" || candidate == "Mouse5" ||
            MacModifierKey(canonical: candidate) != nil) {
            captureStatus = "退出快捷键必须使用功能键或组合键"
            return
        }
        if captureTarget != "exit_shortcut" && candidate.caseInsensitiveCompare(exit) == .orderedSame {
            captureStatus = "语音快捷键不能覆盖全局退出键"
            return
        }
        if captureTarget != "exit_shortcut" && candidate.caseInsensitiveCompare(other) == .orderedSame {
            captureStatus = "听写与语音指令不能绑定同一个按键"
            return
        }
        switch captureTarget {
        case "dictation_shortcut": model.settings.dictationShortcut = candidate
        case "rewrite_shortcut": model.settings.rewriteShortcut = candidate
        case "exit_shortcut": model.settings.exitShortcut = candidate
        default: return
        }
        stopCapture()
        captureStatus = "已录入 \(candidate)，保存设置后生效"
    }

    private func stopCapture() {
        if let captureMonitor { NSEvent.removeMonitor(captureMonitor) }
        captureMonitor = nil
        model.stopKeyboardDeviceCapture()
        captureTarget = nil
        model.setShortcutCaptureActive(false)
    }

    private static func shortcutFromKey(_ event: NSEvent) -> String? {
        let keys: [UInt16: String] = [
            122: "F1", 120: "F2", 99: "F3", 118: "F4", 96: "F5",
            97: "F6", 98: "F7", 100: "F8", 101: "F9", 109: "F10",
            103: "F11", 111: "F12", 105: "F13", 107: "F14",
            113: "F15", 106: "F16", 64: "F17", 79: "F18",
            80: "F19", 90: "F20"
        ]
        let flags = event.modifierFlags
        guard !flags.contains(.command) else { return nil }
        let key: String
        if let function = keys[event.keyCode] {
            key = function
        } else {
            let char = event.charactersIgnoringModifiers?.uppercased() ?? ""
            guard char.count == 1, let scalar = char.unicodeScalars.first,
                  (65...90).contains(scalar.value) || (48...57).contains(scalar.value) else {
                return nil
            }
            key = char
        }
        var modifiers: [String] = []
        if flags.contains(.control) { modifiers.append("Ctrl") }
        if flags.contains(.option) { modifiers.append("Alt") }
        if flags.contains(.shift) { modifiers.append("Shift") }
        if key.count == 1 && !modifiers.contains("Ctrl") && !modifiers.contains("Alt") {
            return nil
        }
        return (modifiers + [key]).joined(separator: "+")
    }
}
