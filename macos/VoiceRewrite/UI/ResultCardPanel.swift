import AppKit
import SwiftUI
import VoiceRewriteInteraction

/// A card only delegates an explicitly authorized edit to InteractionSession.
@MainActor
struct ResultCardApplyAction {
    let isBusy: @MainActor () -> Bool
    let perform: @MainActor () async throws -> Void
    let cancel: @MainActor () -> Void
}

enum ResultCardApplyFailure: Error {
    case editorClosed
    case unsafe
    case unconfirmed
}

private enum ResultApplyCopy {
    static let busy = "任务尚未结束，请稍后点击应用"
    static let checking = "正在核验原选区并应用…"
    static let unsafe = "无法安全替换，请使用复制结果"
    static let editorClosed = "原编辑器已关闭，请使用复制结果"
    static let confirmed = "已应用到原文"
    static let unconfirmed = "写入结果未确认，请检查原文；仍可复制"
}

/// One result card owns its copy status; closing it invalidates later updates.
@MainActor
final class ResultCardActionSession: ObservableObject {
    let text: String
    @Published private(set) var note = ""
    @Published private(set) var isClosed = false
    @Published private(set) var attempted = false
    @Published private(set) var isApplying = false
    private let applyAction: ResultCardApplyAction?

    var hasApplyAction: Bool { applyAction != nil }
    var canApply: Bool { hasApplyAction && !attempted && !isClosed }

    init(text: String, applyAction: ResultCardApplyAction? = nil) {
        self.text = text
        self.applyAction = applyAction
    }

    /// A busy owner keeps the authorization; any attempt consumes it once.
    func apply() async {
        guard canApply, let applyAction else { return }
        if applyAction.isBusy() {
            note = ResultApplyCopy.busy
            return
        }
        attempted = true
        isApplying = true
        defer { isApplying = false }
        note = ResultApplyCopy.checking
        guard !Task.isCancelled, !isClosed else { return }
        do {
            try await applyAction.perform()
            guard !isClosed else { return }
            note = ResultApplyCopy.confirmed
        } catch {
            guard !isClosed else { return }
            switch error {
            case ResultCardApplyFailure.editorClosed,
                 InteractionFailure.noFrontmostApplication, InteractionFailure.noFocusedElement:
                note = ResultApplyCopy.editorClosed
            case ResultCardApplyFailure.unconfirmed, InteractionFailure.writeNotConfirmed:
                note = ResultApplyCopy.unconfirmed
            case is ClipboardFailure:
                // An ownership/restore error can follow an already-posted paste.
                note = ResultApplyCopy.unconfirmed
            default:
                note = ResultApplyCopy.unsafe
            }
        }
    }

    func copyCompleted(_ copied: Bool) {
        guard !isClosed else { return }
        note = copied ? ResultCopy.copySucceeded : ResultCopy.copyFailed
    }

    func close() {
        guard !isClosed else { return }
        isClosed = true
        applyAction?.cancel()
    }
}

/// Result-card dimensions and standard controls follow the generated popup rules.
@MainActor
final class ResultCardPanel {
    static let shared = ResultCardPanel()

    private var panel: NSPanel?
    private var globalMonitor: Any?
    private var localMonitor: Any?
    private var title = ""
    private var session: ResultCardActionSession?
    private var applyWork: Task<Void, Never>?

