import AppKit
import SwiftUI
import VoiceRewriteStorage

// Windows Meeting HUD: Chinese, English, history, replies, expression, source.
enum MeetingReaderLayout {
    static let minimums: [CGFloat] = [46, 32, 34, 36, 62, 28]
    static let defaults: [CGFloat] = [85, 50, 85, 65, 230, 40]

    static func visible(captionsOnly: Bool, expanded: Bool) -> [Int] {
        if captionsOnly { return [0, 1, 2] }
        return expanded ? [0, 1, 2, 4, 5] : [0, 1, 2, 3, 4, 5]
    }

    static func decode(_ value: String) -> [CGFloat] {
        let numbers = value.split(separator: ",").compactMap { Double($0) }
        guard numbers.count == 6, numbers.allSatisfy({ $0.isFinite && $0 >= 0 && $0 <= 10000 }),
              numbers.contains(where: { $0 > 0 }) else { return defaults }
        return numbers.map { CGFloat($0) }
    }

    static func encode(_ values: [CGFloat]) -> String {
        values.map { String(Int($0.rounded())) }.joined(separator: ",")
    }

    static func heights(space: CGFloat, weights: [CGFloat], visible: [Int]) -> [CGFloat] {
        var result = Array(repeating: CGFloat.zero, count: 6)
        let minimum = visible.reduce(CGFloat.zero) { $0 + minimums[$1] }
        let extra = max(0, space - minimum)
        let total = max(1, visible.reduce(CGFloat.zero) { $0 + max(0, weights[$1]) })
        for index in visible {
            result[index] = minimums[index] + extra * max(0, weights[index]) / total
        }
        return result
    }

    static func resized(weights: [CGFloat], visible: [Int],
                        after index: Int, delta: CGFloat, space: CGFloat) -> [CGFloat] {
        guard let position = visible.firstIndex(of: index), position + 1 < visible.count else {
            return weights
        }
        let next = visible[position + 1]
        var sizes = heights(space: space, weights: weights, visible: visible)
        let change = min(max(delta, minimums[index] - sizes[index]), sizes[next] - minimums[next])
        sizes[index] += change
        sizes[next] -= change
        var updated = weights
        for pane in visible {
            updated[pane] = max(0, sizes[pane] - minimums[pane])
        }
        return updated
    }

    static func unread(previous: Int, current: Int, unread: Int, following: Bool,
                       previousID: Int? = nil, currentID: Int? = nil) -> Int {
        if following { return 0 }
        if let previousID, let currentID, currentID < previousID { return 0 }
        if current < previous && currentID == nil { return 0 }
        let newTail = previousID != nil && currentID != nil && currentID! > previousID! ? 1 : 0
        return unread + max(max(0, current - previous), newTail)
    }

    // Preserve the screen with the greatest overlap, including displays
    // positioned at negative desktop coordinates. A disconnected monitor's
    // autosaved frame is instead brought back inside the primary work area.
    static func restoredFrame(_ frame: NSRect, displays: [NSRect]) -> NSRect {
        guard let area = displays.max(by: {
            $0.intersection(frame).width * $0.intersection(frame).height <
            $1.intersection(frame).width * $1.intersection(frame).height
        }) else { return frame }
        let width = min(frame.width, area.width)
        let height = min(frame.height, area.height)
        return NSRect(x: min(max(frame.minX, area.minX), area.maxX - width),
                      y: min(max(frame.minY, area.minY), area.maxY - height),
                      width: width, height: height)
    }

    static func shouldFocusExpression(_ english: String, lastFocused: String, following: Bool,
                                      frozen: Bool, active: Bool, busy: Bool) -> Bool {
        !english.isEmpty && english != lastFocused && following && !frozen && active && !busy
    }

