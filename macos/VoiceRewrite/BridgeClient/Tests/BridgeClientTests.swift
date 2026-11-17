import Foundation
import XCTest
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
