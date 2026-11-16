import XCTest
@testable import VoiceRewrite

final class IndicatorMathTests: XCTestCase {
    func testSharedGeometryAndLabels() {
        XCTAssertEqual(IndicatorMath.height, 50)
        XCTAssertEqual(IndicatorMath.width(.recording), 232)
        XCTAssertEqual(IndicatorMath.width(.rewriting), 252)
        XCTAssertEqual(IndicatorMath.label(.processing, mode: "听写"), "正在转写…")
        XCTAssertEqual(IndicatorMath.label(.processing, mode: "改写"), "识别指令…")
        XCTAssertEqual(IndicatorMath.label(.selection, mode: "改写"), "读取选区…")
        XCTAssertEqual(IndicatorMath.label(.rewriting, mode: "改写"), "AI 处理中…")
        XCTAssertEqual(IndicatorMath.label(.applying, mode: "改写"), "正在替换…")
        XCTAssertEqual(IndicatorMath.label(.success, mode: "听写"), "已发送粘贴指令")
        XCTAssertEqual(IndicatorMath.label(.answer, mode: "改写"), "已显示结果")
        XCTAssertEqual(IndicatorMath.label(.fallback, mode: "听写"), "结果已保留 · 可复制")
        XCTAssertEqual(IndicatorMath.label(.error, mode: "改写"), "操作未完成 · 查看日志")
        XCTAssertEqual(IndicatorMath.label(.recording, mode: "听写"), "")
        XCTAssertEqual(IndicatorMath.accent(phase: .error, mode: "听写"), IndicatorMath.errorAccent)
        XCTAssertEqual(IndicatorMath.accent(phase: .recording, mode: "改写"), IndicatorMath.rewriteAccent)
        XCTAssertEqual(IndicatorMath.accent(phase: .recording, mode: "听写"), IndicatorMath.dictationAccent)
        XCTAssertEqual(IndicatorMath.terminalMark(.answer), IndicatorMath.answerMark)
        XCTAssertEqual(IndicatorMath.terminalMark(.success), IndicatorMath.successMark)
        XCTAssertEqual(IndicatorMath.checkGlyph, IndicatorRGB(r: 0x19, g: 0x34, b: 0x2A))
        let origin = IndicatorMath.spinnerDot(index: 0, tick: 0)
        XCTAssertEqual(origin.x, 38)
        XCTAssertEqual(origin.y, 25)
        XCTAssertEqual(origin.alpha, 65)
        let bead = IndicatorMath.spinnerDot(index: 1, tick: 0)
        XCTAssertEqual(bead.x, 36)
        XCTAssertEqual(bead.y, 31)
        XCTAssertEqual(bead.alpha, 90)
        XCTAssertTrue(IndicatorMath.terminal(.answer))
        XCTAssertFalse(IndicatorMath.terminal(.rewriting))
        XCTAssertLessThan(
            IndicatorMath.waveformX(8) + IndicatorMath.waveBarWidth + 8,
            IndicatorMath.clockX)
    }

    func testOpacityMatchesCoreTiming() {
        let start = Date(timeIntervalSince1970: 100)
        XCTAssertEqual(IndicatorMath.opacity(started: start, changed: start, now: start, phase: .recording), 0)
        XCTAssertEqual(
            IndicatorMath.opacity(started: start, changed: start, now: start.addingTimeInterval(IndicatorMath.intro), phase: .recording),
            255)
        let change = start.addingTimeInterval(1)
        XCTAssertEqual(IndicatorMath.opacity(started: start, changed: change, now: change, phase: .processing), 225)
        XCTAssertEqual(
            IndicatorMath.opacity(started: start, changed: change, now: change.addingTimeInterval(IndicatorMath.phaseEase), phase: .processing),
            255)
        let end = start.addingTimeInterval(2)
        XCTAssertEqual(
            IndicatorMath.opacity(started: start, changed: end, now: end.addingTimeInterval(IndicatorMath.terminalDuration), phase: .success),
            0)
        XCTAssertEqual(IndicatorMath.statusOpacity(started: start, changed: start, now: start, phase: .recording, pending: true), 255)
        XCTAssertEqual(IndicatorMath.statusOpacity(started: start, changed: start, now: start, phase: .recording, pending: false), 0)
    }

    func testBarsAndMicrophoneGainStayBounded() {
        for index in 0..<9 {
            for level in [-1.0, 0, 0.1, 0.5, 1, 3] {
                let height = IndicatorMath.waveHeight(index: index, level: level, tick: 120)
                XCTAssertGreaterThanOrEqual(height, 4)
                XCTAssertLessThanOrEqual(height, 18)
            }
        }
        XCTAssertEqual(IndicatorMath.normalizeMicrophoneLevel(0.1), 0.5)
        XCTAssertEqual(IndicatorMath.normalizeMicrophoneLevel(1), 1)
        XCTAssertEqual(IndicatorMath.normalizeMicrophoneLevel(-1), 0)
        let started = Date(timeIntervalSince1970: 0)
        XCTAssertEqual(IndicatorMath.durationText(started: started, now: started.addingTimeInterval(61)), "01:01")
        XCTAssertEqual(IndicatorMath.durationText(started: started, now: started.addingTimeInterval(-5)), "00:00")
    }
}
