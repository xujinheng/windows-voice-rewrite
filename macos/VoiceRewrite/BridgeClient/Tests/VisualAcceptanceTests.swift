import AppKit
import SwiftUI
import XCTest
@testable import VoiceRewrite

// Off-screen renders of production AppKit/SwiftUI views. No hardware input,
// third-party editor, ASR or meeting capture is involved.
final class VisualAcceptanceTests: XCTestCase {
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
    func testProductionViewStateScreenshots() throws {
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
        let directory = repository.appendingPathComponent("acceptance-output/\\(platform)",
            isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        var captures: [Capture] = []

        func store<V: View>(_ state: String, fixture: String, theme: String,
                             width: CGFloat, height: CGFloat, content: V) throws {
            let view = NSHostingView(rootView: content)
            view.frame = NSRect(x: 0, y: 0, width: width, height: height)
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
            let name = state.replacingOccurrences(of: ".", with: "-") + "-" + theme + ".png"
            try data.write(to: directory.appendingPathComponent(name), options: .atomic)
            let attachment = XCTAttachment(data: data, uniformTypeIdentifier: "public.png")
            attachment.name = state
            attachment.lifetime = .keepAlways
            add(attachment)
            captures.append(Capture(state: state, fixture: fixture, theme: theme, image: name))
        }

        let model = WorkspaceModel() // no launch(), mic, permissions or helper connection
        try store("settings.speech", fixture: "default-settings-v1", theme: "light",
                  width: 980, height: 700,
                  content: SettingsView(model: model).environment(\.colorScheme, .light))

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
            let card = ResultCardActionSession(text: text)
            try store("result.copy", fixture: "result-v1", theme: theme,
                      width: ResultCardMetrics.width, height: 340,
                      content: ResultCardView(title: "AI 回答", session: card,
                          width: ResultCardMetrics.width, height: 340,
                          onCopy: {}, onApply: {}, onClose: {})
                      .environment(\.colorScheme, theme == "dark" ? .dark : .light))
        }

        // Empty Meeting is not equivalent to the populated Windows demo.
        try store("meeting.empty", fixture: "empty-meeting-v1", theme: "light",
                  width: 850, height: 800,
                  content: MeetingReaderView(model: model, embedded: false)
                      .environment(\.colorScheme, .light))
        let manifest = Manifest(platform: platform, captures: captures)
        try JSONEncoder().encode(manifest).write(
            to: directory.appendingPathComponent("manifest.json"), options: .atomic)
        XCTAssertEqual(captures.count, 9)
    }
}
