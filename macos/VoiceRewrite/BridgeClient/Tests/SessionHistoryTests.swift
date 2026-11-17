import VoiceRewriteStorage
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

    @MainActor
    func testMainWindowCloseDoesNotTerminateAndLaunchRunsOnce() {
        let delegate = AppLifecycleDelegate()
        var launches = 0
        delegate.launchWorkspaceOnce { launches += 1 }
        delegate.launchWorkspaceOnce { launches += 1 }
        XCTAssertEqual(launches, 1)
        XCTAssertFalse(delegate.applicationShouldTerminateAfterLastWindowClosed(NSApplication.shared))
        // Finder reopen may reactivate an existing window or let SwiftUI create it.
        // No additional launch() call is needed to preserve the menu-bar model.
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
    func testMenuTracksIndicatorPhasesWithoutReplacingSharedDuration() {
        func title(_ phase: IndicatorPhase, mode: String = "听写") -> String {
            MenuBarStatus.title(paused: false, recording: false, starting: false,
                                processing: false, meetingActive: false, phase: phase, mode: mode)
        }
        func symbol(_ phase: IndicatorPhase) -> String {
            MenuBarStatus.symbol(paused: false, recording: false, starting: false,
                                 processing: false, meetingActive: false,
                                 connectionFailed: true, phase: phase)
        }
        XCTAssertEqual(MenuBarStatus.title(paused: false, recording: false, starting: false,
            processing: false, meetingActive: false, connectionFailed: true), "服务连接失败 · 查看诊断")
        XCTAssertEqual(title(.processing), "正在识别")
        XCTAssertEqual(title(.processing, mode: "改写"), "正在识别指令")
        XCTAssertEqual(title(.rewriting), "正在改写")
        XCTAssertEqual(title(.selection), "正在读取选区")
        XCTAssertEqual(title(.success), "语音任务完成")
        XCTAssertEqual(title(.error), "语音任务失败")
        XCTAssertEqual(title(.fallback), "结果已保留 · 可复制")
        XCTAssertEqual(symbol(.success), "checkmark.circle.fill")
        XCTAssertEqual(symbol(.error), "xmark.octagon.fill")
        XCTAssertEqual(symbol(.rewriting), "arrow.triangle.2.circlepath.circle.fill")
        XCTAssertEqual(symbol(.fallback), "doc.on.doc.fill")
        XCTAssertEqual(MenuBarStatus.symbol(paused: true, recording: false, starting: false,
            processing: false, meetingActive: false, connectionFailed: true), "pause.circle.fill")
    }

    func testFirstRunSettingsAreDetectedWithoutWritingSettings() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoiceRewriteOnboarding-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let path = directory.appendingPathComponent("settings.json")
        XCTAssertTrue(InitialSetup.required(settingsFile: path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: path.path))
        try Data("{}".utf8).write(to: path)
        XCTAssertFalse(InitialSetup.required(settingsFile: path))
    }

    @MainActor
    func testGlobalQuitIsIndependentOfBothVoiceHotkeys() {
        XCTAssertEqual(MacSettings.defaults.exitShortcut, "Ctrl+Alt+F10")
        let exit = ShortcutBinding(canonical: MacSettings.defaults.exitShortcut)
        XCTAssertNotNil(exit)
        XCTAssertFalse(exit?.mouse ?? true)
        let native = MacShortcut.hotKey(key: exit!.key, ctrl: exit!.ctrl,
                                       alt: exit!.alt, shift: exit!.shift)
        XCTAssertNotNil(native)
        let dictation = MacShortcut.hotKey(key: "R", ctrl: true, alt: true, shift: false)
        let rewrite = MacShortcut.hotKey(key: "E", ctrl: true, alt: true, shift: false)
        XCTAssertNotEqual(native, dictation)
        XCTAssertNotEqual(native, rewrite)
    }

    func testUpdatePageAllowsOnlyOfficialHTTPSReleaseURLs() {
        let valid = "https://github.com/xujinheng/windows-voice-rewrite/releases"
        XCTAssertEqual(UpdatePagePolicy.officialReleaseURL(valid)?.absoluteString, valid)
        XCTAssertNotNil(UpdatePagePolicy.officialReleaseURL(valid + "/tag/v1.2.3"))
        for attack in [
            "http://github.com/xujinheng/windows-voice-rewrite/releases",
            "https://github.com.evil.example/xujinheng/windows-voice-rewrite/releases",
            "https://github.com/xujinheng/windows-voice-rewrite/releases.evil",
            "https://github.com/xujinheng/windows-voice-rewrite/releases?download=1",
            "https://evil:password@github.com/xujinheng/windows-voice-rewrite/releases"
        ] {
            XCTAssertNil(UpdatePagePolicy.officialReleaseURL(attack), attack)
        }
    }

}
