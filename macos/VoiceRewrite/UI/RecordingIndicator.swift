import AppKit
import SwiftUI

struct IndicatorSnapshot: Equatable {
    var visible = false
    var pending = false
    var phase: IndicatorPhase = .recording
    var mode = "听写"
    var started = Date()
    var changed = Date()
}

@MainActor
final class IndicatorFrame: ObservableObject {
    @Published var snapshot = IndicatorSnapshot()
    @Published var now = Date()
    @Published var level = 0.0
    @Published var tick: UInt32 = 0
}

@MainActor
final class RecordingIndicatorController {
    private let frame = IndicatorFrame()
    private var panel: NSPanel?
    private var timer: Timer?
    private var levelSource: () -> Double = { 0 }
    private var onCancel: () -> Void = {}
    private var onAutoDismiss: () -> Void = {}
    private var smoothed = 0.0

    func present(
        _ snapshot: IndicatorSnapshot,
        levelSource: @escaping () -> Double,
        onCancel: @escaping () -> Void,
        onAutoDismiss: @escaping () -> Void
    ) {
        self.levelSource = levelSource
        self.onCancel = onCancel
        self.onAutoDismiss = onAutoDismiss
        frame.snapshot = snapshot
        frame.now = Date()
        ensurePanel()
        renderFrame()
        startTimer()
        panel?.orderFrontRegardless()
    }

    func dismiss() {
        timer?.invalidate()
        timer = nil
        smoothed = 0
        frame.level = 0
        frame.tick = 0
        var snapshot = frame.snapshot
        snapshot.visible = false
        snapshot.pending = false
        frame.snapshot = snapshot
        panel?.orderOut(nil)
    }

    private func ensurePanel() {
        guard panel == nil else { return }
        let view = NSHostingView(rootView: RecordingIndicatorView(frame: frame, onCancel: { [weak self] in
            self?.onCancel()
        }))
        view.frame = NSRect(x: 0, y: 0, width: IndicatorMath.recordingWidth, height: IndicatorMath.height)
        let created = NSPanel(
            contentRect: view.frame,
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false)
        created.isFloatingPanel = true
        created.level = .statusBar
        created.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
        created.isOpaque = false
        created.backgroundColor = .clear
        created.hasShadow = false
        created.hidesOnDeactivate = false
        created.becomesKeyOnlyIfNeeded = true
        created.isReleasedWhenClosed = false
        created.contentView = view
        panel = created
    }

    private func startTimer() {
        guard timer == nil else { return }
        let timer = Timer(timeInterval: 0.07, repeats: true) { [weak self] _ in
            // This timer is installed on RunLoop.main. Avoid enqueueing a new
            // actor task every 70ms when the main queue is under load.
            MainActor.assumeIsolated { self?.tick() }
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    private func tick() {
        guard frame.snapshot.visible else { return }
        frame.tick &+= 1
        frame.now = Date()
        smoothed = IndicatorMath.smoothedLevel(previous: smoothed, incoming: levelSource())
        frame.level = smoothed
        let alpha = IndicatorMath.statusOpacity(
            started: frame.snapshot.started,
            changed: frame.snapshot.changed,
            now: frame.now,
            phase: frame.snapshot.phase,
            pending: frame.snapshot.pending)
        if IndicatorMath.terminal(frame.snapshot.phase) && alpha == 0 {
            dismiss()
            onAutoDismiss()
            return
        }
        renderFrame()
    }

    private func renderFrame() {
        guard let panel else { return }
        let width = CGFloat(IndicatorMath.width(frame.snapshot.phase))
        let mouse = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(mouse, $0.frame, false) } ?? NSScreen.main
        let visible = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: width, height: 80)
        let area = DesktopWorkArea(
            left: Int32(visible.minX.rounded()),
            top: Int32(visible.minY.rounded()),
            right: Int32(visible.maxX.rounded()),
            bottom: Int32(visible.maxY.rounded()))
        let rawX = Int32((visible.midX - width / 2).rounded())
        let rawY = Int32((visible.minY + 5).rounded())
        let (x, y) = area.clamp(x: rawX, y: rawY, width: Int32(width), height: Int32(IndicatorMath.height))
        let next = NSRect(x: CGFloat(x), y: CGFloat(y),
                          width: width, height: CGFloat(IndicatorMath.height))
        // Repeated setFrame(display: true) forces AppKit layout/repaint on every
        // wave-meter tick. Only move/resize when the target geometry changes.
        if panel.frame != next {
            panel.setFrame(next, display: true)
            panel.contentView?.frame = NSRect(origin: .zero, size: next.size)
        }
    }
}

struct RecordingIndicatorView: View {
    @ObservedObject var frame: IndicatorFrame
    @State private var closeHovered = false
    let onCancel: () -> Void

