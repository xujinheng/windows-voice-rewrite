import AppKit

// A native paint surface, not another voice-task state machine. Visual geometry,
// colors, and timing match the retired 232x50/252x50 recording pill.
enum PillAppearance {
    case recording, processing, sent, error, fallback
}

@MainActor
final class RecordingPill {
    private let view = PillSurface(frame: .zero)
    private var panel: NSPanel?
    private var timer: Timer?
    private var terminalAt: Date?
    private var levelSource: () -> Double = { 0 }
    var onCancel: (() -> Void)?

    var isVisible: Bool { panel?.isVisible == true }

    func configure(_ spec: PillSpec) {
        view.spec = spec
        if isVisible { reposition(); view.needsDisplay = true }
    }

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
        terminalAt = nil
        view.rewrite = rewrite
        view.started = started
        view.phase = .recording
        view.level = 0
        display()
    }

    func processing() {
        guard isVisible else { return }
        terminalAt = nil
        view.phase = .processing
        display()
    }

    func result(_ phase: PillAppearance) {
        guard isVisible else { return }
        terminalAt = Date()
        view.phase = phase
        display()
    }

    func dismiss() {
        timer?.invalidate()
        timer = nil
        terminalAt = nil
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
        if let terminalAt, let spec = view.spec,
           Date().timeIntervalSince(terminalAt) * 1000 >= Double(spec.terminalMS) {
            dismiss()
            return
        }
        if view.phase == .recording {
            let raw = levelSource()
            view.level = VRLevel(view.level, raw)
        }
        view.tick &+= 1
        view.needsDisplay = true
    }

    private func reposition() {
        guard let panel else { return }
        guard let spec = view.spec else { return }
        let width = CGFloat(view.phase == .recording ? spec.recordingWidth : spec.workingWidth)
        let cursor = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(cursor, $0.frame, false) } ?? NSScreen.main
        guard let bounds = screen?.visibleFrame else { return }
        let frame = NSRect(
            x: max(bounds.minX, min(bounds.maxX - width, bounds.midX - width / 2)),
            y: bounds.minY + 5, width: width, height: CGFloat(spec.height))
        if panel.frame != frame { panel.setFrame(frame, display: true) }
        view.frame = NSRect(origin: .zero, size: frame.size)
    }
}

private final class PillSurface: NSView {
    var spec: PillSpec?
    var phase: PillAppearance = .recording
    var rewrite = false
    var started = Date()
    var level = 0.0
    var tick: UInt32 = 0
    var onCancel: (() -> Void)?
    private var hovered = false

    override var isFlipped: Bool { false }

    private func rgb(_ components: [Int], alpha: CGFloat = 1) -> NSColor {
        NSColor(calibratedRed: CGFloat(components[0]) / 255,
                green: CGFloat(components[1]) / 255,
                blue: CGFloat(components[2]) / 255, alpha: alpha)
    }

    private var accent: NSColor {
        let spec = spec!
        return rgb(phase == .error ? spec.errorAccent :
                   rewrite ? spec.rewriteAccent : spec.dictationAccent)
    }

    private func capsule(_ rect: NSRect, radius: CGFloat, color: NSColor) {
        color.setFill()
        NSBezierPath(roundedRect: rect, xRadius: radius, yRadius: radius).fill()
    }

