import AppKit
import SwiftUI
import VoiceRewriteStorage

struct MainView: View {
    @ObservedObject var model: WorkspaceModel
    @State private var selectedTab = 0

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

                GroupBox("语音识别结果") {
                    resultView(model.transcription, empty: "录音并调用用户配置的 ASR 服务后显示真实结果。")
                }

                GroupBox("选区改写") {
                    VStack(alignment: .leading, spacing: 8) {
                        TextField("输入改写/翻译指令，再切到其他应用选中文字", text: $model.taskInstruction)
                        Text("侧键默认：Mouse4 听写、Mouse5 选区改写；在设置中调整后按新绑定执行。短按侧键保留正常导航。")
                            .font(.footnote).foregroundStyle(.secondary)
                        Text("键盘默认：Ctrl+Option+R 听写、Ctrl+Option+E 语音改写、Ctrl+Option+T 文字指令；可在设置中更改。")
                            .font(.footnote).foregroundStyle(.secondary)
                        Text("语音选区模式需要先在目标应用选中文字；录音中不得改变目标、焦点或选区，否则只保留预览。")
                            .font(.footnote).foregroundStyle(.secondary)
                        resultView(model.processedText, empty: "这里显示真实模型结果；未经授权不会更改其他应用文本。")
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
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("会议助手").font(.title2.bold())
                Spacer()
                Button("刷新音频来源") { model.refreshMeetingSources() }
                    .disabled(model.meetingStarting || model.meetingActive)
                    .accessibilityIdentifier("meeting_refresh_sources")
            }
            HStack {
                Picker("音频来源", selection: $model.meetingSourceID) {
                    ForEach(model.meetingSources) { source in
                        Text(source.name).tag(source.id)
                    }
                }
                .disabled(model.meetingStarting || model.meetingActive)
                .accessibilityIdentifier("meeting_audio_source")
                Spacer()
                if model.meetingActive || model.meetingStarting {
                    Button("停止会议") { model.stopMeeting() }
                } else {
                    Button("开始会议") { model.startMeeting() }
                        .disabled(!model.meetingSources.contains(where: {
                            $0.id == model.meetingSourceID
                        }))
                }
            }
            Picker("会议识别模式", selection: $model.settings.meetingASRMode) {
                Text(ContractLimits.meetingASRMultipartTitle)
                    .tag(ContractLimits.meetingASRMultipart)
                Text(ContractLimits.meetingASRLlamaPrefixTitle)
                    .tag(ContractLimits.meetingASRLlamaPrefix)
            }
            .disabled(model.meetingStarting || model.meetingActive)
            .accessibilityIdentifier("meeting_asr_mode")
            Text("更改识别模式后，请在设置中保存。前缀续写需要 ASR 服务支持该模式。")
                .font(.caption).foregroundStyle(.secondary)
            Text(model.meetingStatus)
                .font(.footnote).foregroundStyle(.secondary)
                .accessibilityIdentifier("meeting_status")
            HStack {
                Button(model.meetingCaptionsOnly
                    ? ContractLimits.meetingCaptionsFullTitle : ContractLimits.meetingCaptionsFocusTitle) {
                    model.toggleMeetingCaptionsOnly()
                }
                .disabled(!model.meetingActive || model.meetingBusy)
                .accessibilityIdentifier("meeting_focus_captions")
                Button(model.meetingExpressionExpanded
                    ? ContractLimits.meetingExpressionCollapseTitle : ContractLimits.meetingExpressionExpandTitle) {
                    model.toggleMeetingExpressionExpanded()
                }
                .disabled(!model.meetingActive || model.meetingBusy || model.meetingCaptionsOnly)
                .accessibilityIdentifier("meeting_expand_expression")
                Spacer()
            }
            HSplitView {
                GroupBox("实时对话与历史字幕") {
                    ScrollView {
                        VStack(alignment: .leading, spacing: 10) {
                            if let latest = model.meetingCaptions.last {
                                Text(latest.source.isEmpty ? "等待发言…" : latest.source)
                                    .font(.title3).textSelection(.enabled)
                                Divider()
                                Text(latest.chinese.isEmpty ? "正在准备中文释义…" : latest.chinese)
                                    .textSelection(.enabled)
                                if !latest.final {
                                    Text("识别中 · 原文仍可能调整")
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                            } else {
                                Text("等待所选应用发言…").foregroundStyle(.secondary)
                            }
                            Divider()
                            Text("历史字幕").font(.headline)
                            ForEach(model.meetingCaptions.filter { $0.final }) { row in
                                Text(row.source).textSelection(.enabled)
                                if !row.chinese.isEmpty {
                                    Text(row.chinese).foregroundStyle(.secondary).textSelection(.enabled)
                                }
                                Divider()
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(10)
                    }
                }
                .frame(minWidth: 210)
                if !model.meetingCaptionsOnly {
                    VSplitView {
                        if !model.meetingExpressionExpanded {
                            GroupBox("英语回复建议") {
                                VStack(alignment: .leading, spacing: 7) {
                                    HStack {
                                        Button("重新生成") { model.requestMeetingReplies() }
                                            .disabled(!model.meetingActive || model.meetingBusy || model.meetingVoiceRecording)
                                        if model.meetingReplyPending {
                                            Button("查看新建议") { model.acceptMeetingReplies() }
                                                .disabled(model.meetingBusy || model.meetingCancelPending)
                                        }
                                        Spacer()
                                        Button("取消") { model.cancelMeetingWork() }
                                            .disabled(!model.meetingActive || model.meetingCancelPending)
                                    }
                                    Text(model.meetingReplyStatus.isEmpty ? "最新完整发言生成最多三条建议" : model.meetingReplyStatus)
                                        .font(.caption).foregroundStyle(.secondary)
                                    if !model.meetingReplySource.isEmpty {
                                        Text(model.meetingReplySource).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                                    }
                                    if !model.meetingReplyTip.isEmpty {
                                        Text("提示 · " + model.meetingReplyTip)
                                            .font(.caption).foregroundStyle(.secondary)
                                            .textSelection(.enabled)
                                            .accessibilityIdentifier("meeting_reply_tip")
                                    }
                                    ScrollView {
                                        VStack(alignment: .leading, spacing: 8) {
                                            ForEach(model.meetingReplies) { reply in
                                                if !reply.english.isEmpty && reply.complete {
                                                    Button {
                                                        model.meetingSelectedIndex = reply.id
                                                    } label: {
                                                        VStack(alignment: .leading, spacing: 4) {
                                                            HStack {
                                                                Image(systemName: model.meetingSelectedIndex == reply.id
                                                                    ? "largecircle.fill.circle" : "circle")
                                                                Text(reply.english).font(.body).multilineTextAlignment(.leading)
                                                            }
                                                            Text(reply.chinese)
                                                                .font(.caption).foregroundStyle(.secondary)
                                                                .multilineTextAlignment(.leading)
                                                        }
                                                        .frame(maxWidth: .infinity, alignment: .leading)
                                                    }
                                                    .buttonStyle(.plain)
                                                }
                                            }
                                        }
                                    }
                                    Button("复制选中英文") { model.copyMeetingReply() }
                                        .disabled(!model.meetingReplies.contains(where: {
                                            $0.id == model.meetingSelectedIndex && $0.complete && !$0.english.isEmpty
                                        }))
                                    TextField("追加要求（例如：更简短、更礼貌）", text: $model.meetingRefineInput)
                                        .accessibilityIdentifier("meeting_refine_input")
                                    Button("微调选中回复") { model.refineMeetingReply() }
                                        .disabled(!model.meetingActive || model.meetingBusy ||
                                            model.meetingRefineInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                                            !model.meetingReplies.contains(where: {
                                                $0.id == model.meetingSelectedIndex && $0.complete && !$0.english.isEmpty
                                            }))
                                        .accessibilityIdentifier("meeting_refine")
                                }.padding(8)
                            }
                            .frame(minHeight: 225)
                        }
                        GroupBox("我的主动表达") {
                            VStack(alignment: .leading, spacing: 8) {
                                TextEditor(text: $model.meetingInput)
                                    .frame(minHeight: 65)
                                    .accessibilityIdentifier("meeting_expression_input")
                                HStack {
                                    Button("生成可直接说的英语") { model.composeMeetingText() }
                                        .disabled(!model.meetingActive || model.meetingBusy ||
                                            model.meetingInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                                    Button(model.meetingVoiceRecording ? "结束语音并生成" : "语音输入") {
                                        model.toggleMeetingVoice()
                                    }
                                    .disabled(!model.meetingActive || model.meetingVoiceStarting ||
                                        (model.meetingBusy && !model.meetingVoiceRecording) ||
                                        model.isRecording || model.isStarting || model.isProcessing)
                                    Spacer()
                                }
                                Text(model.meetingExpressionStatus)
                                    .font(.caption).foregroundStyle(.secondary)
                                ScrollView {
                                    VStack(alignment: .leading, spacing: 6) {
                                        Text(model.meetingExpression.isEmpty
                                            ? "可以主动提问、陈述、要数据或寻求建议，不必等待对方提问。"
                                            : model.meetingExpression)
                                            .textSelection(.enabled)
                                        if !model.meetingExpressionChinese.isEmpty {
                                            Text(model.meetingExpressionChinese)
                                                .foregroundStyle(.secondary).textSelection(.enabled)
                                        }
                                    }.frame(maxWidth: .infinity, alignment: .leading)
                                }
                                Button("复制英文表达") { model.copyMeetingExpression() }
                                    .disabled(model.meetingExpression.isEmpty)
                                    .accessibilityIdentifier("meeting_copy_expression")
                            }.padding(8)
                        }
                        .frame(minHeight: 190)
                    }
                    .frame(minWidth: 290)
                }
            }
            if !model.meetingCopyStatus.isEmpty {
                Text(model.meetingCopyStatus)
                    .font(.caption).foregroundStyle(.secondary)
                    .accessibilityIdentifier("meeting_copy_status")
            }
            Text("选择系统或应用音频后开始字幕；建议由最新完整发言触发。拖动分隔线调整区域大小。实际权限和录音效果仍需在 Mac 上验收。")
                .font(.footnote).foregroundStyle(.secondary)
        }
        .frame(minHeight: 490)
        .onAppear { model.refreshMeetingSources() }
    }
}
