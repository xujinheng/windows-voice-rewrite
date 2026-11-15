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
            VStack(alignment: .leading, spacing: 17) {
                HStack {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("VoiceRewrite").font(.largeTitle.bold())
                        Text("macOS 原生语音识别与选区改写")
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("连接服务") { model.connect() }
                }

                GroupBox("运行状态") {
                    VStack(alignment: .leading, spacing: 8) {
                        LabeledContent("Go Helper") {
                            switch model.connection {
                            case .disconnected: Text("未连接")
                            case .connecting: Text("正在连接")
                            case .connected(let value): Text(value)
                            case .failed(let reason): Text(reason).foregroundStyle(.red)
                            }
                        }
                        LabeledContent("Helper 状态", value: model.serviceStatus)
                        LabeledContent("ASR 服务", value: model.asrProbeStatus)
                        LabeledContent("LLM 服务", value: model.llmProbeStatus)
                        HStack {
                            Spacer()
                            Button("重新检测 ASR / LLM") { model.checkServices() }
                                .disabled(model.isCheckingServices)
                        }
                        LabeledContent("麦克风", value: model.recordingStatus)
                        LabeledContent("原生交互", value: model.interactionStatus)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }

                HStack(spacing: 12) {
                    Button(model.isRecording ? "停止录音并识别" : "开始麦克风录音") {
                        model.toggleRecording()
                    }
                    .disabled(model.isStarting || model.isProcessing)
                    Button("取消任务") { model.cancelCurrent() }
                        .disabled(!model.isRecording && !model.isStarting && !model.isProcessing)
                    Spacer()
                    Button("请求辅助功能权限") { model.requestAccessibility() }
                }

                GroupBox("语音识别结果") {
                    resultView(model.transcription, empty: "录音并调用用户配置的 ASR 服务后显示真实结果。")
                }

                GroupBox("选区改写") {
                    VStack(alignment: .leading, spacing: 8) {
                        TextField("输入改写/翻译指令，再切到其他应用选中文字", text: $model.taskInstruction)
                        Text("Ctrl+Option+E：选中文字后开始语音修改，再按一次结束；Go Core 识别指令并决定改写、翻译或预览。Ctrl+Option+T：使用上方文字指令处理选区。Ctrl+Option+R：普通听写。")
                            .font(.footnote).foregroundStyle(.secondary)
                        Text("语音选区模式需要先在目标应用选中文字；录音中不得改变目标、焦点或选区，否则只保留预览。")
                            .font(.footnote).foregroundStyle(.secondary)
                        resultView(model.processedText, empty: "这里显示真实模型结果；未经授权不会更改其他应用文本。")
                    }
                }
                Text("更改目标应用或选区会阻止旧任务写入。真实麦克风、Keychain 和辅助功能权限需在本机授权。")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func resultView(_ value: String?, empty: String) -> some View {
        Group {
            if let value, !value.isEmpty {
                ScrollView {
                    Text(value).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                }
            } else {
                Text(empty).foregroundStyle(.secondary).frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .frame(minHeight: 70, alignment: .topLeading)
        .padding(8)
    }
}