    override func draw(_ dirtyRect: NSRect) {
        guard let spec else { return }
        let w = bounds.width
        capsule(NSRect(x: 3, y: 2, width: w - 6, height: 40),
                radius: 20, color: NSColor.black.withAlphaComponent(17.0 / 255))
        capsule(NSRect(x: 4, y: 3, width: w - 8, height: 40),
                radius: 20, color: NSColor.black.withAlphaComponent(34.0 / 255))
        capsule(bounds.insetBy(dx: 5, dy: 5), radius: 20, color: rgb(spec.pillColor))
        let color = accent
        if phase == .recording {
            capsule(NSRect(x: 12, y: 18, width: 3, height: 14), radius: 1.5, color: color)
            color.setFill()
            NSBezierPath(ovalIn: NSRect(x: 23, y: CGFloat(spec.iconY - 4), width: 8, height: 8)).fill()
            for index in 0..<spec.waveBars {
                let h = CGFloat(VRWave(Int32(index), level, tick))
                capsule(NSRect(x: CGFloat(spec.waveStartX + index * spec.waveStep),
                               y: CGFloat(spec.iconY) - h / 2,
                               width: CGFloat(spec.waveBarWidth), height: h),
                        radius: CGFloat(spec.waveBarWidth) / 2, color: color)
            }
            if let clock = VRDuration(Int64(Date().timeIntervalSince(started))) {
                label(String(cString: clock), x: CGFloat(spec.clockX),
                      width: CGFloat(spec.clockWidth), size: 14,
                      centered: true, color: rgb(spec.clockColor))
                VRFree(clock)
            }
        } else {
            drawMark()
            let message: String
            switch phase {
            case .processing: message = rewrite ? spec.processingRewrite : spec.processingDictation
            case .sent: message = spec.sentLabel
            case .error: message = spec.errorLabel
            case .fallback: message = spec.fallbackLabel
            case .recording: message = ""
            }
            label(message, x: CGFloat(spec.textX),
                  width: w - CGFloat(spec.textX + spec.textRightGap),
                  size: 13, centered: false, color: rgb(spec.labelColor))
        }
        if hovered {
            capsule(NSRect(x: w - 32, y: 11, width: 28, height: 28),
                    radius: 14, color: rgb(spec.closeHover))
        }
        rgb(spec.closeColor).setStroke()
        let cross = NSBezierPath()
        cross.lineWidth = 1.6
        let cx = w - CGFloat(spec.cancelInset)
        cross.move(to: NSPoint(x: cx - 4, y: CGFloat(spec.iconY - 4)))
        cross.line(to: NSPoint(x: cx + 4, y: CGFloat(spec.iconY + 4)))
        cross.move(to: NSPoint(x: cx + 4, y: CGFloat(spec.iconY - 4)))
        cross.line(to: NSPoint(x: cx - 4, y: CGFloat(spec.iconY + 4)))
        cross.stroke()
    }

    private func label(_ message: String, x: CGFloat, width: CGFloat,
                       size: CGFloat, centered: Bool, color: NSColor) {
        let paragraph = NSMutableParagraphStyle()
        paragraph.alignment = centered ? .center : .left
        let text = message as NSString
        // AppKit's unflipped text box sits slightly high relative to the pill's icons.
        // Move the single shared text lane down 2 pt for the clock and all phases.
        text.draw(in: NSRect(x: x, y: 14, width: width, height: 21), withAttributes: [
            .font: NSFont.systemFont(ofSize: size, weight: centered ? .semibold : .medium),
            .foregroundColor: color,
            .paragraphStyle: paragraph
        ])
    }

    private func drawMark() {
        guard let spec else { return }
        let color = accent
        if phase == .processing {
            for index in 0..<spec.spinnerDots {
                let dot = VRSpinner(Int32(index), tick)
                let x = CGFloat(dot & 255)
                let y = CGFloat((dot >> 8) & 255)
                color.withAlphaComponent(CGFloat((dot >> 16) & 255) / 255).setFill()
                let size = CGFloat(spec.spinnerDotSize)
                NSBezierPath(ovalIn: NSRect(x: x - size / 2, y: y - size / 2,
                                            width: size, height: size)).fill()
            }
        } else {
            rgb(phase == .sent ? spec.sentMark :
                phase == .fallback ? spec.fallbackMark : spec.errorAccent).setFill()
            NSBezierPath(ovalIn: NSRect(x: 22, y: 17, width: 16, height: 16)).fill()
            label(phase == .sent ? "✓" : "!", x: 22, width: 16, size: 11,
                  centered: true, color: rgb(spec.attentionGlyph))
        }
    }

    override func updateTrackingAreas() {
        for area in trackingAreas { removeTrackingArea(area) }
        let width = CGFloat(spec?.cancelWidth ?? 0)
        addTrackingArea(NSTrackingArea(rect: NSRect(x: bounds.width - width, y: 0,
                                                   width: width, height: bounds.height),
                                       options: [.mouseEnteredAndExited, .activeAlways],
                                       owner: self))
        super.updateTrackingAreas()
    }

    override func mouseEntered(with event: NSEvent) { hovered = true; needsDisplay = true }
    override func mouseExited(with event: NSEvent) { hovered = false; needsDisplay = true }

    override func mouseDown(with event: NSEvent) {
        if convert(event.locationInWindow, from: nil).x >= bounds.width - CGFloat(spec?.cancelWidth ?? 0) {
            onCancel?()
        }
    }
}