    // A completed current turn remains fixed until the next utterance arrives.
    // Interim ASR text must never appear in the independently scrolled history.
    static func split(_ captions: [MeetingCaptionRow]) -> (current: MeetingCaptionRow?, history: [MeetingCaptionRow]) {
        let readable = captions.filter { !$0.source.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
        return (readable.last, Array(readable.dropLast().filter(\.final)))
    }
}

@MainActor
private struct ReaderSnapshot {
    let captions: [MeetingCaptionRow]
    let replies: [MeetingReplyRow]
    let replyStatus: String
    let replySource: String
    let replyTip: String
    let expression: String
    let expressionChinese: String
    let expressionSource: String
    let expressionStatus: String

    init(model: WorkspaceModel) {
        captions = model.meetingCaptions
        replies = model.meetingReplies
        replyStatus = model.meetingReplyStatus
        replySource = model.meetingReplySource
        replyTip = model.meetingReplyTip
        expression = model.meetingExpression
        expressionChinese = model.meetingExpressionChinese
        expressionSource = model.meetingExpressionSource
        expressionStatus = model.meetingExpressionStatus
    }
}

// Can be hosted by the main Meeting tab or by G/H's independent Window scene.
// No native window registration or meeting lifecycle is owned by this view.
struct MeetingReaderView: View {
    @ObservedObject var model: WorkspaceModel
    let embedded: Bool

    @AppStorage("meeting.reader.fontSize") private var fontSize = 16
    @AppStorage("meeting.reader.paneWeights") private var savedWeights = "85,50,85,65,230,40"
    @AppStorage("meeting.reader.captionsFocus") private var savedCaptionsFocus = false
    @State private var weights = MeetingReaderLayout.defaults
    @State private var dragBase: [CGFloat]?
    @State private var dragIndex: Int?
    @State private var frozen: ReaderSnapshot?
    @State private var following = true
    @State private var unread = 0
    @State private var lastFocusedExpression = ""
    @State private var showProfile = false
    @State private var showCompose = false

    private var captions: [MeetingCaptionRow] { frozen?.captions ?? model.meetingCaptions }
    private var replies: [MeetingReplyRow] { frozen?.replies ?? model.meetingReplies }
    private var latest: MeetingCaptionRow? { MeetingReaderLayout.split(captions).current }
    private var past: [MeetingCaptionRow] { MeetingReaderLayout.split(captions).history }
    private var reply: MeetingReplyRow? {
        replies.first(where: { $0.id == model.meetingSelectedIndex }) ??
        replies.first(where: { $0.complete && !$0.english.isEmpty })
    }
    private var canCopyReply: Bool { reply?.complete == true && !(reply?.english.isEmpty ?? true) }
    private var expression: String { frozen?.expression ?? model.meetingExpression }
    private var expressionChinese: String { frozen?.expressionChinese ?? model.meetingExpressionChinese }
    private var visible: [Int] {
        MeetingReaderLayout.visible(captionsOnly: model.meetingCaptionsOnly,
                                    expanded: model.meetingExpressionExpanded)
    }
    private var history: String {
        past.map { row in
            row.chinese.isEmpty ? row.source : row.chinese + "\n" + row.source
        }.joined(separator: "\n\n")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("VoiceRewrite · 会议助手")
                        .font(.headline)
                    Text(frozen == nil ? model.meetingStatus : "画面已冻结 · 后台继续处理")
                        .font(.caption)
                        .foregroundColor(model.meetingActive ? .secondary : .orange)
                        .lineLimit(2)
                        .accessibilityIdentifier("meeting_reader_status")
                }
                Spacer(minLength: 6)
                if model.meetingBusy {
                    ProgressView().controlSize(.small).accessibilityLabel("正在处理会议请求")
                }
            }

            toolbar
            HStack {
                Text("当前发言 · 中文优先").font(.caption.bold())
                Spacer()
                Label(model.meetingActive ? "正在聆听" : "等待发言",
                      systemImage: model.meetingActive ? "waveform" : "pause.circle")
                    .font(.caption).foregroundStyle(.secondary)
            }

            GeometryReader { geometry in
                let available = max(1, geometry.size.height - CGFloat(max(0, visible.count - 1)) * 10)
                let sizes = MeetingReaderLayout.heights(space: available, weights: weights, visible: visible)
                VStack(spacing: 0) {
                    ForEach(visible, id: \.self) { index in
                        pane(index)
                            .frame(height: sizes[index])
                            .frame(maxWidth: .infinity)
                            .clipped()
                        if index != visible.last {
                            divider(after: index, space: available)
                        }
                    }
                }
            }
            .frame(minHeight: 365)
            .accessibilityIdentifier("meeting_six_pane_reader")

