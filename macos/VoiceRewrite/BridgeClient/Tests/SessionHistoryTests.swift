import XCTest
@testable import VoiceRewrite

final class SessionHistoryTests: XCTestCase {
    func testKeepsTenNewestAndDropsOversizedText() {
        var history = SessionHistory()
        for index in 0..<14 {
            history.remember(title: "听写结果", value: String(format: "内容%02d", index))
        }
        XCTAssertEqual(history.entries.count, SessionHistory.limit)
        XCTAssertEqual(history.entries.first?.text, "内容13")
        XCTAssertEqual(history.entries.last?.text, "内容04")
        XCTAssertNotNil(history.entry(id: history.entries[0].id))
        XCTAssertNil(history.entry(id: 1))
        history.remember(title: "", value: "   ")
        XCTAssertEqual(history.entries.count, SessionHistory.limit)
        history.remember(title: "大结果", value: String(repeating: "x", count: SessionHistory.maxBytes + 1))
        XCTAssertEqual(history.entries.count, SessionHistory.limit)
        history.clear()
        XCTAssertTrue(history.entries.isEmpty)
    }

    func testMainWindowCloseDoesNotTerminateAndLaunchRunsOnce() {
        let delegate = AppLifecycleDelegate()
        var launches = 0
        delegate.launchWorkspaceOnce { launches += 1 }
        delegate.launchWorkspaceOnce { launches += 1 }
        XCTAssertEqual(launches, 1)
        XCTAssertFalse(delegate.applicationShouldTerminateAfterLastWindowClosed(NSApplication.shared))
        XCTAssertTrue(delegate.applicationShouldHandleReopen(NSApplication.shared, hasVisibleWindows: false))
    }

    func testMenuStatusDoesNotMisreportPausedMeetingOrProcessing() {
        XCTAssertEqual(MenuBarStatus.title(paused: false, recording: false, starting: false, processing: false, meetingActive: false), "待命")
        XCTAssertEqual(MenuBarStatus.title(paused: true, recording: false, starting: false, processing: false, meetingActive: true), "会议进行中 · 普通语音已暂停")
        XCTAssertEqual(MenuBarStatus.title(paused: false, recording: true, starting: false, processing: false, meetingActive: false), "正在录音")
        XCTAssertEqual(MenuBarStatus.title(paused: false, recording: false, starting: false, processing: true, meetingActive: true), "正在处理语音")
        XCTAssertEqual(MenuBarStatus.symbol(paused: false, recording: false, starting: false, processing: false, meetingActive: false, connectionFailed: false), "waveform")
        XCTAssertEqual(MenuBarStatus.symbol(paused: true, recording: false, starting: false, processing: false, meetingActive: false, connectionFailed: false), "pause.circle.fill")
        XCTAssertEqual(MenuBarStatus.symbol(paused: false, recording: true, starting: false, processing: false, meetingActive: false, connectionFailed: false), "mic.fill")
        XCTAssertEqual(MenuBarStatus.symbol(paused: false, recording: false, starting: false, processing: false, meetingActive: true, connectionFailed: false), "person.2.wave.2.fill")
        XCTAssertEqual(MenuBarStatus.symbol(paused: false, recording: false, starting: false, processing: false, meetingActive: false, connectionFailed: true), "exclamationmark.triangle.fill")
    }

    func testMenuLineShowsKindAndTimeOnly() {
        var components = DateComponents()
        components.year = 2026
        components.month = 1
        components.day = 2
        components.hour = 15
        components.minute = 4
        let created = Calendar(identifier: .gregorian).date(from: components)!
        let dictation = SessionHistory.menuLine(index: 0, title: "听写结果", createdAt: created)
        let answer = SessionHistory.menuLine(index: 2, title: "AI translate", createdAt: created)
        XCTAssertEqual(dictation, "1. 听写 · 15:04")
        XCTAssertEqual(answer, "3. AI 结果 · 15:04")
        XCTAssertFalse(dictation.contains("内容"))
        XCTAssertFalse(answer.contains("secret"))
    }
}
