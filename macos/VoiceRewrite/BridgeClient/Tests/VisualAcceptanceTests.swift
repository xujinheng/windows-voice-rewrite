import AppKit
import SwiftUI
import XCTest
@testable import VoiceRewrite

// Off-screen renders of production AppKit/SwiftUI views. No hardware input,
// third-party editor, ASR or meeting capture is involved.
final class VisualAcceptanceTests: XCTestCase {
    func testSettingsViewportMatchesStandardSettingsWindow() {
        XCTAssertEqual(SettingsWindowMetrics.width, 860)
        XCTAssertEqual(SettingsWindowMetrics.height, 530)
        XCTAssertEqual(SettingsWindowMetrics.minimumSize.width, 480)
        XCTAssertEqual(SettingsWindowMetrics.minimumSize.height, 420)
    }

    struct Capture: Encodable {
        let state: String
        let fixture: String
        let theme: String
        let locale = "zh-CN"
        let scale = "logical-1x"
        let image: String
    }
    struct Manifest: Encodable {
        let schema = 1
        let platform: String
        let classification = "simulated-ui"
        let captures: [Capture]
    }

    @MainActor
    func testProductionViewStateScreenshots() async throws {
        let environment = ProcessInfo.processInfo.environment
        // Xcode does not reliably forward the parent process environment to its
        // XCTest runner. A repository-local sentinel scopes capture to this CI job.
        let repository = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
        guard FileManager.default.fileExists(atPath:
            repository.appendingPathComponent("acceptance-output/.enabled").path) else {
            throw XCTSkip("visual evidence capture is enabled only in the acceptance workflow")
        }
        #if arch(arm64)
        let nativePlatform = "macos-arm64"
        #else
        let nativePlatform = "macos-x64"
        #endif
        let platform = environment["VOICEREWRITE_ACCEPTANCE_PLATFORM"] ?? nativePlatform
        let directory = repository.appendingPathComponent("acceptance-output/\(platform)",
            isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        var captures: [Capture] = []

        func store<V: View>(_ state: String, fixture: String, theme: String,
                             width: CGFloat, height: CGFloat, content: V) throws {
            // SwiftUI must be hosted by an AppKit window for reliable CI painting.
            let view = NSHostingView(rootView: content)
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: width, height: height),
                                  styleMask: [.borderless], backing: .buffered, defer: false)
            window.contentView = view
            view.frame = NSRect(x: 0, y: 0, width: width, height: height)
            window.orderFrontRegardless()
            RunLoop.main.run(until: Date().addingTimeInterval(0.06))
            view.layoutSubtreeIfNeeded()
            view.displayIfNeeded()
            guard let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds) else {
                XCTFail("Unable to snapshot production view for \(state)")
                return
            }
            view.cacheDisplay(in: view.bounds, to: bitmap)
            guard let data = bitmap.representation(using: .png, properties: [:]), data.count > 200 else {
                XCTFail("Empty PNG for \(state)")
                return
            }
            window.orderOut(nil)
            let name = state.replacingOccurrences(of: ".", with: "-") + "-" + theme + ".png"
            try data.write(to: directory.appendingPathComponent(name), options: .atomic)
            let attachment = XCTAttachment(data: data, uniformTypeIdentifier: "public.png")
            attachment.name = state
            attachment.lifetime = .keepAlways
            add(attachment)
            captures.append(Capture(state: state, fixture: fixture, theme: theme, image: name))
        }

        let model = WorkspaceModel() // no launch(), mic, permissions or helper connection
        for page in ["speech", "writing", "input"] {
            try store("settings.\(page)", fixture: "default-settings-v1", theme: "light",
                      width: SettingsWindowMetrics.width, height: SettingsWindowMetrics.height,
                      content: SettingsView(model: model, initialPage: page)
                          .environment(\.colorScheme, .light))
        }

        let started = Date(timeIntervalSince1970: 100)
        let phases: [(String, IndicatorPhase, Bool)] = [
            ("pill.preview", .recording, true),
            ("pill.recording", .recording, false),
            ("pill.processing", .processing, false),
            ("pill.failure", .error, false),
            ("pill.success-event-sent", .success, false)
        ]
        for (state, phase, pending) in phases {
            let frame = IndicatorFrame()
            frame.snapshot = IndicatorSnapshot(visible: true, pending: pending, phase: phase,
                mode: "听写", started: started, changed: started)
            frame.now = started.addingTimeInterval(0.4)
            frame.level = 0.6
            frame.tick = 11
            try store(state, fixture: "synthetic-dictation-v1", theme: "dark",
                      width: CGFloat(IndicatorMath.width(phase)),
                      height: CGFloat(IndicatorMath.height),
                      content: RecordingIndicatorView(frame: frame, onCancel: {})
                          .environment(\.colorScheme, .dark))
        }

        let text = "这是 VoiceRewrite 的真实结果卡片。\n\n使用本地模型可以完成：\n• 改写与润色\n• 翻译与总结\n• 对选中文本提问\n\n所有内容均为视觉验收用的固定示例文字。"
        for theme in ["light", "dark"] {
            for editable in [false, true] {
                // An inert authorized action exercises the real Apply/Copy
                // layout, but never pastes into a real editor in CI.
                let applyAction: ResultCardApplyAction? = editable
                    ? ResultCardApplyAction(isBusy: { false }, perform: {}, cancel: {})
                    : nil
                let card = ResultCardActionSession(text: text, applyAction: applyAction)
                let height = CGFloat(ResultCardMetrics.height(unicodeScalars: text.unicodeScalars.count))
                try store(editable ? "result.apply" : "result.copy",
                          fixture: editable ? "result-apply-v1" : "result-v1", theme: theme,
                          width: ResultCardMetrics.width, height: height,
                          content: ResultCardView(title: editable ? "AI 改写结果" : "AI 回答",
                              session: card, width: ResultCardMetrics.width, height: height,
                              onCopy: {}, onApply: {}, onClose: {})
                              .environment(\.colorScheme, theme == "dark" ? .dark : .light))
            }
        }

        // The empty state is not a substitute for Windows's populated demo.
        try store("meeting.empty", fixture: "empty-meeting-v1", theme: "light",
                  width: 640, height: 720,
                  content: MeetingReaderView(model: model, embedded: false)
                      .environment(\.colorScheme, .light))

        // Exercise the actual Go Core offline fixture through bundled voicebridge.
        // No microphone, ScreenCaptureKit or cloud model calls are made.
        model.connect()
        defer { model.shutdown() }
        for _ in 0..<80 {
            if case .connected = model.connection { break }
            try await Task.sleep(nanoseconds: 100_000_000)
        }
        guard case .connected = model.connection else {
            XCTFail("Go voicebridge is unavailable for populated Meeting screenshots")
            return
        }
        model.startMeetingDemo()
        for _ in 0..<80 {
            if model.meetingDemoActive { break }
            try await Task.sleep(nanoseconds: 100_000_000)
        }
        guard model.meetingDemoActive, !model.meetingCaptions.isEmpty else {
            XCTFail("Go Core did not return the populated Meeting demo fixture")
            return
        }
        let fontKey = "meeting.reader.fontSize"
        let previousFont = UserDefaults.standard.object(forKey: fontKey)
        defer {
            if let previousFont { UserDefaults.standard.set(previousFont, forKey: fontKey) }
            else { UserDefaults.standard.removeObject(forKey: fontKey) }
        }
        UserDefaults.standard.set(16, forKey: fontKey)
        func storeMeeting(_ state: String) throws {
            try store(state, fixture: "populated-demo-v1", theme: "light",
                      width: 640, height: 720,
                      content: MeetingReaderView(model: model, embedded: false)
                          .environment(\.colorScheme, .light))
        }
        try storeMeeting("meeting.full")
        model.toggleMeetingCaptionsOnly()
        XCTAssertTrue(model.meetingCaptionsOnly)
        try storeMeeting("meeting.focus")
        model.toggleMeetingCaptionsOnly()
        model.toggleMeetingExpressionExpanded()
        XCTAssertTrue(model.meetingExpressionExpanded)
        try storeMeeting("meeting.expanded")
        model.toggleMeetingExpressionExpanded()
        UserDefaults.standard.set(22, forKey: fontKey)
        try storeMeeting("meeting.large-type")

        let manifest = Manifest(platform: platform, captures: captures)
        try JSONEncoder().encode(manifest).write(
            to: directory.appendingPathComponent("manifest.json"), options: .atomic)
        XCTAssertEqual(captures.count, 17)
    }
}