            if !model.meetingCopyStatus.isEmpty {
                Text(model.meetingCopyStatus).font(.caption).foregroundStyle(.secondary)
                    .accessibilityIdentifier("meeting_copy_status")
            }
        }
        .padding(12)
        .frame(minWidth: embedded ? 590 : 600, minHeight: embedded ? 625 : 550)
        .onAppear { weights = MeetingReaderLayout.decode(savedWeights) }
        .onChange(of: model.meetingActive) { _, active in
            if !active {
                frozen = nil
                following = true
                unread = 0
                lastFocusedExpression = ""
            } else if savedCaptionsFocus != model.meetingCaptionsOnly {
                model.toggleMeetingCaptionsOnly()
            }
        }
        .onChange(of: model.meetingCaptionsOnly) { _, value in
            if model.meetingActive { savedCaptionsFocus = value }
        }
        .onChange(of: model.meetingExpression) { _, _ in requestExpressionFocusIfReady() }
        .onChange(of: model.meetingBusy) { _, _ in requestExpressionFocusIfReady() }
        .onChange(of: following) { _, _ in requestExpressionFocusIfReady() }
        .onChange(of: frozen == nil) { _, _ in requestExpressionFocusIfReady() }
        .sheet(isPresented: $showProfile) {
            MeetingProfileDraftView(model: model)
        }
        .sheet(isPresented: $showCompose) {
            MeetingTextComposerSheet(model: model)
        }
    }

    private func requestExpressionFocusIfReady() {
        let current = model.meetingExpression
        guard MeetingReaderLayout.shouldFocusExpression(current,
            lastFocused: lastFocusedExpression, following: following,
            frozen: frozen != nil, active: model.meetingActive, busy: model.meetingBusy) else { return }
        lastFocusedExpression = current
        model.focusMeetingExpressionIfReadingLatest()
    }

    private var toolbar: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 7) { toolbarLeading; toolbarTrailing }
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 7) { toolbarLeading }
                HStack(spacing: 7) { toolbarTrailing }
            }
        }
        .controlSize(.small)
    }

    private var toolbarLeading: some View {
        Group {
            Button(frozen == nil ? "冻结画面" : "恢复画面") {
                frozen = frozen == nil ? ReaderSnapshot(model: model) : nil
            }
            .disabled(!model.meetingActive)
            .accessibilityIdentifier("meeting_freeze")
            Button(model.meetingCaptionsOnly
                   ? "完整视图" : "专注字幕") { model.toggleMeetingCaptionsOnly() }
                .disabled(!model.meetingActive || model.meetingBusy)
                .accessibilityIdentifier("meeting_focus_captions")
            Button("停止会议") { model.stopMeeting() }
                .disabled(!model.meetingActive && !model.meetingStarting)
                .accessibilityIdentifier("meeting_reader_stop")
        }
    }

    private var toolbarTrailing: some View {
        Group {
            Button("A−") { fontSize = max(12, fontSize - 2) }
                .disabled(fontSize <= 12)
                .accessibilityLabel("缩小阅读字号")
            Button("A+") { fontSize = min(28, fontSize + 2) }
                .disabled(fontSize >= 28)
                .accessibilityLabel("放大阅读字号")
            Button(model.meetingCapturePaused ? "恢复采集" : "暂停采集") {
                model.toggleMeetingCapturePause()
            }
            .disabled(!model.meetingActive || model.meetingCaptureSwitching ||
                      model.meetingVoiceRecording || model.meetingVoiceStarting)
            .accessibilityIdentifier("meeting_capture_pause")
            Button("会议资料") { showProfile = true }
                .accessibilityIdentifier("meeting_profile")
        }
    }

    private func divider(after index: Int, space: CGFloat) -> some View {
        Rectangle()
            .fill(Color(nsColor: .separatorColor).opacity(0.6))
            .frame(height: 10)
            .overlay {
                Capsule().fill(Color.secondary.opacity(0.65))
                    .frame(width: 42, height: 2)
            }
            .contentShape(Rectangle())
            .onHover { inside in
                if inside { NSCursor.resizeUpDown.push() } else { NSCursor.pop() }
            }
            .gesture(DragGesture(minimumDistance: 1)
                .onChanged { value in
                    if dragIndex != index {
                        dragIndex = index
                        dragBase = weights
                    }
                    weights = MeetingReaderLayout.resized(
                        weights: dragBase ?? weights, visible: visible,
                        after: index, delta: value.translation.height, space: space)
                }
                .onEnded { _ in
                    savedWeights = MeetingReaderLayout.encode(weights)
                    dragIndex = nil
                    dragBase = nil
                })
            .accessibilityLabel("调整相邻阅读区域高度")
            .accessibilityIdentifier("meeting_divider_\(index)")
    }

    @ViewBuilder
    private func pane(_ index: Int) -> some View {
        switch index {
        case 0:
            section("当前中文", identifier: "meeting_pane_chinese") {
                reading(latest?.chinese.isEmpty == false ? latest!.chinese : "正在准备中文释义…",
                        size: fontSize + 6, weight: .semibold)
            }
        case 1:
            section("当前英文", identifier: "meeting_pane_english") {
                reading(latest?.source.isEmpty == false ? latest!.source : "等待所选应用发言…",
                        size: fontSize + 1)
            }
        case 2:
            section("之前的对话 · \(past.count) 段\(model.meetingHistoryTrimmed > 0 ? " · 更早 \(model.meetingHistoryTrimmed) 段已清理" : "")\(unread > 0 ? " · \(unread) 条新内容" : "")",
                    identifier: "meeting_pane_history") {
                VStack(spacing: 1) {
                    HStack {
                        Spacer()
                        Button("回到最新") {
                            following = true
                            unread = 0
                        }
                        .disabled(following && unread == 0)
                        .controlSize(.mini)
                        .accessibilityIdentifier("meeting_history_latest")
                    }
                    HistoryReaderText(text: history, completedCount: past.count,
                                      newestID: past.last?.id, fontSize: fontSize,
                                      following: $following, unread: $unread)
                        .accessibilityIdentifier("meeting_history_text")
                }
            }
        case 3:
            section("回复建议", identifier: "meeting_pane_replies") {
                repliesPane
            }
        case 4:
            section("我的英文表达", identifier: "meeting_pane_expression") {
                expressionPane
            }
        default:
            section("原话", identifier: "meeting_pane_source") {
                sourcePane
            }
        }
    }

    private func section<Content: View>(_ heading: String, identifier: String,
                                        @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(heading).font(.caption.bold()).lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
            content()
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6)
            .stroke(Color(nsColor: .separatorColor).opacity(0.5)))
        .accessibilityIdentifier(identifier)
    }

    private func reading(_ value: String, size: Int, weight: Font.Weight = .regular) -> some View {
        ScrollView {
            Text(value).font(.system(size: CGFloat(size), weight: weight))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 2)
        }
    }

    private var repliesPane: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 7) {
                Button("生成建议") { model.requestMeetingReplies() }
                    .disabled(!model.meetingActive || model.meetingBusy || frozen != nil)
                Button(model.meetingReplyPending ? "查看新建议" : "无新建议") {
                    model.acceptMeetingReplies()
                }
                .disabled(!model.meetingReplyPending || model.meetingBusy || frozen != nil)
                Button("复制回复") { model.copyMeetingReply() }
                    .disabled(!canCopyReply || frozen != nil)
                Spacer(minLength: 0)
            }
            .controlSize(.small)
            HStack(spacing: 8) {
                ForEach(0..<3, id: \.self) { index in
                    Button("\(index + 1) \(["直接回应", "澄清一下", "争取时间"][index])") {
                        model.meetingSelectedIndex = index
                    }
                    .disabled(frozen != nil || !replies.contains(where: {
                        $0.id == index && $0.complete && !$0.english.isEmpty
                    }))
                    .buttonStyle(.bordered)
                    .tint(model.meetingSelectedIndex == index ? .accentColor : .gray)
                }
            }
            .controlSize(.small)
            ScrollView {
                VStack(alignment: .leading, spacing: 5) {
                    Text(reply?.english.isEmpty == false ? reply!.english
                         : "听到完整发言后生成建议；新建议可点击“查看新建议”后阅读。")
                        .font(.system(size: CGFloat(fontSize)))
                    if let reply, !reply.chinese.isEmpty {
                        Text(reply.chinese).foregroundStyle(.secondary)
                    }
                    let tip = frozen?.replyTip ?? model.meetingReplyTip
                    if !tip.isEmpty { Text("提示 · " + tip).foregroundStyle(.secondary) }
                    let status = frozen?.replyStatus ?? model.meetingReplyStatus
                    if !status.isEmpty { Text(status).foregroundStyle(.secondary) }
                }
                .textSelection(.enabled)
            }
            HStack {
                TextField("改写要求，例如：更简短、先说明风险", text: $model.meetingRefineInput)
                    .onChange(of: model.meetingRefineInput) { _, value in
                        if value.count > 600 { model.meetingRefineInput = String(value.prefix(600)) }
                    }
                    .accessibilityIdentifier("meeting_refine_input")
                Button("改写回复") { model.refineMeetingReply() }
                    .disabled(frozen != nil || !canCopyReply || !model.meetingActive ||
                              model.meetingBusy ||
                              model.meetingRefineInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .accessibilityIdentifier("meeting_refine")
            }
            .controlSize(.small)
        }
    }

    private var expressionPane: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(frozen?.expressionStatus ?? model.meetingExpressionStatus)
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                Spacer()
                Button(model.meetingExpressionExpanded ? "收起英文" : "放大英文") {
                    model.toggleMeetingExpressionExpanded()
                }
                .disabled(!model.meetingActive || model.meetingBusy || model.meetingCaptionsOnly)
                .controlSize(.small)
                .accessibilityIdentifier("meeting_expand_expression")
                Button("复制英文") { model.copyMeetingExpression() }
                    .disabled(expression.isEmpty || frozen != nil)
                    .controlSize(.small)
                    .accessibilityIdentifier("meeting_copy_expression")
            }
            reading(expression.isEmpty
                    ? "听写键：直译自己的发言；改写键：提问、回答、补充观点。"
                    : expression, size: fontSize + 1)
            if !expressionChinese.isEmpty {
                Text(expressionChinese).font(.system(size: CGFloat(fontSize)))
                    .foregroundStyle(.secondary)
                    .lineLimit(2).textSelection(.enabled)
            }
        }
    }

    private var sourcePane: some View {
        HStack(alignment: .top, spacing: 6) {
            let original = frozen?.expressionSource ?? model.meetingExpressionSource
            let status = frozen?.expressionStatus ?? model.meetingExpressionStatus
            Text(original.isEmpty
                 ? (status.isEmpty ? "语音原话会显示在这里" : status)
                 : "已识别 · " + original)
                .font(.system(size: CGFloat(fontSize)))
                .lineLimit(2)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .help(original)
                .accessibilityIdentifier("meeting_expression_source")
            Button("文字输入") { showCompose = true }
                .disabled(!model.meetingActive || model.meetingBusy || frozen != nil)
                .accessibilityIdentifier("meeting_compose_open")
            Button(model.meetingVoiceRecording ? "结束语音" : "语音输入") {
                model.toggleMeetingVoice()
            }
            .disabled(!model.meetingActive || model.meetingVoiceStarting ||
                      (model.meetingBusy && !model.meetingVoiceRecording) ||
                      model.isRecording || model.isStarting || model.isProcessing ||
                      frozen != nil)
            Button("取消处理") { model.cancelMeetingWork() }
                .disabled(!model.meetingActive || model.meetingCancelPending || frozen != nil)
        }
        .controlSize(.mini)
    }
}

