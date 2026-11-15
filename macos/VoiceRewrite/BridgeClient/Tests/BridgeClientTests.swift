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
}
