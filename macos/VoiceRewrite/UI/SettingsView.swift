// Source: voice-rewrite/internal/contract
import SwiftUI
import AppKit
import VoiceRewriteStorage
import Microphone

struct SettingsView: View {
    @ObservedObject var model: WorkspaceModel
    @State private var selectedPage = "speech"
    @State private var showAdvanced = false
    @State private var captureTarget: String?
    @State private var captureStatus = ""
    @State private var captureMonitor: Any?

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
                                Text("录入语音指令、听写快捷键，并选择麦克风。").font(.subheadline).foregroundStyle(.secondary)
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
                TextField("API 地址", text: $model.settings.asrBaseURL)
                    .accessibilityIdentifier("asr_base_url")
                TextField("模型 ID", text: $model.settings.asrModel)
                    .accessibilityIdentifier("asr_model")
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
                SecureField("ASR API Key（本地服务可留空；留空保留旧值）", text: $model.asrKeyInput)
                    .accessibilityLabel("API Key")
                    .accessibilityIdentifier("asr_api_key")
                Button("测试连接") { model.checkASRService() }
                    .disabled(model.isCheckingServices)
                    .accessibilityIdentifier("probe_asr")
                Text(model.asrProbeStatus).font(.footnote).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .accessibilityIdentifier("probe_asr_status")
                            } 
                            else if selectedPage == "writing" {
                                Text("模型配置").font(.headline)
                TextField("API 地址", text: $model.settings.llmBaseURL)
                    .accessibilityIdentifier("llm_base_url")
                TextField("模型 ID", text: $model.settings.textModel)
                    .accessibilityIdentifier("text_model")
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
                SecureField("LLM API Key（本地服务可留空；留空保留旧值）", text: $model.llmKeyInput)
                    .accessibilityLabel("API Key")
                    .accessibilityIdentifier("llm_api_key")
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
                TextField("语音指令", text: $model.settings.rewriteShortcut)
                    .accessibilityIdentifier("rewrite_shortcut")
                Button(captureTarget == "rewrite_shortcut" ? "等待按键…" : "录入按键") { beginCapture("rewrite_shortcut") }
                    .accessibilityIdentifier("capture_rewrite")
                                }
                                HStack(spacing: 12) {
                TextField("听写快捷键", text: $model.settings.dictationShortcut)
                    .accessibilityIdentifier("dictation_shortcut")
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
                Text("可用 Ctrl+Alt+R、F8 或 Mouse4。macOS 的 Alt 即 Option；Mouse4 默认听写，Mouse5 默认改写。仅监听已绑定侧键，F21–F24 不可用；听写与改写不能绑定同一个按键。")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("input_note_macos")
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
                Button("关闭") { NSApp.keyWindow?.close() }
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
        .frame(minWidth: 540, minHeight: 480)
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
        .onAppear { model.refreshMicrophoneDevices() }
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
        captureMonitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown, .otherMouseDown]) { event in
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
                captureStatus = "请使用 Ctrl/Option 加字母数字，或 F1–F20"
                return nil
            }
            acceptCapture(candidate)
            return nil
        }
    }

    private func acceptCapture(_ candidate: String) {
        let other = captureTarget == "dictation_shortcut"
            ? model.settings.rewriteShortcut : model.settings.dictationShortcut
        if candidate.caseInsensitiveCompare(other) == .orderedSame {
            captureStatus = "听写与语音指令不能绑定同一个按键"
            return
        }
        switch captureTarget {
        case "dictation_shortcut": model.settings.dictationShortcut = candidate
        case "rewrite_shortcut": model.settings.rewriteShortcut = candidate
        default: return
        }
        stopCapture()
        captureStatus = "已录入 \(candidate)，保存设置后生效"
    }

    private func stopCapture() {
        if let captureMonitor { NSEvent.removeMonitor(captureMonitor) }
        captureMonitor = nil
        captureTarget = nil
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
