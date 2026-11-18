import AppKit

// A native paint surface, not another voice-task state machine. Visual geometry,
// colors, and timing match the retired 232x50/252x50 recording pill.
enum PillAppearance {
    case recording, processing, sent, error, fallback
}

@MainActor
final class RecordingPill {
    private let view = PillSurface(frame: NSRect(x: 0, y: 0, width: 232, height: 50))
    private var panel: NSPanel?
    private var timer: Timer?
    private var terminalAt: Date?
    private var levelSource: () -> Double = { 0 }
    private var smoothed = 0.0
    var onCancel: (() -> Void)?

    var isVisible: Bool { panel?.isVisible == true }

    func prepare() {
        guard panel == nil else { return }
        let window = NSPanel(
            contentRect: view.bounds,
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false)
        window.level = .statusBar
        window.isFloatingPanel = true
        window.hidesOnDeactivate = false
        window.isReleasedWhenClosed = false
        window.becomesKeyOnlyIfNeeded = true
        window.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
        window.isOpaque = false
        window.backgroundColor = .clear
        window.hasShadow = false
        view.onCancel = { [weak self] in self?.onCancel?() }
        window.contentView = view
        panel = window
    }

    func recording(rewrite: Bool, started: Date, meter: @escaping () -> Double) {
        levelSource = meter
        smoothed = 0
        terminalAt = nil
        view.rewrite = rewrite
        view.started = started
        view.appearance = .recording
        view.level = 0
        display()
    }

    func processing() {
        guard isVisible else { return }
        terminalAt = nil
        view.appearance = .processing
        display()
    }

    func result(_ appearance: PillAppearance) {
        guard isVisible else { return }
        terminalAt = Date()
        view.appearance = appearance
        display()
    }

    func dismiss() {
        timer?.invalidate()
        timer = nil
        terminalAt = nil
        smoothed = 0
        levelSource = { 0 }
        panel?.orderOut(nil)
    }

    private func display() {
        prepare()
        reposition()
        view.needsDisplay = true
        panel?.orderFrontRegardless()
        if timer == nil {
            let ticker = Timer(timeInterval: 0.07, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.tick() }
            }
            RunLoop.main.add(ticker, forMode: .common)
            timer = ticker
        }
    }

    private func tick() {
        guard isVisible else { dismiss(); return }
        if let terminalAt, Date().timeIntervalSince(terminalAt) >= 1.75 {
            dismiss()
            return
        }
        if view.appearance == .recording {
            let raw = levelSource()
            let normalized = raw.isFinite ? min(1, max(0, raw * 5)) : 0
            smoothed = 0.72 * smoothed + 0.28 * normalized
            view.level = smoothed
        }
        view.tick &+= 1
        view.needsDisplay = true
    }

    private func reposition() {
        guard let panel else { return }
        let width: CGFloat = view.appearance == .recording ? 232 : 252
        let cursor = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(cursor, $0.frame, false) } ?? NSScreen.main
        guard let bounds = screen?.visibleFrame else { return }
        let frame = NSRect(
            x: max(bounds.minX, min(bounds.maxX - width, bounds.midX - width / 2)),
            y: bounds.minY + 5, width: width, height: 50)
        if panel.frame != frame { panel.setFrame(frame, display: true) }
        view.frame = NSRect(origin: .zero, size: frame.size)
    }
}

private final class PillSurface: NSView {
    var appearance: PillAppearance = .recording
    var rewrite = false
    var started = Date()
    var level = 0.0
    var tick: UInt32 = 0
    var onCancel: (() -> Void)?
    private var hovered = false

    override var isFlipped: Bool { false }

    private func rgb(_ red: Int, _ green: Int, _ blue: Int, alpha: CGFloat = 1) -> NSColor {
        NSColor(calibratedRed: CGFloat(red) / 255, green: CGFloat(green) / 255,
                blue: CGFloat(blue) / 255, alpha: alpha)
    }

    private var accent: NSColor {
        if appearance == .error { return rgb(255, 129, 138) }
        return rewrite ? rgb(175, 165, 255) : rgb(233, 129, 170)
    }

    private func capsule(_ rect: NSRect, radius: CGFloat, color: NSColor) {
        color.setFill()
        NSBezierPath(roundedRect: rect, xRadius: radius, yRadius: radius).fill()
    }