// Compose is a separate editor so the sixth reading region remains the
// original utterance, matching the Windows read-only expression-source field.
private struct MeetingTextComposerSheet: View {
    @ObservedObject var model: WorkspaceModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("生成可直接说的英语").font(.headline)
            TextEditor(text: $model.meetingInput)
                .font(.system(size: 16))
                .frame(minHeight: 150)
                .accessibilityIdentifier("meeting_expression_input")
            HStack {
                Spacer()
                Button("取消") { dismiss() }
                Button("生成英文") {
                    model.composeMeetingText()
                    dismiss()
                }
                .disabled(!model.meetingActive || model.meetingBusy ||
                    model.meetingInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .accessibilityIdentifier("meeting_compose_submit")
            }
        }
        .padding(20)
        .frame(width: 540)
    }
}

// Structured editing is serialized to the two shared Windows/Go profile
// strings. Untagged saved profiles remain editable without losing legacy text.
struct MeetingProfileFields {
    var role = ""
    var experience = ""
    var project = ""
    var goal = ""
    var constraints = ""
    var properNouns = ""
    var acronyms = ""
    var bilingualTerms = ""

    init(background: String = "", terms: String = "") {
        let b = Self.decode(background, labels: ["角色", "经历", "项目背景", "目标", "限制"],
                            legacy: 2)
        role = b[0]; experience = b[1]; project = b[2]; goal = b[3]; constraints = b[4]
        let t = Self.decode(terms, labels: ["专有名词", "缩写", "中英术语"], legacy: 2)
        properNouns = t[0]; acronyms = t[1]; bilingualTerms = t[2]
    }