    func present(title: String, text: String, applyAction: ResultCardApplyAction? = nil) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        dismiss()
        self.title = title
        session = ResultCardActionSession(text: trimmed, applyAction: applyAction)
        ensurePanel()
        guard let panel else { return }
        place(panel, idealHeight: ResultCardMetrics.height(unicodeScalars: trimmed.unicodeScalars.count))
        panel.orderFrontRegardless()
        installMonitors()
    }

    func dismiss() {
        applyWork?.cancel()
        applyWork = nil
        session?.close()
        session = nil
        if let globalMonitor { NSEvent.removeMonitor(globalMonitor) }
        if let localMonitor { NSEvent.removeMonitor(localMonitor) }
        globalMonitor = nil
        localMonitor = nil
        panel?.orderOut(nil)
    }

    private func copy() {
        guard let session, panel?.isVisible == true, !session.isApplying else { return }
        NSPasteboard.general.clearContents()
        session.copyCompleted(NSPasteboard.general.setString(session.text, forType: .string))
    }

    private func apply() {
        guard let session, panel?.isVisible == true, applyWork == nil else { return }
        applyWork = Task { [weak self] in
            await session.apply()
            if self?.session === session { self?.applyWork = nil }
        }
    }

    private func show(on panel: NSPanel, width: CGFloat, height: CGFloat) {
        guard let session else { return }
        panel.contentView = NSHostingView(rootView: ResultCardView(
            title: title, session: session, width: width, height: height,
            onCopy: { [weak self] in self?.copy() },
            onApply: { [weak self] in self?.apply() },
            onClose: { [weak self] in self?.dismiss() }))
    }

    private func ensurePanel() {
        guard panel == nil else { return }
        let created = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: ResultCardMetrics.width, height: 258),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered, defer: false)
        created.isFloatingPanel = true
        created.level = .floating
        created.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        created.isOpaque = false
        created.backgroundColor = .clear
        created.hasShadow = true
        created.hidesOnDeactivate = false
        created.becomesKeyOnlyIfNeeded = true
        created.isReleasedWhenClosed = false
        panel = created
    }

    private func place(_ panel: NSPanel, idealHeight: CGFloat) {
        let mouse = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(mouse, $0.frame, false) } ?? NSScreen.main
        let visible = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 800, height: 600)
        let width = min(ResultCardMetrics.width, max(1, visible.width))
        let height = min(idealHeight, max(1, visible.height))
        let area = DesktopWorkArea(
            left: Int32(visible.minX.rounded()), top: Int32(visible.minY.rounded()),
            right: Int32(visible.maxX.rounded()), bottom: Int32(visible.maxY.rounded()))
        let (x, y) = area.clamp(
            x: Int32((mouse.x + 18).rounded()),
            y: Int32((mouse.y - 22 - height).rounded()),
            width: Int32(width), height: Int32(height))
        panel.setFrame(NSRect(x: CGFloat(x), y: CGFloat(y), width: width, height: height), display: true)
        show(on: panel, width: width, height: height)
    }

    private func installMonitors() {
        if globalMonitor == nil {
            globalMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown]) { [weak self] _ in
                Task { @MainActor in self?.dismissIfOutsideClick() }
            }
        }
        if localMonitor == nil {
            localMonitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .keyDown]) { [weak self] event in
                if event.type == .keyDown && event.keyCode == 53 {
                    Task { @MainActor in self?.dismiss() }
                    return nil
                }
                if event.type == .leftMouseDown {
                    Task { @MainActor in self?.dismissIfOutsideClick() }
                }
                return event
            }
        }
    }

    private func dismissIfOutsideClick() {
        guard let panel, panel.isVisible else { return }
        let mouse = NSEvent.mouseLocation
        let frame = panel.frame
        let localX = Int32((mouse.x - frame.minX).rounded())
        let localY = Int32((frame.maxY - mouse.y).rounded())
        let height = Int32(frame.height.rounded())
        let width = Int32(frame.width.rounded())
        let window = ResultHitRect(left: 0, top: 0, right: width, bottom: height)
        let editor = ResultCardChrome.editor(width: width, height: height).hit
        var buttons = [
            ResultCardChrome.closeButton(width: width).hit,
            ResultCardChrome.copyButton(width: width, height: height).hit,
        ]
        if session?.hasApplyAction == true {
            buttons.append(ResultCardView.applyFrame(width: width, height: height).hit)
        }
        if ResultCardMetrics.clickDismisses(x: localX, y: localY, window: window, editor: editor, buttons: buttons) {
            dismiss()
        }
    }
}

struct ResultCardPalette {
    let background: Color
    let surface: Color
    let text: Color
    let muted: Color
    let border: Color
    let accent: Color
    let accentText: Color
    let secondary: Color

    init(dark: Bool) {
        if dark {
            background = Color(red: 16/255, green: 18/255, blue: 24/255)
            surface = Color(red: 35/255, green: 38/255, blue: 49/255)
            text = Color(red: 242/255, green: 241/255, blue: 247/255)
            muted = Color(red: 164/255, green: 165/255, blue: 183/255)
            border = Color(red: 53/255, green: 57/255, blue: 72/255)
            accent = Color(red: 186/255, green: 174/255, blue: 255/255)
            accentText = Color(red: 16/255, green: 18/255, blue: 24/255)
            secondary = Color(red: 41/255, green: 44/255, blue: 56/255)
        } else {
            background = Color(red: 248/255, green: 249/255, blue: 252/255)
            surface = .white
            text = Color(red: 37/255, green: 38/255, blue: 55/255)
            muted = Color(red: 119/255, green: 123/255, blue: 140/255)
            border = Color(red: 229/255, green: 230/255, blue: 239/255)
            accent = Color(red: 114/255, green: 88/255, blue: 207/255)
            accentText = .white
            secondary = Color(red: 240/255, green: 241/255, blue: 246/255)
        }
    }
}

struct ResultCardView: View {
    @Environment(\.colorScheme) private var colorScheme
    let title: String
    @ObservedObject var session: ResultCardActionSession
    let width: CGFloat
    let height: CGFloat
    let onCopy: () -> Void
    let onApply: () -> Void
    let onClose: () -> Void

