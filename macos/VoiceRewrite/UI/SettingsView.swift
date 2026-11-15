import SwiftUI

struct SettingsView: View {
    @ObservedObject var model: WorkspaceModel

    var body: some View {
        Form {
            Section("语音识别（ASR）") {
                TextField("ASR URL", text: $model.asrURL)
                    .accessibilityLabel("ASR 服务 URL")
            }
            Section("文本模型（LLM）") {
                TextField("LLM URL", text: $model.llmURL)
                    .accessibilityLabel("LLM 服务 URL")
                TextField("模型名称", text: $model.modelName)
                    .accessibilityLabel("LLM 模型名称")
            }
            Section("集成状态") {
                Text("这些值可以编辑，但目前只保存在内存中。服务调用、持久化和 Keychain 将由后续集成接入；当前不会把设置发送给 Helper。")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                Text("录音和全局快捷键需要系统麦克风、辅助功能等权限；此版本不会主动请求未使用的权限。")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
    }
}
