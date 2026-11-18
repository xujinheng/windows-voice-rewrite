import AppKit
import SwiftUI
import VoiceRewriteStorage

struct MainView: View {
    @ObservedObject var model: WorkspaceModel
    @State private var selectedTab = 0
    @State private var showInlineResults = false

    var body: some View {
        TabView(selection: $selectedTab) {
            dashboard
                .tabItem { Label("工作台", systemImage: "waveform") }
                .tag(0)
            MeetingView(model: model)
                .tabItem { Label("会议", systemImage: "person.2.wave.2") }
                .tag(1)
            SettingsView(model: model, onClose: { selectedTab = 0 })
                .tabItem { Label("设置", systemImage: "gearshape") }
                .tag(2)
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
                        if model.microphonePermission == .denied {
                            HStack(alignment: .firstTextBaseline, spacing: 10) {
                                Text("macOS 已拒绝麦克风访问；请在系统设置中允许 VoiceRewrite，然后退出并重新打开应用。")
                                    .font(.footnote)
                                    .foregroundStyle(.orange)
                                Spacer()
                                Button("打开麦克风权限设置") { model.openMicrophonePrivacySettings() }
                            }
                        } else if model.microphonePermission == .restricted {
                            Text("麦克风访问受到 macOS 或设备管理策略限制。")
                                .font(.footnote)
                                .foregroundStyle(.orange)
                        }
                        LabeledContent("原生交互", value: model.interactionStatus)
                        LabeledContent("鼠标侧键", value: model.sideMouseStatus)
                        Text(model.updateStatus).font(.footnote).foregroundStyle(.secondary).textSelection(.enabled)
                        HStack {
                            Button("检查更新") { model.checkForUpdate() }
                                .disabled(model.isCheckingUpdate)
                            if model.updatePage != nil {
                                Button("打开发布页面") { model.openUpdatePage() }
                            }
                            Button(ResultCopy.viewDiagnosticLog) { DiagnosticLog.reveal() }
                            Spacer()
                        }
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
                    Button(model.voicePaused ? VoicePauseCopy.resume : VoicePauseCopy.pause) {
                        model.toggleVoicePause()
                    }
                    Spacer()
                    Button("请求辅助功能权限") { model.requestAccessibility() }
                    Button("重新检测鼠标侧键") { model.refreshSideMouse() }
                }

                DisclosureGroup("本次语音结果（可选详情）", isExpanded: $showInlineResults) {
                    GroupBox("语音识别结果") {
                        resultView(model.transcription, empty: "录音并调用用户配置的 ASR 服务后显示真实结果。")
                    }
                }

                GroupBox("选区改写") {
                    VStack(alignment: .leading, spacing: 8) {
                        TextField("输入改写/翻译指令，再切到其他应用选中文字", text: $model.taskInstruction)
                        Toggle("使用文字指令改写（无需录音）", isOn: $model.useTypedRewriteInstruction)
                            .accessibilityIdentifier("rewrite_typed_mode")
                        Text("在目标应用选中文字后，按住 \(MacShortcut.display(model.settings.rewriteShortcut)) 或 Mouse5；开启文字指令模式则执行上方填写的指令。")
                            .font(.footnote).foregroundStyle(.secondary)
                        Text("侧键默认：Mouse4 听写、Mouse5 选区改写；在设置中调整后按新绑定执行。短按侧键保留正常导航。")
                            .font(.footnote).foregroundStyle(.secondary)
                        Text("当前听写：\(MacShortcut.display(model.settings.dictationShortcut))；当前改写：\(MacShortcut.display(model.settings.rewriteShortcut))。按住说话，松开结束；Mouse4/Mouse5 仍可使用。")
                            .font(.footnote).foregroundStyle(.secondary)
                        Text("语音选区模式需要先在目标应用选中文字；录音中不得改变目标、焦点或选区，否则只保留预览。")
                            .font(.footnote).foregroundStyle(.secondary)
                        if showInlineResults {
                            resultView(model.processedText, empty: "这里显示真实模型结果；未经授权不会更改其他应用文本。")
                        }
                    }
                }

                GroupBox(SessionHistory.sectionTitle) {
                    VStack(alignment: .leading, spacing: 8) {
                        if model.sessionHistory.isEmpty {
                            Text(SessionHistory.emptySession)
                                .foregroundStyle(.secondary)
                        } else {
                            ForEach(Array(model.sessionHistory.enumerated()), id: \.element.id) { index, entry in
                                Button(SessionHistory.menuLine(index: index, title: entry.title, createdAt: entry.createdAt)) {
                                    model.showSessionHistory(id: entry.id)
                                }
                                .buttonStyle(.plain)
                            }
                            if let restoredHistoryText = model.restoredHistoryText, !restoredHistoryText.isEmpty {
                                resultView(restoredHistoryText, empty: "")
                            }
                            Button(SessionHistory.clearSession) { model.clearSessionHistory() }
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(8)
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
                VStack(alignment: .trailing, spacing: 6) {
                    Button {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(value, forType: .string)
                    } label: {
                        Label(ResultCopy.copyAction, systemImage: "doc.on.doc")
                    }
                    .accessibilityLabel("复制文本结果")
                    ScrollView {
                        Text(value).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            } else {
                Text(empty).foregroundStyle(.secondary).frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .frame(minHeight: resultHeight(value), alignment: .topLeading)
        .padding(8)
    }

    private func resultHeight(_ value: String?) -> CGFloat {
        guard let value, !value.isEmpty else { return 70 }
        return ResultCardMetrics.height(unicodeScalars: value.unicodeScalars.count)
    }
}

private struct MeetingView: View {
    @ObservedObject var model: WorkspaceModel

    var body: some View {
        ScrollView([.horizontal, .vertical]) {
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text("会议助手").font(.title2.bold())
                    Spacer()
                    Button("打开独立阅读窗口") {
                        MeetingReaderWindowController.shared.open(model: model)
                    }
                    .accessibilityIdentifier("meeting_open_reader")
                    Button("刷新音频来源") { model.refreshMeetingSources() }
                        .disabled(model.meetingStarting || model.meetingActive)
                        .accessibilityIdentifier("meeting_refresh_sources")
                }
                HStack(spacing: 12) {
                    Picker("音频来源", selection: $model.meetingSourceID) {
                        ForEach(model.meetingSources) { source in
                            Text(source.name).tag(source.id)
                        }
                    }
                    .disabled(model.meetingStarting || model.meetingActive)
                    .accessibilityIdentifier("meeting_audio_source")
                    Picker("会议识别模式", selection: $model.settings.meetingASRMode) {
                        Text(ContractLimits.meetingASRMultipartTitle)
                            .tag(ContractLimits.meetingASRMultipart)
                        Text(ContractLimits.meetingASRLlamaPrefixTitle)
                            .tag(ContractLimits.meetingASRLlamaPrefix)
                    }
                    .disabled(model.meetingStarting || model.meetingActive)
                    .accessibilityIdentifier("meeting_asr_mode")
                    Button(model.meetingDemoActive ? "退出演示" : (model.meetingActive || model.meetingStarting ? "停止会议" : "开始会议")) {
                        if model.meetingActive || model.meetingStarting {
                            model.stopMeeting()
                        } else {
                            model.startMeeting()
                        }
                    }
                    .disabled(!model.meetingActive && !model.meetingStarting &&
                              !model.meetingSources.contains(where: { $0.id == model.meetingSourceID }))
                    .accessibilityIdentifier("meeting_start_stop")
                    Button("离线演示") { model.startMeetingDemo() }
                        .disabled(model.meetingStarting || model.meetingActive)
                        .accessibilityIdentifier("meeting_offline_demo")
                }
                Text("会议资料中可在运行时更换识别模式。离线演示只显示固定测试数据，不采集音频，不连接在线模型服务。")
                    .font(.caption).foregroundStyle(.secondary)
                MeetingReaderView(model: model, embedded: true)
                    .frame(height: 690)
            }
            .padding(.horizontal, 4)
        }
        .onAppear { model.refreshMeetingSources() }
    }
}
