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

    func present(title: String, text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        self.title = title
        self.text = trimmed
        let height = ResultCardMetrics.height(unicodeScalars: trimmed.unicodeScalars.count)
        ensurePanel()
        guard let panel else { return }
        panel.contentView = NSHostingView(rootView: ResultCardView(
            title: title, text: trimmed, width: ResultCardMetrics.width, height: height,
            onCopy: { [weak self] in self?.copy() },
            onClose: { [weak self] in self?.dismiss() }))
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
        NSPasteboard.general.setString(text, forType: .string)
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
        let editor = ResultHitRect(left: 16, top: 44, right: width - 16, bottom: height - 56)
        let buttons = [
            ResultHitRect(left: width - 196, top: height - 48, right: width - 104, bottom: height - 12),
            ResultHitRect(left: width - 96, top: height - 48, right: width - 16, bottom: height - 12),
        ]
        if ResultCardMetrics.clickDismisses(x: localX, y: localY, window: window, editor: editor, buttons: buttons) {
            dismiss()
        }
    }
}

private struct ResultCardView: View {
    let title: String
    let text: String
    let width: CGFloat
    let height: CGFloat
    let onCopy: () -> Void
    let onClose: () -> Void

    var body: some View {
        ZStack(alignment: .topLeading) {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(Color(indicator: IndicatorMath.pill))
            Text(title)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Color(indicator: IndicatorMath.labelText))
                .lineLimit(1)
                .frame(width: width - 32, alignment: .leading)
                .position(x: width / 2, y: 28)
            ScrollView {
                Text(text).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                    .foregroundStyle(Color(indicator: IndicatorMath.clockText))
            }
            .frame(width: width - 32, height: height - 100)
            .position(x: width / 2, y: 44 + (height - 100) / 2)
            Button(action: onCopy) {
                Text("复制").frame(width: 88, height: 32)
            }
            .buttonStyle(.borderedProminent)
            .position(x: width - 148, y: height - 30)
            Button(action: onClose) {
                Text("关闭").frame(width: 80, height: 32)
            }
            .buttonStyle(.bordered)
            .position(x: width - 56, y: height - 30)
        }
        .frame(width: width, height: height)
    }
}
