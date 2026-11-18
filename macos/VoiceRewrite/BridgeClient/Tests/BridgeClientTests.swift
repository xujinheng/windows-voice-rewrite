import Foundation
import XCTest
import Darwin
import AppKit
import Microphone
import VoiceRewriteInteraction
@testable import VoiceRewrite

final class BridgeClientTests: XCTestCase {
    func testVoiceFlowV1EffectDecodingRejectsUnknownOrForeignNativeCommands() throws {
        let request = try BridgeRequest(id: "task-event", operation: "voice_event", fields: [
            "event": .object([
                "v": .number(1), "type": .string("record_arm"),
                "session_id": .string("session-1"), "generation": .number(3),
                "command_id": .string("arm-3"), "mode": .string("dictation"),
                "can_insert": .boolean(true),
            ]),
        ]).line()
        let payload = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(request.dropLast()))
                                    as? [String: Any])
        XCTAssertEqual(payload["op"] as? String, "voice_event")
        XCTAssertEqual((payload["event"] as? [String: Any])?["session_id"] as? String, "session-1")

        func decoded(_ actionType: String, effectSession: String = "session-1",
                     effectGeneration: Int = 3) throws -> VoiceFlowTransitionV1 {
            let sample = """
            {"v":1,"id":"task-event","ok":true,"result":{
                "accepted":true,"duplicate":false,"session_id":"session-1",
                "generation":3,"phase":"starting","effects":[{
                    "type":"\(actionType)","session_id":"\(effectSession)",
                    "generation":\(effectGeneration),"command_id":"session-1/3/1",
                    "request_id":"session-1/3/1"}]}}
            """
            let reply = try JSONDecoder().decode(BridgeReply.self, from: Data(sample.utf8))
            return try VoiceFlowTransitionV1.decode(reply)
        }

        let valid = try decoded("start_native_capture")
        XCTAssertEqual(valid.effects.count, 1)
        XCTAssertEqual(valid.effects.first?.type, .startNativeCapture)
        XCTAssertEqual(valid.effects.first?.generation, 3)
        XCTAssertThrowsError(try decoded("unexpected_native_write"))
        XCTAssertThrowsError(try decoded("attempt_native_write", effectSession: "other-session"))
        XCTAssertThrowsError(try decoded("attempt_native_write", effectGeneration: 4))
    }

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

        let response = Data(#"{"v":1,"id":"voice-selected","ok":true,"result":{"instruction":"请翻译成英文","intent":"translate","text":"Original","route":"replace"}}"#.utf8)
        let reply = try JSONDecoder().decode(BridgeReply.self, from: response)
        try reply.validate()
        XCTAssertEqual(reply.result?["instruction"]?.string, "请翻译成英文")
        XCTAssertEqual(reply.result?["route"]?.string, "replace")
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

    /// The real bundled Go bridge must deny all callbacks after a native
    /// cancellation, including late write receipts and stale timeouts. This
    /// complements ResultGeneration's immediate native OS safety fence.
    func testBundledEpochCancelRejectsLateWriteAndAllowsNextTask() throws {
        let client = BridgeClient(executableURL: try bundledHelper())
        try client.start()
        defer { client.stop() }

        func event(_ type: String, task: String, generation: Int, stage: String? = nil) throws -> BridgeReply {
            let finished = expectation(description: "epoch \(type) \(task)")
            var reply: BridgeReply?
            var error: BridgeFailure?
            var fields: [String: JSONValue] = [
                "epoch_event": .string(type),
                "session_id": .string(task),
                "generation": .number(Double(generation)),
            ]
            if let stage { fields["stage"] = .string(stage) }
            client.send(operation: "voice_epoch", fields: fields) { result in
                switch result {
                case .success(let value): reply = value
                case .failure(let failure): error = failure
                }
                finished.fulfill()
            }
            wait(for: [finished], timeout: 4)
            XCTAssertNil(error)
            return try XCTUnwrap(reply)
        }

        let first = try event("begin", task: "mac-native-old", generation: 1)
        XCTAssertTrue(first.result?["allowed"]?.boolValue == true)
        let stage = try event("stage", task: "mac-native-old", generation: 1, stage: "native_write")
        XCTAssertTrue(stage.result?["allowed"]?.boolValue == true)
        XCTAssertEqual(stage.result?["budget_ms"]?.int, 20_000)
        let canceled = try event("cancel", task: "mac-native-old", generation: 1)
        XCTAssertTrue(canceled.result?["allowed"]?.boolValue == true)
        if case .array(let effects)? = canceled.result?["effects"] {
            XCTAssertFalse(effects.isEmpty)
        } else {
            XCTFail("missing Go cancel effects")
        }
        XCTAssertFalse(try event("authorize", task: "mac-native-old", generation: 1, stage: "native_write")
            .result?["allowed"]?.boolValue == true)
        XCTAssertFalse(try event("receipt", task: "mac-native-old", generation: 1, stage: "native_write")
            .result?["allowed"]?.boolValue == true)
        XCTAssertFalse(try event("timeout", task: "mac-native-old", generation: 1, stage: "native_write")
            .result?["allowed"]?.boolValue == true)
        XCTAssertTrue(try event("begin", task: "mac-native-new", generation: 2)
            .result?["allowed"]?.boolValue == true)
        XCTAssertTrue(try event("authorize", task: "mac-native-new", generation: 2)
            .result?["allowed"]?.boolValue == true)
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

    @MainActor
    func testMeetingSnapshotTimeoutCannotRemainPendingForever() {
        XCTAssertFalse(WorkspaceModel.meetingSnapshotExpired(startedAt: 0, now: 100))
        XCTAssertFalse(WorkspaceModel.meetingSnapshotExpired(startedAt: 100, now: 111.99))
        XCTAssertTrue(WorkspaceModel.meetingSnapshotExpired(startedAt: 100, now: 112))
        XCTAssertTrue(WorkspaceModel.meetingSnapshotExpired(startedAt: 100, now: 130))
    }

    @MainActor
    func testAudioDeadlineAlwaysFinishesAndScalesWithDuration() {
        XCTAssertEqual(WorkspaceModel.asrDeadline(for: Data(repeating: 0, count: 32000)), 80)
        XCTAssertEqual(WorkspaceModel.asrDeadline(for: Data(repeating: 0, count: 960000)), 120)
        XCTAssertEqual(WorkspaceModel.asrDeadline(for: Data(repeating: 0, count: 32000 * 180)), 235)
    }

    func testQueuedPCMFramesAndLocalEncodingFailureDoNotKillBridge() throws {
        let client = BridgeClient(executableURL: try bundledHelper())
        try client.start()
        defer { client.stop() }

        let small = expectation(description: "PCM frame serialized in background")
        client.sendPCM(operation: "hello", pcm: Data([0, 1, 2, 3]), id: "pcm-small") { result in
            guard case .success(let reply) = result else {
                XCTFail("small PCM frame was not delivered"); small.fulfill(); return
            }
            XCTAssertEqual(reply.id, "pcm-small")
            XCTAssertTrue(reply.ok)
            small.fulfill()
        }

        // A Base64 value larger than the JSONL limit must fail that request,
        // not terminate the entire Helper session or strand its callback.
        let tooLarge = expectation(description: "oversized PCM fails locally")
        let audio = Data(repeating: 0, count: JSONLFramer.maximumFrameBytes * 3 / 4 + 128)
        client.sendPCM(operation: "transcribe", pcm: audio, id: "pcm-too-large") { result in
            if case .failure(.frameTooLarge) = result { /* expected */ }
            else { XCTFail("expected frameTooLarge, got \(result)") }
            tooLarge.fulfill()
        }

        let after = expectation(description: "bridge still usable")
        client.send(operation: "hello", id: "after-large") { result in
            XCTAssertEqual(try? result.get().result?["protocol"]?.int, 1)
            after.fulfill()
        }
        wait(for: [small, tooLarge, after], timeout: 20)
    }

    func testCancelDuringPCMEncodingRetiresReplyAndAllowsLaterRequests() throws {
        let client = BridgeClient(executableURL: try bundledHelper())
        try client.start()
        defer { client.stop() }
        let cancelled = expectation(description: "queued large audio cancelled")
        let audio = Data(repeating: 0, count: 2 << 20)
        let id = client.sendPCM(operation: "transcribe", pcm: audio, id: "cancel-during-encoding") { result in
            if case .failure(.cancelled) = result { /* expected */ }
            else { XCTFail("request was not retired: \(result)") }
            cancelled.fulfill()
        }
        XCTAssertTrue(client.cancel(id: id))

        let healthy = expectation(description: "cancel did not jam serial writer")
        client.send(operation: "hello", id: "after-cancel") { result in
            XCTAssertTrue((try? result.get().ok) == true)
            healthy.fulfill()
        }
        wait(for: [cancelled, healthy], timeout: 15)
    }

    func testRegistryLocalWriteErrorDoesNotDiscardOtherReplies() throws {
        let registry = ReplyRegistry()
        let failed = expectation(description: "only failed ID retired")
        let survived = expectation(description: "other ID resolved")
        try registry.register(id: "bad") { result in
            if case .failure(.invalidRequest) = result { /* expected */ }
            else { XCTFail("unexpected local failure") }
            failed.fulfill()
        }
        try registry.register(id: "good") { result in
            XCTAssertTrue((try? result.get().ok) == true)
            survived.fulfill()
        }
        XCTAssertTrue(registry.contains(id: "bad"))
        registry.fail(id: "bad", reason: .invalidRequest)
        XCTAssertFalse(registry.contains(id: "bad"))
        XCTAssertTrue(registry.contains(id: "good"))
        let reply = try JSONDecoder().decode(BridgeReply.self,
            from: Data(#"{"v":1,"id":"good","ok":true,"result":{"text":"ok"}}"#.utf8))
        XCTAssertTrue(registry.resolve(reply))
        wait(for: [failed, survived], timeout: 3)
    }

    func testHelperThatNeverReadsStdinCannotBlockLaterIPCForever() throws {
        let script = FileManager.default.temporaryDirectory
            .appendingPathComponent("voicebridge-unread-\(UUID().uuidString).sh")
        try Data("#!/bin/sh\nexec sleep 8\n".utf8).write(to: script)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        defer { try? FileManager.default.removeItem(at: script) }

        let terminated = expectation(description: "stuck Helper transport terminated")
        let client = BridgeClient(executableURL: script, writeTimeout: 0.5) { failure in
            XCTAssertEqual(failure, .writeFailed)
            terminated.fulfill()
        }
        try client.start()
        defer { client.stop() }

        let rejected = expectation(description: "large pending PCM retired")
        client.sendPCM(operation: "transcribe", pcm: Data(repeating: 0, count: 2 << 20)) { result in
            if case .failure(.writeFailed) = result { /* expected */ }
            else { XCTFail("blocked pipe did not fail: \(result)") }
            rejected.fulfill()
        }
        wait(for: [rejected, terminated], timeout: 5)
    }

    func testClosedStdoutWhileHelperStaysAliveDoesNotStrandCallbacks() throws {
        let script = FileManager.default.temporaryDirectory
            .appendingPathComponent("voicebridge-eof-\(UUID().uuidString).sh")
        try Data("#!/bin/sh\nexec 1>&-\nsleep 10\n".utf8).write(to: script)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        defer { try? FileManager.default.removeItem(at: script) }

        let ended = expectation(description: "stdout EOF retires session")
        let client = BridgeClient(executableURL: script) { failure in
            XCTAssertEqual(failure, .truncatedReply)
            ended.fulfill()
        }
        try client.start()
        defer { client.stop() }

        let retired = expectation(description: "outstanding callback retired")
        client.send(operation: "hello") { result in
            if case .failure(.truncatedReply) = result { /* expected */ }
            else { XCTFail("request was stranded after EOF: \(result)") }
            retired.fulfill()
        }
        wait(for: [retired, ended], timeout: 5)
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


    /// Dictation must begin capturing audio while a third-party Accessibility
    /// service is slow; waiting on AX before starting CoreAudio loses speech.
    @MainActor
    func testDictationMicrophoneStartsWhileAXTargetCaptureIsBlocked() async throws {
        let microphone = PerformanceFixtureMicrophone()
        let writer = PerformanceFixtureWriter(editor: NSTextView())
        writer.deferTargetCaptureForCI = true
        let model = WorkspaceModel(microphone: microphone, dictationDelivery: writer)
        model.setKeyboardHoldEnabled(true)
        defer {
            writer.resumeTargetCaptureForCI()
            model.cancelCurrent()
            model.shutdown()
        }

        model.keyboardPressed(.dictation, targetPID: 4242)
        let deadline = ProcessInfo.processInfo.systemUptime + 3
        while !writer.targetCapturePendingForCI &&
              ProcessInfo.processInfo.systemUptime < deadline {
            try await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertTrue(writer.targetCapturePendingForCI)

        while !microphone.recording && ProcessInfo.processInfo.systemUptime < deadline {
            try await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertTrue(microphone.recording,
                      "Audio should start before blocked third-party AX capture finishes")
        XCTAssertFalse(model.isRecording,
                       "UI should remain pending until the target is safely captured")
        writer.resumeTargetCaptureForCI()
        while !model.isRecording && ProcessInfo.processInfo.systemUptime < deadline {
            try await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertTrue(model.isRecording)
    }

    /// This is the same keyboard state machine and presentation controller as
    /// production, with only the external mic, ASR endpoint and OS paste
    /// replaced by deterministic CI fixtures. It is NOT a real-hardware test.
    @MainActor
    func testCISyntheticRightCommandDownToVisibleTextLatency() async throws {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
        let directory = root.appendingPathComponent("performance-output", isDirectory: true)
        guard FileManager.default.fileExists(atPath:
            directory.appendingPathComponent(".enabled").path) else {
            throw XCTSkip("Synthetic modifier-to-text latency runs inside the macOS build jobs")
        }

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
            *'"op":"voice_epoch"'*)
              # The synthetic latency fixture only emulates the control reply;
              # actual epoch policy runs in the bundled Go bridge tests below.
              printf '{"v":1,"id":"%s","ok":true,"result":{"allowed":true,"budget_ms":30000,"effects":[]}}\\n' "$id" ;;
            *'"op":"transcribe"'*)
              case "$line" in
                *'"can_insert":true'*)
                  route="insert"
                  effect='{"phase":6,"mode":"听写","recording_status":"语音识别已完成","transcription":"CI latency test.","history":[{"title":"听写","value":"CI latency test."}],"insert_text":"CI latency test."}' ;;
                *)
                  route="preview"
                  effect='{"phase":9,"mode":"听写","recording_status":"语音识别已完成","transcription":"CI latency test.","history":[{"title":"听写","value":"CI latency test."}],"card_title":"未插入","card_text":"CI latency test."}' ;;
              esac
              printf '{"v":1,"id":"%s","ok":true,"result":{"text":"CI latency test.","route":"%s","effect":%s}}\\n' "$id" "$route" "$effect" ;;
            *'"op":"task_receipt"'*)
              printf '{"v":1,"id":"%s","ok":true,"result":{"effect":{"phase":7,"mode":"听写","interaction_status":"已发送，待用户确认输入框"}}}\\n' "$id" ;;
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
        // Drive the real macOS modifier handler through its 200ms hold timer,
        // not WorkspaceModel.keyboardPressed() after that timer has elapsed.
        // CGEvent dispatch is synthetic; live hardware must be tested separately.
        var activationCount = 0
        var releaseCount = 0
        let driver = MacModifierHoldDriver(
            onHold: { key, _, pressedAt in
                XCTAssertEqual(key, .rightCommand)
                activationCount += 1
                model.keyboardPressed(.dictation, targetPID: 4242, pressedAt: pressedAt)
            },
            onRelease: { key in
                XCTAssertEqual(key, .rightCommand)
                releaseCount += 1
                model.keyboardReleased(.dictation)
            },
            onCancel: { XCTFail("Modifier hold unexpectedly cancelled") })
        driver.setBindings(dictation: .rightCommand, rewrite: nil)
        defer { driver.shutdown() }

        let loopCount = 12
        var activationTimes: [Double] = []
        var indicatorTimes: [Double] = []
        var microphoneTimes: [Double] = []
        var insertionTimes: [Double] = []
        let text = "CI latency test."
        for cycle in 0..<loopCount {
            let previousPendingCount = model.pendingRecordingIndicatorForCI().count
            let onset = ProcessInfo.processInfo.systemUptime
            // Clock starts BEFORE MacModifierHoldDriver schedules the hold
            // activation timer. The scheduler/threshold are genuinely exercised.
            driver.simulateModifierTransitionForCI(key: .rightCommand, pressed: true)
            try await waitUntil("200ms right Command activation #\(cycle)") {
                activationCount == cycle + 1
            }
            activationTimes.append((ProcessInfo.processInfo.systemUptime - onset) * 1000)
            let presentation = model.pendingRecordingIndicatorForCI()
            // A previously visible status-bar panel is not evidence of a
            // pending recording presentation for this key press.
            guard presentation.count == previousPendingCount + 1,
                  let panel = presentation.panel, panel.isVisible,
                  panel.level == .statusBar else {
                XCTFail("No new pending recording indicator for cycle \(cycle)")
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
            driver.simulateModifierTransitionForCI(key: .rightCommand, pressed: false)
            try await waitUntil("right Command released #\(cycle)") {
                releaseCount == cycle + 1
            }
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
            "test": "synthetic-right-command-down-to-visible-text",
            "environment": "macOS CI; synthetic CGEvent through production modifier handler and 200ms keyboard timer; fake microphone, local JSONL ASR, in-process NSTextView",
            "coverage": [
                "real_MacModifierHoldDriver_flagsChanged_state_machine_and_timer",
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
            "keyboard_hold_threshold_ms": Int(MacModifierHoldTiming.activationThreshold * 1000),
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
        print("VOICE_REWRITE_USER_LATENCY " + String(decoding: json, as: UTF8.self))
        // Hard limits only catch pathological regressions in cloud runners.
        // Compare P50/P95 as a trend, never claim these are physical-device SLOs.
        XCTAssertGreaterThanOrEqual(stats(activationTimes)["p50_ms"]!, MacModifierHoldTiming.activationThreshold * 1000)
        XCTAssertLessThan(stats(activationTimes)["p95_ms"]!, 1200)
        XCTAssertLessThan(stats(indicatorTimes)["p95_ms"]!, 1500)
        XCTAssertLessThan(stats(microphoneTimes)["p95_ms"]!, 2500)
        XCTAssertLessThan(stats(insertionTimes)["p95_ms"]!, 5000)
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
    var deferTargetCaptureForCI = false
    private(set) var targetCapturePendingForCI = false
    private var deferredCapture: CheckedContinuation<MacDictationTarget, Never>?

    init(editor: NSTextView) { self.editor = editor }

    func captureAsync(processID: Int32) async throws -> MacDictationTarget {
        guard deferTargetCaptureForCI else {
            return MacDictationTarget.performanceFixture(processID: processID)
        }
        return await withCheckedContinuation { continuation in
            targetCapturePendingForCI = true
            deferredCapture = continuation
        }
    }

    func resumeTargetCaptureForCI() {
        deferredCapture?.resume(returning: .performanceFixture(processID: 4242))
        deferredCapture = nil
        targetCapturePendingForCI = false
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