    var body: some View {
        let snapshot = frame.snapshot
        let width = CGFloat(IndicatorMath.width(snapshot.phase))
        let accent = accentColor(snapshot)
        ZStack(alignment: .topLeading) {
            // Same layered 3/8 and 4/7 shadow offsets as the Win32 pill.
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .fill(Color.black.opacity(17.0 / 255))
                .frame(width: width - 6, height: 40)
                .position(x: width / 2, y: 28)
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .fill(Color.black.opacity(34.0 / 255))
                .frame(width: width - 8, height: 40)
                .position(x: width / 2, y: 27)
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .fill(Color(indicator: IndicatorMath.pill))
                .padding(.horizontal, 5)
                .padding(.vertical, 5)
            if snapshot.phase == .recording {
                Capsule().fill(accent).frame(width: 3, height: 14).position(x: 13.5, y: 25)
                Circle().fill(accent).frame(width: 8, height: 8).position(x: 27, y: CGFloat(IndicatorMath.iconCenterY))
                ForEach(0..<9, id: \.self) { index in
                    let bar = IndicatorMath.waveHeight(index: index, level: frame.level, tick: frame.tick)
                    Capsule()
                        .fill(accent)
                        .frame(width: CGFloat(IndicatorMath.waveBarWidth), height: CGFloat(bar))
                        .position(
                            x: CGFloat(IndicatorMath.waveformX(index)) + CGFloat(IndicatorMath.waveBarWidth) / 2,
                            y: CGFloat(IndicatorMath.iconCenterY))
                }
                Text(IndicatorMath.durationText(started: snapshot.started, now: frame.now))
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(Color(indicator: IndicatorMath.clockText))
                    .position(x: CGFloat(IndicatorMath.clockX + IndicatorMath.clockWidth / 2), y: 25)
            } else {
                phaseMark(snapshot, accent: accent, tick: frame.tick)
                    .position(x: 30, y: CGFloat(IndicatorMath.iconCenterY))
                Text(IndicatorMath.label(snapshot.phase, mode: snapshot.mode))
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(Color(indicator: IndicatorMath.labelText))
                    .lineLimit(1)
                    .frame(width: width - CGFloat(IndicatorMath.workingTextX + IndicatorMath.workingTextRightGap), alignment: .leading)
                    .position(
                        x: CGFloat(IndicatorMath.workingTextX) + (width - CGFloat(IndicatorMath.workingTextX + IndicatorMath.workingTextRightGap)) / 2,
                        y: 25)
            }
            Button(action: onCancel) {
                ZStack {
                    if closeHovered {
                        Circle()
                            .fill(Color(indicator: IndicatorMath.closeHover))
                            .frame(width: 28, height: 28)
                    }
                    Image(systemName: "xmark")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(Color(indicator: IndicatorMath.closeGlyph))
                }
                .frame(width: CGFloat(IndicatorMath.cancelWidth), height: CGFloat(IndicatorMath.height))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .onHover { closeHovered = $0 }
            .position(x: width - CGFloat(IndicatorMath.cancelWidth) / 2, y: CGFloat(IndicatorMath.height) / 2)
            .accessibilityLabel("取消")
        }
        .frame(width: width, height: CGFloat(IndicatorMath.height))
        .opacity(Double(IndicatorMath.statusOpacity(
            started: snapshot.started, changed: snapshot.changed, now: frame.now,
            phase: snapshot.phase, pending: snapshot.pending)) / 255)
    }

    private func accentColor(_ snapshot: IndicatorSnapshot) -> Color {
        Color(indicator: IndicatorMath.accent(phase: snapshot.phase, mode: snapshot.mode))
    }

    @ViewBuilder
    private func phaseMark(_ snapshot: IndicatorSnapshot, accent: Color, tick: UInt32) -> some View {
        if IndicatorMath.terminal(snapshot.phase) {
            let color = Color(indicator: IndicatorMath.terminalMark(snapshot.phase))
            ZStack {
                Circle().fill(color).frame(width: 16, height: 16)
                if snapshot.phase == .error || snapshot.phase == .fallback {
                    Text("!").font(.system(size: 11, weight: .bold)).foregroundStyle(Color(indicator: IndicatorMath.attentionGlyph))
                } else {
                    Image(systemName: "checkmark").font(.system(size: 9, weight: .bold)).foregroundStyle(Color(indicator: IndicatorMath.checkGlyph))
                }
            }
        } else {
            ForEach(0..<IndicatorMath.spinnerDots, id: \.self) { index in
                let dot = IndicatorMath.spinnerDot(index: index, tick: tick)
                Circle()
                    .fill(accent.opacity(Double(dot.alpha) / 255))
                    .frame(width: CGFloat(IndicatorMath.spinnerDotSize), height: CGFloat(IndicatorMath.spinnerDotSize))
                    .position(x: CGFloat(dot.x), y: CGFloat(dot.y))
            }
        }
    }
}

extension Color {
    init(indicator rgb: IndicatorRGB) {
        self.init(red: Double(rgb.r) / 255, green: Double(rgb.g) / 255, blue: Double(rgb.b) / 255)
    }
}