    var backgroundText: String {
        Self.encode([role, experience, project, goal, constraints],
                    labels: ["角色", "经历", "项目背景", "目标", "限制"])
    }
    var termsText: String {
        Self.encode([properNouns, acronyms, bilingualTerms],
                    labels: ["专有名词", "缩写", "中英术语"])
    }

    private static func encode(_ values: [String], labels: [String]) -> String {
        zip(labels, values).compactMap { label, text -> String? in
            let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
            return trimmed.isEmpty ? nil : "【\(label)】\n\(trimmed)"
        }.joined(separator: "\n\n")
    }

    private static func decode(_ text: String, labels: [String], legacy: Int) -> [String] {
        var result = Array(repeating: "", count: labels.count)
        guard labels.contains(where: { text.hasPrefix("【\($0)】\n") }) else {
            result[legacy] = text
            return result
        }
        var current: Int?
        for line in text.components(separatedBy: "\n") {
            if let index = labels.firstIndex(where: { line == "【\($0)】" }) {
                current = index
            } else if let index = current {
                result[index] += result[index].isEmpty ? line : "\n" + line
            }
        }
        return result.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
    }
}

private struct MeetingProfileDraftView: View {
    @ObservedObject var model: WorkspaceModel
    @Environment(\.dismiss) private var dismiss
    @State private var fields: MeetingProfileFields
    @State private var mode: String
    @State private var error = ""
    @State private var saving = false

