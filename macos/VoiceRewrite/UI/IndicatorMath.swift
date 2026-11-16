import Foundation

/// Portable recording-pill model. Numeric results match `internal/core/indicator`.
struct IndicatorRGB: Equatable {
    let r, g, b: UInt8
}

enum IndicatorPhase: Int {
    case recording = 1
    case processing = 2
    case success = 3
    case error = 4
    case rewriting = 5
    case applying = 6
    case answer = 7
    case selection = 8
    case fallback = 9
}

enum IndicatorMath {
    static let height = 50
    static let recordingWidth = 232
    static let workingWidth = 252
    static let cancelWidth = 36
    static let waveBarWidth = 3
    static let clockX = 145
    static let clockWidth = 50
    static let cancelGlyphInset = 19
    static let iconCenterY = 25
    static let workingTextX = 53
    static let workingTextRightGap = 38
    static let intro: TimeInterval = 0.170
    static let phaseEase: TimeInterval = 0.160
    static let terminalDuration: TimeInterval = 1.750
    static let outro: TimeInterval = 0.240
    static let microphoneGain = 5.0
    static let spinnerDots = 8
    static let spinnerCenterX = 30
    static let spinnerRadius = 8
    static let spinnerDotSize = 4

    static let pill = IndicatorRGB(r: 0x20, g: 0x24, b: 0x2E)
    static let clockText = IndicatorRGB(r: 0xF5, g: 0xF3, b: 0xF7)
    static let labelText = IndicatorRGB(r: 0xEC, g: 0xE8, b: 0xF0)
    static let dictationAccent = IndicatorRGB(r: 0xE9, g: 0x81, b: 0xAA)
    static let rewriteAccent = IndicatorRGB(r: 0xAF, g: 0xA5, b: 0xFF)
    static let errorAccent = IndicatorRGB(r: 0xFF, g: 0x81, b: 0x8A)
    static let successMark = IndicatorRGB(r: 0x8D, g: 0xBD, b: 0xE8)
    static let answerMark = IndicatorRGB(r: 0x78, g: 0xD9, b: 0xAD)
    static let fallbackMark = IndicatorRGB(r: 0xF5, g: 0xC5, b: 0x6B)
    static let attentionGlyph = IndicatorRGB(r: 0x38, g: 0x19, b: 0x1D)
    static let checkGlyph = IndicatorRGB(r: 0x19, g: 0x34, b: 0x2A)
    static let closeGlyph = IndicatorRGB(r: 0xAA, g: 0xA5, b: 0xB3)

    static func accent(phase: IndicatorPhase, mode: String) -> IndicatorRGB {
        if phase == .error { return errorAccent }
        if mode == "改写" { return rewriteAccent }
        return dictationAccent
    }

    static func terminalMark(_ phase: IndicatorPhase) -> IndicatorRGB {
        switch phase {
        case .answer: return answerMark
        case .error: return errorAccent
        case .fallback: return fallbackMark
        default: return successMark
        }
    }

    static func spinnerDot(index: Int, tick: UInt32) -> (x: Int, y: Int, alpha: UInt8) {
        let angle = Double(tick) * 0.2 + Double(index) * .pi / 4
        let x = Int((Double(spinnerCenterX) + Double(spinnerRadius) * cos(angle)).rounded())
        let y = Int((Double(iconCenterY) + Double(spinnerRadius) * sin(angle)).rounded())
        let value = min(255, 65 + index * 25)
        return (x, y, UInt8(value))
    }

    static func width(_ phase: IndicatorPhase) -> Int {
        phase == .recording ? recordingWidth : workingWidth
    }

    static func terminal(_ phase: IndicatorPhase) -> Bool {
        switch phase {
        case .success, .error, .answer, .fallback: return true
        default: return false
        }
    }

    static func label(_ phase: IndicatorPhase, mode: String) -> String {
        switch phase {
        case .processing: return mode == "改写" ? "识别指令…" : "正在转写…"
        case .selection: return "读取选区…"
        case .rewriting: return "AI 处理中…"
        case .applying: return "正在替换…"
        case .success: return "已发送粘贴指令"
        case .answer: return "已显示结果"
        case .error: return "操作未完成 · 查看日志"
        case .fallback: return "已复制 · 请手动粘贴"
        case .recording: return ""
        }
    }

    static func durationText(started: Date, now: Date) -> String {
        var sec = Int(now.timeIntervalSince(started))
        if sec < 0 { sec = 0 }
        return String(format: "%02d:%02d", sec / 60, sec % 60)
    }

    static func waveformX(_ index: Int) -> Int { 52 + index * 8 }

    static func waveHeight(index: Int, level: Double, tick: UInt32) -> Int {
        var level = level
        if level < 0 { level = 0 }
        if level > 1 { level = 1 }
        let envelope = 0.44 + 0.56 * abs(sin(Double(index) * 0.75 + Double(tick) * 0.24))
        var height = Int((4 + 14 * pow(level, 0.7) * envelope).rounded())
        if height < 4 { height = 4 }
        if height > 18 { height = 18 }
        return height
    }

    static func opacity(started: Date, changed: Date, now: Date, phase: IndicatorPhase) -> UInt8 {
        if now < started { return 0 }
        var age = now.timeIntervalSince(started)
        if age < intro {
            return UInt8((255 * age / intro).rounded())
        }
        if terminal(phase) {
            age = now.timeIntervalSince(changed)
            if age >= terminalDuration { return 0 }
            if age > terminalDuration - outro {
                let left = terminalDuration - age
                return UInt8((255 * left / outro).rounded())
            }
        }
        let elapsed = now.timeIntervalSince(changed)
        if elapsed >= 0 && elapsed < phaseEase {
            return UInt8(225.0 + (30 * elapsed / phaseEase).rounded())
        }
        return 255
    }

    static func statusOpacity(started: Date, changed: Date, now: Date, phase: IndicatorPhase, pending: Bool) -> UInt8 {
        pending ? 255 : opacity(started: started, changed: changed, now: now, phase: phase)
    }

    static func smoothedLevel(previous: Double, incoming: Double) -> Double {
        let incoming = min(1, max(0, incoming))
        let previous = min(1, max(0, previous))
        return previous * 0.72 + incoming * 0.28
    }

    static func normalizeMicrophoneLevel(_ raw: Double) -> Double {
        guard raw >= 0, raw.isFinite else { return 0 }
        return min(1, raw * microphoneGain)
    }
}
