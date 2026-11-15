import SwiftUI

struct MainView: View {
    @ObservedObject var model: WorkspaceModel

    var body: some View {
        TabView {
            dashboard
                .tabItem { Label("工作台", systemImage: "waveform") }
            SettingsView(model: model)
                .tabItem { Label("设置", systemImage: "gearshape") }
        }
        .padding(20)
    }

    private var dashboard: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("VoiceRewrite").font(.largeTitle.bold())
                        Text("原生 macOS 宿主 · 桥接连接状态")
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("重新连接") { model.connect() }
                        .accessibilityLabel("重新连接 Go Helper")
                }

                GroupBox("运行状态") {
                    VStack(alignment: .leading, spacing: 12) {
                        LabeledContent("Go Helper") {
                            HStack(spacing: 8) {
                                Image(systemName: statusSymbol)
                                    .foregroundStyle(statusColor)
                                Text(connectionLabel)
                                    .textSelection(.enabled)
                            }
                        }
                        LabeledContent("服务状态", value: model.serviceStatus)
                        LabeledContent("录音状态", value: model.recordingStatus)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(8)
                }

                GroupBox("识别结果") {
                    resultView(model.transcription, empty: "尚无识别结果。PCM16 录音与 transcribe 尚未接入。")
                }
                GroupBox("文本处理结果") {
                    resultView(model.processedText, empty: "尚无文本处理结果。selected_task 尚未接入。")
                }
                Text("当前版本只验证打包的 Go Helper 的真实 hello 响应；未运行的功能不会显示模拟结果。")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func resultView(_ value: String?, empty: String) -> some View {
        Group {
            if let value, !value.isEmpty {
                ScrollView { Text(value).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }
            } else {
                Text(empty).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .frame(minHeight: 85, alignment: .topLeading)
        .padding(8)
        .accessibilityElement(children: .combine)
    }

    private var connectionLabel: String {
        switch model.connection {
        case .disconnected: return "未连接"
        case .connecting: return "正在握手…"
        case .connected(let descriptor): return "已连接：\(descriptor)"
        case .failed(let detail): return "连接失败：\(detail)"
        }
    }
    private var statusSymbol: String {
        switch model.connection {
        case .connected: return "checkmark.circle.fill"
        case .failed: return "exclamationmark.triangle.fill"
        case .disconnected, .connecting: return "circle.dotted"
        }
    }
    private var statusColor: Color {
        switch model.connection {
        case .connected: return .green
        case .failed: return .red
        case .disconnected, .connecting: return .secondary
        }
    }
}
