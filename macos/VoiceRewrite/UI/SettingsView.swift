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
            }
            Section("文本模型（LLM）") {
                TextField("LLM 基础 URL", text: $model.settings.llmBaseURL)
                    .accessibilityLabel("LLM 基础 URL")
                TextField("LLM 模型", text: $model.settings.textModel)
                SecureField("LLM API Key（本地服务可留空；留空保留旧值）", text: $model.llmKeyInput)
            }
            Section("录音配置") {
                Stepper("最长录音：\(model.settings.maxRecordSeconds) 秒",
                        value: $model.settings.maxRecordSeconds, in: 1...60)
                Text("录音数据为 16 kHz 单声道 PCM16；最长 60 秒。")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Section("安全保存") {
                Button("保存设置和 Keychain 凭据") { model.saveSettings() }
                Text(model.settingsStatus).foregroundStyle(.secondary)
                    .font(.footnote).textSelection(.enabled)
                Text("API Key 为可选项；仅有需要认证的服务才需填写。密钥保存在 Keychain，单次任务仅通过私有 Pipe 传递所需密钥。")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
    }
}
