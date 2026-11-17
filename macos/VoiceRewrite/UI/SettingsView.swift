// Source: voice-rewrite/internal/contract
import SwiftUI
import VoiceRewriteStorage

struct SettingsView: View {
    @ObservedObject var model: WorkspaceModel

    var body: some View {
        Form {
            Section("语音识别") {
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
            Section("AI 写作") {
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
            Section("快捷键与设备") {
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
                   !model.microphoneDevices.contains(where: { $0.id == model.settings.microphoneUID }) {
                    Text("已保存的麦克风不在当前设备列表中，录音会提示设备不可用。")
                        .font(.footnote).foregroundStyle(.orange)
                }
                Stepper("最长录音：\(model.settings.maxRecordSeconds) 秒",
                        value: $model.settings.maxRecordSeconds, in: MacSettings.recordSeconds)
                    .accessibilityIdentifier("max_record_seconds")
                Text("录音数据为 16 kHz 单声道 PCM16，最长 180 秒。")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("record_note")
                TextField("语音指令", text: $model.settings.rewriteShortcut)
                    .accessibilityIdentifier("rewrite_shortcut")
                TextField("听写快捷键", text: $model.settings.dictationShortcut)
                    .accessibilityIdentifier("dictation_shortcut")
                TextField("文字指令", text: $model.settings.textEditShortcut)
                    .accessibilityIdentifier("text_edit_shortcut")
                Text("与 Windows 使用同一套写法，例如 Ctrl+Alt+R、F8 或 Mouse4。Alt 在 macOS 上是 Option。未配置鼠标绑定时，Mouse4 默认听写、Mouse5 默认改写；显式配置鼠标键时仅拦截已分配的侧键。F21–F24 在 macOS 上不可用，三个功能不能绑定同一个按键。")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("input_note_macos")
            }
            Section("安全保存") {
                Button("保存设置") { model.saveSettings() }
                    .accessibilityIdentifier("save")
                Text(model.settingsStatus).foregroundStyle(.secondary)
                    .font(.footnote).textSelection(.enabled)
                Text("API Key 为可选项；需要认证时先保存再检测。连接检测共用 Windows 的 Go Core 规则：ASR 发送生成的静音 WAV，LLM 查询 /models，不发送用户录音或文本。密钥仅保存在 Keychain。")
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("save_note")
            }
        }
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
        .formStyle(.grouped)
    }
}
