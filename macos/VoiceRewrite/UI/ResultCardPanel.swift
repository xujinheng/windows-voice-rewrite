import AppKit
import SwiftUI

/// Floating result card. Size and outside-click dismissal follow the shared popup rules.
@MainActor
final class ResultCardPanel {
    static let shared = ResultCardPanel()

    private var panel: NSPanel?
    private var globalMonitor: Any?
    private var localMonitor: Any?
    private var title = ""
    private var text = ""
    private var note = ""

    func present(title: String, text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        self.title = title
        self.text = trimmed
        note = ""
        let height = ResultCardMetrics.height(unicodeScalars: trimmed.unicodeScalars.count)
        ensurePanel()
        guard let panel else { return }
        show(on: panel, height: height)
        place(panel, height: height)
        panel.orderFrontRegardless()
        installMonitors()
    }

    func dismiss() {
        if let globalMonitor { NSEvent.removeMonitor(globalMonitor) }
        if let localMonitor { NSEvent.removeMonitor(localMonitor) }
        globalMonitor = nil
        localMonitor = nil
        panel?.orderOut(nil)
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        let copied = NSPasteboard.general.setString(text, forType: .string)
        note = copied ? ResultCopy.copySucceeded : ResultCopy.copyFailed
        guard let panel else { return }
        let height = ResultCardMetrics.height(unicodeScalars: text.unicodeScalars.count)
        show(on: panel, height: height)
    }

    private func show(on panel: NSPanel, height: CGFloat) {
        panel.contentView = NSHostingView(rootView: ResultCardView(
            title: title, text: text, note: note, width: ResultCardMetrics.width, height: height,
            onCopy: { [weak self] in self?.copy() },
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

    private func place(_ panel: NSPanel, height: CGFloat) {
        let mouse = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(mouse, $0.frame, false) } ?? NSScreen.main
        let visible = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 800, height: 600)
        let area = DesktopWorkArea(
            left: Int32(visible.minX.rounded()), top: Int32(visible.minY.rounded()),
            right: Int32(visible.maxX.rounded()), bottom: Int32(visible.maxY.rounded()))
        let (x, y) = area.clamp(
            x: Int32((mouse.x + 18).rounded()),
            y: Int32((mouse.y - 22 - height).rounded()),
            width: Int32(ResultCardMetrics.width), height: Int32(height))
        panel.setFrame(NSRect(x: CGFloat(x), y: CGFloat(y), width: ResultCardMetrics.width, height: height), display: true)
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
                Task { @MainActor in self?.dismissIfOutsideClick() }
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
        let buttons = [
            ResultCardChrome.closeButton(width: width).hit,
            ResultCardChrome.copyButton(width: width, height: height).hit,
        ]
        if ResultCardMetrics.clickDismisses(x: localX, y: localY, window: window, editor: editor, buttons: buttons) {
            dismiss()
        }
    }
}

private struct ResultCardView: View {
    let title: String
    let text: String
    let note: String
    let width: CGFloat
    let height: CGFloat
    let onCopy: () -> Void
    let onClose: () -> Void

    var body: some View {
        ZStack(alignment: .topLeading) {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(Color(indicator: IndicatorMath.pill))
            let editor = ResultCardChrome.editor(width: Int32(width), height: Int32(height))
            let close = ResultCardChrome.closeButton(width: Int32(width))
            let copy = ResultCardChrome.copyButton(width: Int32(width), height: Int32(height))
            let status = ResultCardChrome.note(width: Int32(width))
            Text(title)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Color(indicator: IndicatorMath.labelText))
                .lineLimit(1)
                .frame(width: note.isEmpty ? CGFloat(close.x - 28) : CGFloat(status.x - 24), alignment: .leading)
                .position(x: 16 + (note.isEmpty ? CGFloat(close.x - 28) : CGFloat(status.x - 24)) / 2, y: 28)
            if !note.isEmpty {
                Text(note)
                    .font(.system(size: 12))
                    .foregroundStyle(Color(indicator: IndicatorMath.labelText))
                    .lineLimit(1)
                    .frame(width: CGFloat(status.w), height: CGFloat(status.h), alignment: .leading)
                    .position(x: CGFloat(status.x) + CGFloat(status.w) / 2, y: CGFloat(status.y) + CGFloat(status.h) / 2)
            }
            ScrollView {
                Text(text).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                    .foregroundStyle(Color(indicator: IndicatorMath.clockText))
            }
            .frame(width: CGFloat(editor.w), height: CGFloat(editor.h))
            .position(x: CGFloat(editor.x) + CGFloat(editor.w) / 2, y: CGFloat(editor.y) + CGFloat(editor.h) / 2)
            Button(action: onCopy) {
                Text("复制结果").frame(width: CGFloat(copy.w), height: CGFloat(copy.h))
            }
            .buttonStyle(.borderedProminent)
            .position(x: CGFloat(copy.x) + CGFloat(copy.w) / 2, y: CGFloat(copy.y) + CGFloat(copy.h) / 2)
            Button(action: onClose) {
                Text("×").frame(width: CGFloat(close.w), height: CGFloat(close.h))
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color(indicator: IndicatorMath.closeGlyph))
            .position(x: CGFloat(close.x) + CGFloat(close.w) / 2, y: CGFloat(close.y) + CGFloat(close.h) / 2)
        }
        .frame(width: width, height: height)
    }
}
