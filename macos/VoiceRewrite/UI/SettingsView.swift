import SwiftUI

struct SettingsView: View {
    @ObservedObject var model: WorkspaceModel

    var body: some View {
        Form {
            Section("语音识别（ASR）") {
                TextField("ASR 基础 URL", text: $model.settings.asrBaseURL)
                    .accessibilityLabel("ASR 基础 URL")
                TextField("ASR 模型", text: $model.settings.asrModel)
                SecureField("ASR API Key（本地服务可留空；留空保留旧值）", text: $model.asrKeyInput)
                Button("测试 ASR 连接") { model.checkASRService() }
                    .disabled(model.isCheckingServices)
                Text(model.asrProbeStatus).font(.footnote).foregroundStyle(.secondary)
                    .textSelection(.enabled)
            }
            Section("文本模型（LLM）") {
                TextField("LLM 基础 URL", text: $model.settings.llmBaseURL)
                    .accessibilityLabel("LLM 基础 URL")
                TextField("LLM 模型", text: $model.settings.textModel)
                SecureField("LLM API Key（本地服务可留空；留空保留旧值）", text: $model.llmKeyInput)
                Button("测试 LLM 连接") { model.checkLLMService() }
                    .disabled(model.isCheckingServices)
                Text(model.llmProbeStatus).font(.footnote).foregroundStyle(.secondary)
                    .textSelection(.enabled)
            }
            Section("录音配置") {
                Stepper("最长录音：\(model.settings.maxRecordSeconds) 秒",
                        value: $model.settings.maxRecordSeconds, in: MacSettings.recordSeconds)
                Text("录音数据为 16 kHz 单声道 PCM16，最长 \(MacSettings.recordSeconds.upperBound) 秒。")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Section("键盘快捷键") {
                TextField("听写", text: $model.settings.dictationShortcut)
                TextField("语音修改选区", text: $model.settings.rewriteShortcut)
                TextField("文字指令", text: $model.settings.textEditShortcut)
                Text("与 Windows 使用同一套写法，例如 Ctrl+Alt+R 或 F8。Alt 在 macOS 上是 Option。Mouse4 和 Mouse5 仍由侧键处理。")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Section("安全保存") {
                Button("保存设置和 Keychain 凭据") { model.saveSettings() }
                Text(model.settingsStatus).foregroundStyle(.secondary)
                    .font(.footnote).textSelection(.enabled)
                Text("API Key 为可选项；需要认证时先保存再检测。连接检测共用 Windows 的 Go Core 规则：ASR 发送生成的静音 WAV，LLM 查询 /models，不发送用户录音或文本。密钥仅保存在 Keychain。")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
    }
}