    override func draw(_ dirtyRect: NSRect) {
        let w = bounds.width
        capsule(NSRect(x: 3, y: 2, width: w - 6, height: 40),
                radius: 20, color: rgb(0, 0, 0, alpha: 17.0 / 255))
        capsule(NSRect(x: 4, y: 3, width: w - 8, height: 40),
                radius: 20, color: rgb(0, 0, 0, alpha: 34.0 / 255))
        capsule(bounds.insetBy(dx: 5, dy: 5), radius: 20, color: rgb(32, 36, 46))
        let color = accent
        if appearance == .recording {
            capsule(NSRect(x: 12, y: 18, width: 3, height: 14), radius: 1.5, color: color)
            color.setFill()
            NSBezierPath(ovalIn: NSRect(x: 23, y: 21, width: 8, height: 8)).fill()
            for index in 0..<9 {
                let envelope = 0.44 + 0.56 * abs(sin(Double(index) * 0.75 + Double(tick) * 0.24))
                let h = CGFloat(min(18, max(4, (4 + 14 * pow(level, 0.7) * envelope).rounded())))
                capsule(NSRect(x: CGFloat(52 + index * 8), y: 25 - h / 2, width: 3, height: h),
                        radius: 1.5, color: color)
            }
            let seconds = max(0, Int(Date().timeIntervalSince(started)))
            let time = String(format: "%02d:%02d", seconds / 60, seconds % 60)
            label(time, x: 145, width: 50, size: 14, centered: true)
        } else {
            drawMark()
            let message: String
            switch appearance {
            case .processing: message = rewrite ? "识别指令…" : "正在转写…"
            case .sent: message = "已发送粘贴指令"
            case .error: message = "操作未完成 · 查看日志"
            case .fallback: message = "结果未写入"
            case .recording: message = ""
            }
            label(message, x: 53, width: w - 91, size: 13, centered: false)
        }
        if hovered {
            capsule(NSRect(x: w - 32, y: 11, width: 28, height: 28),
                    radius: 14, color: rgb(52, 58, 73))
        }
        rgb(170, 165, 179).setStroke()
        let cross = NSBezierPath()
        cross.lineWidth = 1.6
        cross.move(to: NSPoint(x: w - 22, y: 21))
        cross.line(to: NSPoint(x: w - 14, y: 29))
        cross.move(to: NSPoint(x: w - 14, y: 21))
        cross.line(to: NSPoint(x: w - 22, y: 29))
        cross.stroke()
    }

    private func label(_ message: String, x: CGFloat, width: CGFloat,
                       size: CGFloat, centered: Bool) {
        let paragraph = NSMutableParagraphStyle()
        paragraph.alignment = centered ? .center : .left
        let text = message as NSString
        text.draw(in: NSRect(x: x, y: 16, width: width, height: 21), withAttributes: [
            .font: NSFont.systemFont(ofSize: size, weight: centered ? .semibold : .medium),
            .foregroundColor: rgb(245, 243, 247),
            .paragraphStyle: paragraph
        ])
    }

    private func drawMark() {
        let color = accent
        if appearance == .processing {
            for index in 0..<8 {
                let angle = Double(tick) * 0.2 + Double(index) * .pi / 4
                let x = CGFloat(30 + 8 * cos(angle))
                let y = CGFloat(25 + 8 * sin(angle))
                color.withAlphaComponent(CGFloat(65 + index * 25) / 255).setFill()
                NSBezierPath(ovalIn: NSRect(x: x - 2, y: y - 2, width: 4, height: 4)).fill()
            }
        } else {
            let fill: NSColor
            switch appearance {
            case .sent: fill = rgb(141, 189, 232)
            case .fallback: fill = rgb(245, 197, 107)
            default: fill = rgb(255, 129, 138)
            }
            fill.setFill()
            NSBezierPath(ovalIn: NSRect(x: 22, y: 17, width: 16, height: 16)).fill()
            label(appearance == .sent ? "✓" : "!", x: 22, width: 16, size: 11, centered: true)
        }
    }

    override func updateTrackingAreas() {
        for area in trackingAreas { removeTrackingArea(area) }
        addTrackingArea(NSTrackingArea(rect: NSRect(x: bounds.width - 36, y: 0, width: 36, height: 50),
                                       options: [.mouseEnteredAndExited, .activeAlways],
                                       owner: self))
        super.updateTrackingAreas()
    }

    override func mouseEntered(with event: NSEvent) { hovered = true; needsDisplay = true }
    override func mouseExited(with event: NSEvent) { hovered = false; needsDisplay = true }

    override func mouseDown(with event: NSEvent) {
        if convert(event.locationInWindow, from: nil).x >= bounds.width - 36 {
            onCancel?()
        }
    }
}
