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
                Text("API 基础 URL 和模型名称仅在当前运行期间保存在内存中。桥接连接检查不使用这些字段。")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                Text("此页面不请求麦克风或辅助功能权限。使用相应的系统功能前，需要在系统设置中授权。")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
    }
}