    // Shared popup.ApplyButton is 116x33, 124 points left of Copy.
    static func applyFrame(width: Int32, height: Int32) -> ResultRect {
        let copy = ResultCardChrome.copyButton(width: width, height: height)
        return ResultRect(x: copy.x - 124, y: copy.y, w: 116, h: copy.h)
    }

    var body: some View {
        let palette = ResultCardPalette(dark: colorScheme == .dark)
        let note = session.note
        let editor = ResultCardChrome.editor(width: Int32(width), height: Int32(height))
        let close = ResultCardChrome.closeButton(width: Int32(width))
        let copy = ResultCardChrome.copyButton(width: Int32(width), height: Int32(height))
        let apply = Self.applyFrame(width: Int32(width), height: Int32(height))
        let status = ResultCardChrome.note(width: Int32(width))
        ZStack(alignment: .topLeading) {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(palette.background)
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(palette.surface)
                .overlay(RoundedRectangle(cornerRadius: 12).stroke(palette.border, lineWidth: 1))
                .frame(width: width - 24, height: height - 104)
                .position(x: width / 2, y: 48 + (height - 104) / 2)
            Rectangle().fill(palette.border).frame(width: width - 32, height: 1)
                .position(x: width / 2, y: height - 56)
            Text(title)
                .font(.system(size: 18, weight: .semibold))
                .foregroundStyle(palette.text)
                .lineLimit(1)
                .frame(width: note.isEmpty ? CGFloat(close.x - 34) : CGFloat(status.x - 24), alignment: .leading)
                .position(x: 18 + (note.isEmpty ? CGFloat(close.x - 34) : CGFloat(status.x - 24)) / 2, y: 27)
            if !note.isEmpty {
                Text(note)
                    .font(.system(size: 12))
                    .foregroundStyle(palette.muted)
                    .lineLimit(1)
                    .help(note)
                    .frame(width: CGFloat(status.w), height: CGFloat(status.h), alignment: .leading)
                    .position(x: CGFloat(status.x) + CGFloat(status.w) / 2,
                              y: CGFloat(status.y) + CGFloat(status.h) / 2)
                    .accessibilityIdentifier("result-card-note")
            }
            ScrollView {
                Text(session.text)
                    .font(.system(size: 16))
                    .textSelection(.enabled)
                    .foregroundStyle(palette.text)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 3)
            }
            .frame(width: CGFloat(editor.w), height: CGFloat(editor.h))
            .position(x: CGFloat(editor.x) + CGFloat(editor.w) / 2,
                      y: CGFloat(editor.y) + CGFloat(editor.h) / 2)
            .accessibilityIdentifier("result-card-body")
            Button(action: onCopy) {
                Text(ResultCopy.copyAction)
                    .font(.system(size: 14, weight: .semibold))
                    .frame(width: CGFloat(copy.w), height: CGFloat(copy.h))
                    .background(RoundedRectangle(cornerRadius: 8)
                        .fill(session.canApply ? palette.secondary : palette.accent))
                    .foregroundStyle(session.canApply ? palette.text : palette.accentText)
            }
            .buttonStyle(.plain)
            .disabled(session.isApplying)
            .position(x: CGFloat(copy.x) + CGFloat(copy.w) / 2,
                      y: CGFloat(copy.y) + CGFloat(copy.h) / 2)
            .accessibilityIdentifier("result-card-copy")
            if session.hasApplyAction {
                Button(action: onApply) {
                    Text(ResultCopy.applyAction)
                        .font(.system(size: 14, weight: .semibold))
                        .frame(width: CGFloat(apply.w), height: CGFloat(apply.h))
                        .background(RoundedRectangle(cornerRadius: 8)
                            .fill(session.canApply ? palette.accent : palette.secondary))
                        .foregroundStyle(session.canApply ? palette.accentText : palette.muted)
                }
                .buttonStyle(.plain)
                .disabled(!session.canApply)
                .position(x: CGFloat(apply.x) + CGFloat(apply.w) / 2,
                          y: CGFloat(apply.y) + CGFloat(apply.h) / 2)
                .accessibilityIdentifier("result-card-apply")
            }
            Button(action: onClose) {
                Text(ResultCopy.closeGlyph)
                    .font(.system(size: 21))
                    .frame(width: CGFloat(close.w), height: CGFloat(close.h))
                    .foregroundStyle(palette.muted)
            }
            .buttonStyle(.plain)
            .position(x: CGFloat(close.x) + CGFloat(close.w) / 2,
                      y: CGFloat(close.y) + CGFloat(close.h) / 2)
            .accessibilityIdentifier("result-card-close")
        }
        .frame(width: width, height: height)
    }
}