    init(model: WorkspaceModel) {
        self.model = model
        _fields = State(initialValue: MeetingProfileFields(
            background: model.settings.meetingBackground, terms: model.settings.meetingTerms))
        _mode = State(initialValue: model.settings.meetingASRMode)
    }

    private var valid: Bool {
        fields.backgroundText.unicodeScalars.count <= 4000 &&
        fields.termsText.unicodeScalars.count <= 2000 &&
        ContractLimits.meetingASRModes.contains(mode)
    }

    private func section(_ label: String, value: Binding<String>, height: CGFloat = 76) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.subheadline.bold())
            TextEditor(text: value)
                .frame(height: height)
                .overlay(RoundedRectangle(cornerRadius: 5).stroke(Color(nsColor: .separatorColor)))
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("会议资料").font(.title2.bold())
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    Text("个人与会议背景").font(.headline)
                    section("我的角色", value: $fields.role)
                    section("相关经历", value: $fields.experience)
                    section("项目背景", value: $fields.project, height: 105)
                    section("本次会议目标", value: $fields.goal)
                    section("限制与边界条件", value: $fields.constraints)
                    Text("术语与缩写").font(.headline)
                    section("专有名词与产品名称", value: $fields.properNouns)
                    section("缩写及其完整含义", value: $fields.acronyms)
                    section("中英文术语对照（每行一条）", value: $fields.bilingualTerms, height: 105)
                    Picker("会议识别模式", selection: $mode) {
                        Text(ContractLimits.meetingASRMultipartTitle)
                            .tag(ContractLimits.meetingASRMultipart)
                        Text(ContractLimits.meetingASRLlamaPrefixTitle)
                            .tag(ContractLimits.meetingASRLlamaPrefix)
                    }
                    .accessibilityIdentifier("meeting_profile_asr_mode")
                    Text("分段识别使用兼容的音频上传接口；前缀续写需要支持该协议的本地 ASR 服务。")
                        .font(.caption).foregroundStyle(.secondary)
                }
                .padding(.trailing, 5)
            }
            Text("背景最多 4000 字，术语最多 2000 字（包含分组标题）。运行中保存将取消旧识别、翻译和生成任务，保留已完成字幕，并立即应用新配置；若更新失败，保存内容在下次会议生效。")
                .font(.caption).foregroundStyle(.secondary)
            if !valid { Text("背景或术语超出长度限制").foregroundStyle(.red) }
            if !error.isEmpty {
                Text(error).foregroundStyle(.red).accessibilityIdentifier("meeting_profile_error")
            }
            HStack {
                Spacer()
                Button("取消") { dismiss() }.disabled(saving)
                Button(saving ? "正在应用…" : "保存并应用") {
                    saving = true
                    model.updateMeetingProfile(background: fields.backgroundText,
                                               terms: fields.termsText, mode: mode) { ok, message in
                        saving = false
                        if ok { dismiss() } else { error = message }
                    }
                }
                .disabled(!valid || saving || model.meetingProfileUpdating)
                .accessibilityIdentifier("meeting_profile_save")
            }
        }
        .padding(20)
        .frame(width: 590, height: 700)
    }
}

