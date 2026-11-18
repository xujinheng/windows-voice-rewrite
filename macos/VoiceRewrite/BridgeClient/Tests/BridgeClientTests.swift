import Foundation
import XCTest
import AppKit
import Microphone
import VoiceRewriteInteraction
import Darwin
@testable import VoiceRewrite

final class BridgeClientTests: XCTestCase {
    private func bundledHelper() throws -> URL {
        guard let hostExecutable = Bundle.main.executableURL else {
            XCTFail("Test host executable not found")
            throw BridgeFailure.helperMissing
        }
        let executable = hostExecutable.deletingLastPathComponent()
            .appendingPathComponent("voicebridge")
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            XCTFail("Bundled Go helper missing: \(executable.path)")
            throw BridgeFailure.helperMissing
        }
        return executable
    }

    func testSpokenSelectionRequestAndGuardedResponse() throws {
        let request = try BridgeRequest(id: "voice-selected", operation: "selected_voice_task",
            fields: [
                "config": .object(["asr_base_url": .string("http://127.0.0.1:8080/v1")]),
                "selected_text": .string("原文"),
                "pcm_base64": .string("AAAAAA=="),
            ]).line()
        let payload = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(request.dropLast()))
                                    as? [String: Any])
        XCTAssertEqual(payload["op"] as? String, "selected_voice_task")
        XCTAssertEqual(payload["selected_text"] as? String, "原文")
        XCTAssertNil(payload["instruction"])

        let response = Data(#"{"v":1,"id":"voice-selected","ok":true,"result":{"instruction":"请翻译成英文","intent":"translate","text":"Original","preview_only":false,"may_attempt_replace":true}}"#.utf8)
        let reply = try JSONDecoder().decode(BridgeReply.self, from: response)
        try reply.validate()
        XCTAssertEqual(reply.result?["instruction"]?.string, "请翻译成英文")
        XCTAssertEqual(reply.result?["may_attempt_replace"]?.boolValue, true)
        XCTAssertEqual(reply.result?["preview_only"]?.boolValue, false)
    }

    func testSharedCoreProbeOperationsAndResponses() throws {
        for operation in ["probe_asr", "probe_llm"] {
            let request = try BridgeRequest(id: operation, operation: operation,
                                            fields: ["config": .object(["asr_base_url":
                                                .string("http://127.0.0.1:8080/v1")])]).line()
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(request.dropLast()))
                                       as? [String: Any])
            XCTAssertEqual(object["op"] as? String, operation)
            XCTAssertEqual(object["v"] as? Int, 1)
            XCTAssertNil(object["selected_text"])
            XCTAssertNil(object["pcm_base64"])
        }

        let data = Data(#"{"v":1,"id":"probe-asr","ok":true,"result":{"kind":"ASR","message":"ASR：静音测试成功"}}"#.utf8)
        let reply = try JSONDecoder().decode(BridgeReply.self, from: data)
        try reply.validate()
        XCTAssertEqual(reply.result?["kind"]?.string, "ASR")
        XCTAssertTrue(reply.result?["message"]?.string?.contains("静音测试成功") == true)
    }

    func testFragmentedAndMultipleJSONLLines() throws {
        let first = Data(#"{"v":1,"id":"a","ok":true,"result":{"name":"VoiceRewrite","protocol":1}}"#.utf8)
        let second = Data(#"{"v":1,"id":"b","ok":false,"error":{"code":"unsupported_op","message":"unsupported operation"}}"#.utf8)
        var framer = JSONLFramer()
        XCTAssertTrue(try framer.append(first.prefix(9)).isEmpty)
        XCTAssertTrue(framer.hasPartialFrame)
        var payload = Data(first.dropFirst(9))
        payload.append(contentsOf: [13, 10])
        payload.append(second)
        payload.append(10)
        let lines = try framer.append(payload)
        XCTAssertEqual(lines.count, 2)
        XCTAssertFalse(framer.hasPartialFrame)
        let success = try JSONDecoder().decode(BridgeReply.self, from: lines[0])
        try success.validate()
        XCTAssertEqual(success.id, "a")
        XCTAssertEqual(success.result?["protocol"]?.int, 1)
        let failure = try JSONDecoder().decode(BridgeReply.self, from: lines[1])
        try failure.validate()
        XCTAssertEqual(failure.error?.code, "unsupported_op")
        XCTAssertEqual(failure.error?.userText, "unsupported operation")
    }

    func testBridgeErrorPrefersCoreRecoveryText() {
        let recovery = BridgeErrorMessage(
            code: "asr_failed",
            message: "API 验证失败。检查服务地址、模型 ID 和 API Key。")
        XCTAssertTrue(recovery.userText.contains("API 验证失败"))
        XCTAssertEqual(BridgeErrorMessage(code: "asr_failed", message: "   ").userText, "asr_failed")
    }

    func testFrameLimitAndMalformedResponse() throws {
        var framer = JSONLFramer()
        XCTAssertThrowsError(try framer.append(Data(repeating: 0x61, count: JSONLFramer.maximumFrameBytes + 2))) {
            XCTAssertEqual($0 as? BridgeFailure, .frameTooLarge)
        }
        XCTAssertThrowsError(try JSONDecoder().decode(BridgeReply.self, from: Data("{invalid}".utf8)))
        let contradictory = try JSONDecoder().decode(BridgeReply.self,
            from: Data(#"{"v":1,"id":"c","ok":true,"error":{"code":"x","message":"x"}}"#.utf8))
        XCTAssertThrowsError(try contradictory.validate())
    }

    func testDuplicateIDsCancelAndRetiredLateReply() throws {
        let registry = ReplyRegistry()
        let canceled = expectation(description: "cancel callback")
        try registry.register(id: "same") { result in
            if case .failure(.cancelled) = result { canceled.fulfill() }
        }
        XCTAssertThrowsError(try registry.register(id: "same") { _ in }) {
            XCTAssertEqual($0 as? BridgeFailure, .duplicateID)
        }
        XCTAssertTrue(registry.cancel(id: "same"))
        XCTAssertFalse(registry.cancel(id: "same"))
        XCTAssertThrowsError(try registry.register(id: "same") { _ in })
        let late = try JSONDecoder().decode(BridgeReply.self,
            from: Data(#"{"v":1,"id":"same","ok":true,"result":{"text":"late"}}"#.utf8))
        XCTAssertFalse(registry.resolve(late))
        XCTAssertEqual(registry.count, 0)
        wait(for: [canceled], timeout: 2)
    }

    func testResultGenerations() {
        var gate = ResultGeneration()
        let old = gate.begin()
        let newest = gate.begin()
        XCTAssertFalse(gate.accepts(old))
        XCTAssertTrue(gate.accepts(newest))
    }

    func testRealBundledGoHelloAndErrorCorrelatedByID() throws {
        let client = BridgeClient(executableURL: try bundledHelper())
        try client.start()
        defer { client.stop() }
        let hello = expectation(description: "real Go hello")
        let error = expectation(description: "real Go unsupported op")
        client.send(operation: "hello", id: "hello-test") { response in
            switch response {
            case .success(let value):
                XCTAssertEqual(value.id, "hello-test")
                XCTAssertTrue(value.ok)
                XCTAssertEqual(value.result?["name"]?.string, "VoiceRewrite")
                XCTAssertEqual(value.result?["protocol"]?.int, 1)
            case .failure(let failure): XCTFail("hello failed: \(failure)")
            }
            hello.fulfill()
        }
        client.send(operation: "not-supported", id: "invalid-operation") { response in
            switch response {
            case .success(let value):
                XCTAssertEqual(value.id, "invalid-operation")
                XCTAssertFalse(value.ok)
                XCTAssertEqual(value.error?.code, "unsupported_op")
            case .failure(let failure): XCTFail("transport failed: \(failure)")
            }
            error.fulfill()
        }
        wait(for: [hello, error], timeout: 10)
    }

    func testRealProcessTerminationAndCleanup() throws {
        let ended = expectation(description: "helper exit detected")
        let client = BridgeClient(executableURL: try bundledHelper()) { reason in
            if case .processExited = reason { ended.fulfill() }
            else { XCTFail("unexpected exit reason: \(reason)") }
        }
        try client.start()
        let pid = try XCTUnwrap(client.processID)
        XCTAssertGreaterThan(pid, 0)
        XCTAssertEqual(kill(pid, SIGKILL), 0)
        wait(for: [ended], timeout: 10)
        XCTAssertNil(client.processID)
        let rejected = expectation(description: "request after exit rejected")
        client.send(operation: "hello") { result in
            XCTAssertEqual(try? result.get(), nil)
            if case .failure(.notRunning) = result { rejected.fulfill() }
        }
        wait(for: [rejected], timeout: 3)
        client.stop()
    }

    func testCancelAndRegistryProcessExitFailure() throws {
        let registry = ReplyRegistry()
        let canceled = expectation(description: "cancelled")
        let exited = expectation(description: "exit failed outstanding")
        try registry.register(id: "one") { result in
            if case .failure(.cancelled) = result { canceled.fulfill() }
        }
        try registry.register(id: "two") { result in
            if case .failure(.processExited(9)) = result { exited.fulfill() }
        }
        XCTAssertTrue(registry.cancel(id: "one"))
        registry.failAll(.processExited(9))
        XCTAssertEqual(registry.count, 0)
        wait(for: [canceled, exited], timeout: 2)
    }

    /// This is the same keyboard state machine and presentation controller as
    /// production, with only the external mic, ASR endpoint and OS paste
    /// replaced by deterministic CI fixtures. It is NOT a real-hardware test.
    @MainActor
    func testBaselineSyntheticRightCommandDownToVisibleTextLatency() async throws {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
        let directory = root.appendingPathComponent("performance-output", isDirectory: true)
        // Temporary baseline-only runner: always exercise this isolated fixture.

        let fixture = FileManager.default.temporaryDirectory
            .appendingPathComponent("voice-rewrite-ci-asr-\(UUID().uuidString).sh")
        // Local, deterministic JSONL Helper. No remote ASR, network or
        // physical audio device; BridgeClient's real Process/Pipe code runs.
        let script = """
        #!/bin/sh
        while IFS= read -r line; do
          id="$(printf '%s' "$line" | /usr/bin/sed -n 's/.*"id":"\\([^"]*\\)".*/\\1/p')"
          [ -n "$id" ] || continue
          case "$line" in
            *'"op":"hello"'*)
              printf '{"v":1,"id":"%s","ok":true,"result":{"name":"VoiceRewrite","protocol":1}}\\n' "$id" ;;
            *'"op":"transcribe"'*)
              printf '{"v":1,"id":"%s","ok":true,"result":{"text":"CI latency test."}}\\n' "$id" ;;
            *)
              printf '{"v":1,"id":"%s","ok":false,"error":{"code":"unsupported_op","message":"test fixture"}}\\n' "$id" ;;
          esac
        done
        """
        try Data(script.utf8).write(to: fixture, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                               ofItemAtPath: fixture.path)
        defer { try? FileManager.default.removeItem(at: fixture) }

        // A real on-screen AppKit editor, not a string variable standing in
        // for UI. Only the permission-requiring cross-process paste is faked.
        let editor = NSTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        let editorWindow = NSWindow(
            contentRect: NSRect(x: 80, y: 80, width: 400, height: 100),
            styleMask: [.titled], backing: .buffered, defer: false)
        editorWindow.title = "VoiceRewrite CI target editor"
        editorWindow.contentView = editor
        editorWindow.orderFrontRegardless()
        defer { editorWindow.orderOut(nil) }
        let mic = PerformanceFixtureMicrophone()
        let writer = PerformanceFixtureWriter(editor: editor)
        let model = WorkspaceModel(microphone: mic, dictationDelivery: writer,
                                   helperExecutableURL: fixture)
        // These values don't affect the deterministic helper but must satisfy
        // the real request-configuration validation path.
        model.settings.asrBaseURL = "http://127.0.0.1:49251/v1"
        model.settings.asrModel = "CI-ASR"
        model.setKeyboardHoldEnabled(true)
        model.connect()
        defer { model.shutdown() }

        func waitUntil(_ description: String, timeout: Double = 5,
                       _ condition: () -> Bool) async throws {
            let expires = ProcessInfo.processInfo.systemUptime + timeout
            while !condition() {
                if ProcessInfo.processInfo.systemUptime >= expires {
                    XCTFail("CI journey stuck waiting for \(description)")
                    throw PerformanceFixtureFailure.timeout
                }
                try await Task.sleep(nanoseconds: 10_000_000)
            }
        }

        try await waitUntil("Helper hello", timeout: 10) {
            if case .connected = model.connection { return true }
            return false
        }
        // Use the same production hold state + 250ms interval as PR #119.
        // Baseline driver has no injectable event tap; Quartz scheduling itself
        // is intentionally excluded from both callback-to-indicator comparisons.
        var modifierState = MacModifierHoldState()
        let loopCount = 12
        var activationTimes: [Double] = []
        var indicatorTimes: [Double] = []
        var microphoneTimes: [Double] = []
        var insertionTimes: [Double] = []
        let text = "CI latency test."
        for cycle in 0..<loopCount {
            let onset = ProcessInfo.processInfo.systemUptime
            // Clock starts BEFORE MacModifierHoldDriver schedules the hold
            // activation timer. The scheduler/threshold are genuinely exercised.
            let token = try XCTUnwrap(modifierState.press(.rightCommand, unmodified: true))
            try await Task.sleep(nanoseconds: 250_000_000)
            XCTAssertTrue(modifierState.activate(.rightCommand, token: token))
            model.keyboardPressed(.dictation, targetPID: 4242)
            activationTimes.append((ProcessInfo.processInfo.systemUptime - onset) * 1000)
            try await waitUntil("visible recording indicator #\(cycle)") {
                NSApp.windows.contains { $0 is NSPanel && $0.isVisible && $0.level == .statusBar }
            }
            let panel = NSApp.windows.first {
                $0 is NSPanel && $0.isVisible && $0.level == .statusBar
            }
            guard let panel else {
                XCTFail("No visible user recording indicator on key-down")
                throw PerformanceFixtureFailure.timeout
            }
            panel.contentView?.layoutSubtreeIfNeeded()
            panel.contentView?.displayIfNeeded()
            let indicator = ProcessInfo.processInfo.systemUptime
            indicatorTimes.append((indicator - onset) * 1000)

            try await waitUntil("mic ready #\(cycle)") { model.isRecording }
            let started = ProcessInfo.processInfo.systemUptime
            microphoneTimes.append((started - onset) * 1000)
            XCTAssertTrue(mic.recording)
            XCTAssertFalse(model.isStarting)

            let previous = editor.string.count
            let release = ProcessInfo.processInfo.systemUptime
            XCTAssertEqual(modifierState.release(.rightCommand), .rightCommand)
            model.keyboardReleased(.dictation)
            try await waitUntil("inserted text #\(cycle)") {
                editor.string.count == previous + text.count && !model.isProcessing
            }
            editor.layoutSubtreeIfNeeded()
            editor.displayIfNeeded()
            insertionTimes.append(
                (ProcessInfo.processInfo.systemUptime - release) * 1000)
            XCTAssertTrue(editorWindow.isVisible)
            XCTAssertEqual(editor.string, String(repeating: text, count: cycle + 1))
            XCTAssertFalse(mic.recording)
        }

        func stats(_ samples: [Double]) -> [String: Double] {
            let sorted = samples.sorted()
            func percentile(_ p: Double) -> Double {
                sorted[min(sorted.count - 1, max(0, Int(ceil(Double(sorted.count) * p)) - 1))]
            }
            return [
                "p50_ms": percentile(0.50),
                "p95_ms": percentile(0.95),
                "max_ms": sorted.last ?? 0,
                "mean_ms": samples.reduce(0, +) / Double(samples.count),
                "samples": Double(samples.count),
            ]
        }
        #if arch(arm64)
        let architecture = "arm64"
        #elseif arch(x86_64)
        let architecture = "x64"
        #else
        let architecture = "unknown"
        #endif
        let report: [String: Any] = [
            "schema_version": 2,
            "test": "baseline-synthetic-right-command-down-to-visible-text",
            "environment": "macOS CI; production hold state + 250ms sleep; fake microphone, local JSONL ASR, in-process NSTextView",
            "coverage": [
                "real_MacModifierHoldState_plus_synthetic_timer",
                "real_WorkspaceModel_keyboard_state_machine",
                "real_AppKit_indicator_panel",
                "real_Background_BridgeClient_IPC",
                "real_WorkspaceModel_result_dispatch",
                "synthetic_audio_and_target_editor",
            ],
            "excludes": [
                "physical_keyboard_HID_scan_and_OS_event_delivery",
                "hardware_microphone_start", "cloud_ASR",
                "system_paste_and_cross_application_AX",
            ],
            "architecture": architecture,
            "successful_cycles": loopCount,
            "timeouts": 0,
            "synthetic_down_to_hold_activation": stats(activationTimes),
            "synthetic_down_to_indicator_render": stats(indicatorTimes),
            "synthetic_down_to_recording_ready": stats(microphoneTimes),
            "release_to_text_visible": stats(insertionTimes),
        ]
        let folder = directory.appendingPathComponent("macos-\(architecture)", isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let json = try JSONSerialization.data(withJSONObject: report,
                                               options: [.prettyPrinted, .sortedKeys])
        try json.write(to: folder.appendingPathComponent("journey.json"), options: .atomic)
        print("VOICE_REWRITE_BASELINE_LATENCY " + String(decoding: json, as: UTF8.self))
        // Hard limits only catch pathological regressions in cloud runners.
        // Compare P50/P95 as a trend, never claim these are physical-device SLOs.
        XCTAssertGreaterThanOrEqual(stats(activationTimes)["p50_ms"]!, SideHold.threshold * 1000)
        XCTAssertLessThan(stats(activationTimes)["p95_ms"]!, 1200)
        XCTAssertLessThan(stats(indicatorTimes)["p95_ms"]!, 1500)
        XCTAssertLessThan(stats(microphoneTimes)["p95_ms"]!, 2500)
        XCTAssertLessThan(stats(insertionTimes)["p95_ms"]!, 5000)
    }


}


@MainActor
private final class PerformanceFixtureMicrophone: VoiceRecordingDevice {
    var permissionStatus: MicrophonePermissionStatus { .authorized }
    var rmsLevel: Float { 0.1 }
    private(set) var recording = false

    func start(deviceUID: String, maximumSeconds: Int) async throws {
        try await Task.sleep(nanoseconds: 40_000_000)
        recording = true
    }

    func stop() async throws -> Data {
        guard recording else { throw MicrophoneError.notRecording }
        recording = false
        try await Task.sleep(nanoseconds: 20_000_000)
        return Data(repeating: 0, count: 32_000)
    }

    func cancel() { recording = false }
}

@MainActor
private final class PerformanceFixtureWriter: DictationDelivering {
    let editor: NSTextView

    init(editor: NSTextView) { self.editor = editor }

    func capture(processID: Int32) throws -> MacDictationTarget {
        MacDictationTarget.performanceFixture(processID: processID)
    }

    func deliver(_ text: String, to target: MacDictationTarget,
                 stillValid: () -> Bool) async throws -> DeliveryState {
        guard stillValid() else { throw InteractionFailure.cancelled }
        editor.string += text
        editor.needsDisplay = true
        return .confirmed
    }
}

private enum PerformanceFixtureFailure: Error { case timeout }
