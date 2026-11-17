import AppKit
import SwiftUI

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
        return numbers.map(CGFloat.init)
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

    static func unread(previous: Int, current: Int, unread: Int, following: Bool) -> Int {
        if following || current < previous { return 0 }
        return unread + max(0, current - previous)
    }
}

private struct ReaderSnapshot {
    let captions: [MeetingCaptionRow]
    let replies: [MeetingReplyRow]
    let replyStatus: String
    let replySource: String
    let replyTip: String
    let expression: String
    let expressionChinese: String
    let expressionStatus: String

    init(model: WorkspaceModel) {
        captions = model.meetingCaptions
        replies = model.meetingReplies
        replyStatus = model.meetingReplyStatus
        replySource = model.meetingReplySource
        replyTip = model.meetingReplyTip
        expression = model.meetingExpression
        expressionChinese = model.meetingExpressionChinese
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
    @State private var weights = MeetingReaderLayout.defaults
    @State private var dragBase: [CGFloat]?
    @State private var dragIndex: Int?
    @State private var frozen: ReaderSnapshot?
    @State private var following = true
    @State private var unread = 0
    @State private var showProfile = false

    private var captions: [MeetingCaptionRow] { frozen?.captions ?? model.meetingCaptions }
    private var replies: [MeetingReplyRow] { frozen?.replies ?? model.meetingReplies }
    private var latest: MeetingCaptionRow? { captions.last }
    private var past: [MeetingCaptionRow] {
        let finalized = captions.filter(\.final)
        guard let last = latest, last.final, finalized.last?.id == last.id else { return finalized }
        return Array(finalized.dropLast())
    }
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
            row.chinese.isEmpty ? row.source : row.source + "\n" + row.chinese
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
        .frame(minWidth: embedded ? 590 : 700, minHeight: embedded ? 625 : 735)
        .onAppear { weights = MeetingReaderLayout.decode(savedWeights) }
        .onChange(of: model.meetingActive) { _, active in
            if !active {
                frozen = nil
                following = true
                unread = 0
            }
        }
        .sheet(isPresented: $showProfile) {
            MeetingProfileDraftView(mode: model.settings.meetingASRMode)
        }
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
            Button("暂停采集") {}
                .disabled(true)
                .help("等待 D/F 提供会议采集暂停和恢复状态接口")
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
                Capsule().fill(.secondary.opacity(0.65))
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
            section("之前的对话 · \(past.count) 段\(unread > 0 ? " · \(unread) 条新内容" : "")",
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
                    HistoryReaderText(text: history, completedCount: past.count, fontSize: fontSize,
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
        VStack(alignment: .leading, spacing: 5) {
            TextEditor(text: $model.meetingInput)
                .font(.system(size: CGFloat(fontSize)))
                .scrollContentBackground(.hidden)
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
                Button("取消处理") { model.cancelMeetingWork() }
                    .disabled(!model.meetingActive || model.meetingCancelPending)
                Spacer()
            }
            .controlSize(.small)
        }
    }
}

// This input surface is ready for F/H's profile persistence and live mode-change
// endpoints. It must not claim that unsent notes affect ASR or the model.
private struct MeetingProfileDraftView: View {
    let mode: String
    @Environment(\.dismiss) private var dismiss
    @State private var background = ""
    @State private var terms = ""
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("会议资料").font(.title2.bold())
            Text("背景：角色、项目、目标、已知限制").font(.headline)
            TextEditor(text: $background).frame(height: 135)
                .onChange(of: background) { _, value in
                    if value.count > 4000 { background = String(value.prefix(4000)) }
                }
            Text("术语：人名、缩写、产品名和中英对照（每行一条）").font(.headline)
            TextEditor(text: $terms).frame(height: 120)
                .onChange(of: terms) { _, value in
                    if value.count > 2000 { terms = String(value.prefix(2000)) }
                }
            Text("会议识别方式：\(mode)").foregroundStyle(.secondary)
            Text("会议资料保存接口尚未接入。当前输入不会发送给 ASR 或 LLM。")
                .font(.caption).foregroundStyle(.orange)
            HStack {
                Spacer()
                Button("取消") { dismiss() }
                Button("保存并应用") {}.disabled(true)
            }
        }
        .padding(20)
        .frame(width: 540)
    }
}

// NSTextView keeps the user's scroll offset while new finalized turns arrive.
// SwiftUI-only ScrollViewReader would move old-history readers on every poll.
private struct HistoryReaderText: NSViewRepresentable {
    let text: String
    let completedCount: Int
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
            coordinator.updating = true
            editor.string = text
            coordinator.updating = false
            if previouslyFollowing { scrollToEnd(scroller) }
        } else if following && !coordinator.wasFollowing {
            scrollToEnd(scroller)
        }
        let nextUnread = MeetingReaderLayout.unread(
            previous: oldCount, current: completedCount,
            unread: unread, following: following)
        coordinator.count = completedCount
        coordinator.wasFollowing = following
        if nextUnread != unread {
            DispatchQueue.main.async { [weak coordinator] in
                guard let coordinator else { return }
                coordinator.parent.unread = nextUnread
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