// NSTextView keeps the user's scroll offset while new finalized turns arrive.
// SwiftUI-only ScrollViewReader would move old-history readers on every poll.
private struct HistoryReaderText: NSViewRepresentable {
    let text: String
    let completedCount: Int
    let newestID: Int?
    let fontSize: Int
    @Binding var following: Bool
    @Binding var unread: Int

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let scroller = NSScrollView()
        scroller.hasVerticalScroller = true
        scroller.drawsBackground = false
        let editor = NSTextView(frame: .zero)
        editor.isEditable = false
        editor.isSelectable = true
        editor.isRichText = false
        editor.drawsBackground = false
        editor.isVerticallyResizable = true
        editor.isHorizontallyResizable = false
        editor.textContainer?.widthTracksTextView = true
        editor.autoresizingMask = [.width]
        editor.font = NSFont.systemFont(ofSize: CGFloat(fontSize))
        scroller.documentView = editor
        scroller.contentView.postsBoundsChangedNotifications = true
        context.coordinator.scrollView = scroller
        NotificationCenter.default.addObserver(context.coordinator,
            selector: #selector(Coordinator.scrolled(_:)),
            name: NSView.boundsDidChangeNotification, object: scroller.contentView)
        return scroller
    }

    func updateNSView(_ scroller: NSScrollView, context: Context) {
        guard let editor = scroller.documentView as? NSTextView else { return }
        let coordinator = context.coordinator
        coordinator.parent = self
        if editor.font?.pointSize != CGFloat(fontSize) {
            editor.font = NSFont.systemFont(ofSize: CGFloat(fontSize))
        }
        let oldCount = coordinator.count
        if editor.string != text {
            let previouslyFollowing = following
            let oldOffset = scroller.contentView.bounds.origin
            coordinator.updating = true
            editor.string = text
            if previouslyFollowing {
                scrollToEnd(scroller)
            } else {
                // Updating the NSTextStorage can change its document height.
                // Restore the reader's prior viewport rather than tracking a new turn.
                let limit = max(0, editor.frame.height - scroller.contentView.bounds.height)
                scroller.contentView.scroll(to: NSPoint(x: oldOffset.x, y: min(oldOffset.y, limit)))
                scroller.reflectScrolledClipView(scroller.contentView)
            }
            coordinator.updating = false
        } else if following && !coordinator.wasFollowing {
            scrollToEnd(scroller)
        }
        let nextUnread = MeetingReaderLayout.unread(
            previous: oldCount, current: completedCount,
            unread: unread, following: following,
            previousID: coordinator.newestID, currentID: newestID)
        coordinator.count = completedCount
        coordinator.newestID = newestID
        coordinator.wasFollowing = following
        if nextUnread != unread {
            DispatchQueue.main.async { [weak coordinator] in
                guard let coordinator else { return }
                if coordinator.parent.following { coordinator.parent.unread = 0 }
                else { coordinator.parent.unread = nextUnread }
            }
        }
    }

    private func scrollToEnd(_ scroller: NSScrollView) {
        guard let document = scroller.documentView else { return }
        let end = max(0, document.frame.height - scroller.contentView.bounds.height)
        scroller.contentView.scroll(to: NSPoint(x: 0, y: end))
        scroller.reflectScrolledClipView(scroller.contentView)
    }

    static func dismantleNSView(_ nsView: NSScrollView, coordinator: Coordinator) {
        NotificationCenter.default.removeObserver(coordinator)
    }

    final class Coordinator: NSObject {
        var parent: HistoryReaderText
        weak var scrollView: NSScrollView?
        var count = 0
        var newestID: Int?
        var wasFollowing = true
        var updating = false

        init(_ parent: HistoryReaderText) { self.parent = parent }

        @objc func scrolled(_ notification: Notification) {
            guard !updating, let scroller = scrollView,
                  let document = scroller.documentView else { return }
            let atBottom = document.frame.height -
                scroller.contentView.bounds.maxY <= 24
            if atBottom != parent.following {
                DispatchQueue.main.async { [weak self] in
                    self?.parent.following = atBottom
                    if atBottom { self?.parent.unread = 0 }
                }
            }
        }
    }
}


/// Single native, floating reader window. Unlike SwiftUI openWindow(id:),
/// this does not depend on a scene registered by another feature PR and
/// survives activation of Zoom/Teams without covering the editor itself.
@MainActor
final class MeetingReaderWindowController: NSObject, NSWindowDelegate {
    static let shared = MeetingReaderWindowController()

    private var panel: NSPanel?
    private weak var meeting: WorkspaceModel?

    func open(model: WorkspaceModel) {
        if let panel {
            panel.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            return
        }
        meeting = model
        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 780, height: 840),
            styleMask: [.titled, .closable, .resizable, .miniaturizable],
            backing: .buffered,
            defer: false)
        panel.title = "VoiceRewrite · 会议阅读"
        panel.isFloatingPanel = true
        panel.level = .floating
        panel.hidesOnDeactivate = false
        panel.isReleasedWhenClosed = false
        panel.minSize = NSSize(width: 680, height: 610)
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.contentView = NSHostingView(rootView:
            MeetingReaderView(model: model, embedded: false))
        panel.delegate = self
        if !panel.setFrameUsingName("VoiceRewriteMeetingReader") {
            panel.center()
        }
        let safeFrame = MeetingReaderLayout.restoredFrame(panel.frame,
            displays: NSScreen.screens.map(\.visibleFrame))
        if safeFrame != panel.frame { panel.setFrame(safeFrame, display: false) }
        panel.setFrameAutosaveName("VoiceRewriteMeetingReader")
        self.panel = panel
        panel.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func windowWillClose(_ notification: Notification) {
        panel = nil
        // Do not leave the microphone/system capture running invisibly
        // when the user explicitly closes the independent Meeting reader.
        if let meeting, meeting.meetingActive || meeting.meetingStarting {
            meeting.stopMeeting()
        }
        meeting = nil
    }
}
