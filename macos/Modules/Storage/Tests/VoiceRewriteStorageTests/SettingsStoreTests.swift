import Foundation
import XCTest
@testable import VoiceRewriteStorage

final class SettingsStoreTests: XCTestCase {
    private func temporaryDirectory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("VoiceRewriteStorageTests-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url.appendingPathComponent("VoiceRewrite")
    }

    func testDefaultsAndRoundTrip() throws {
        let dir = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: dir.deletingLastPathComponent()) }
        let store = SettingsStore(directory: dir)
        XCTAssertEqual(try store.load(), .defaults)
        XCTAssertEqual(MacSettings.defaults.maxRecordSeconds, 60)
        var settings = MacSettings.defaults
        settings.textModel = "text-model"
        settings.maxRecordSeconds = 90
        settings.microphoneUID = "recording-device-uid"
        try store.save(settings)
        XCTAssertEqual(try store.load(), settings)
        let file = dir.appendingPathComponent("settings.json")
        let data = try Data(contentsOf: file)
        var json = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(Set(json.keys), [
            "asr_base_url", "llm_base_url", "asr_model", "text_model", "max_record_seconds",
            "dictation_shortcut", "rewrite_shortcut", "text_edit_shortcut", "microphone_uid",
        ])
        XCTAssertEqual((try FileManager.default.attributesOfItem(atPath: dir.path)[.posixPermissions] as? NSNumber)?.intValue, 0o700)
        XCTAssertEqual((try FileManager.default.attributesOfItem(atPath: file.path)[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: dir.path), ["settings.json"])
        json.removeValue(forKey: "dictation_shortcut")
        json.removeValue(forKey: "rewrite_shortcut")
        json.removeValue(forKey: "text_edit_shortcut")
        try JSONSerialization.data(withJSONObject: json).write(to: file)
        let migrated = try store.load()
        XCTAssertEqual(migrated.dictationShortcut, "Ctrl+Alt+R")
        XCTAssertEqual(migrated.rewriteShortcut, "Ctrl+Alt+E")
        XCTAssertEqual(migrated.textEditShortcut, "Ctrl+Alt+T")
        XCTAssertEqual(migrated.textModel, "text-model")
        XCTAssertEqual(migrated.microphoneUID, "recording-device-uid")
    }

    func testInvalidJSONAndUnknownSecretFieldsAreRejected() throws {
        let dir = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: dir.deletingLastPathComponent()) }
        let store = SettingsStore(directory: dir)
        try store.save(.defaults)
        let file = dir.appendingPathComponent("settings.json")
        try Data("{bad json}".utf8).write(to: file)
        XCTAssertThrowsError(try store.load()) { XCTAssertEqual($0 as? SettingsError, .invalidJSON) }
        let valid = try JSONEncoder().encode(MacSettings.defaults)
        var obj = try XCTUnwrap(JSONSerialization.jsonObject(with: valid) as? [String: Any])
        obj["asr_api_key"] = "fixture-not-a-credential"
        try JSONSerialization.data(withJSONObject: obj).write(to: file)
        XCTAssertThrowsError(try store.load()) { XCTAssertEqual($0 as? SettingsError, .invalidJSON) }
        obj.removeValue(forKey: "asr_api_key")
        obj.removeValue(forKey: "llm_base_url")
        try JSONSerialization.data(withJSONObject: obj).write(to: file)
        XCTAssertThrowsError(try store.load()) { XCTAssertEqual($0 as? SettingsError, .invalidJSON) }
    }

    func testWriteFailureKeepsOldValidFileAndRemovesTemp() throws {
        enum SimulatedFailure: Error { case diskFull }
        let dir = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: dir.deletingLastPathComponent()) }
        let store = SettingsStore(directory: dir)
        try store.save(.defaults)
        let original = try Data(contentsOf: dir.appendingPathComponent("settings.json"))
        let failing = SettingsStore(directory: dir, beforeReplace: { throw SimulatedFailure.diskFull })
        var edited = MacSettings.defaults
        edited.asrModel = "changed"
        XCTAssertThrowsError(try failing.save(edited))
        XCTAssertEqual(try Data(contentsOf: dir.appendingPathComponent("settings.json")), original)
        XCTAssertEqual(try store.load(), .defaults)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: dir.path), ["settings.json"])
    }

    func testFilesystemFailureAndSymlinkDefense() throws {
        let dir = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: dir.deletingLastPathComponent()) }
        try FileManager.default.createDirectory(at: dir.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data("not a directory".utf8).write(to: dir)
        XCTAssertThrowsError(try SettingsStore(directory: dir).save(.defaults))
        try FileManager.default.removeItem(at: dir)
        let store = SettingsStore(directory: dir)
        try store.save(.defaults)
        let file = dir.appendingPathComponent("settings.json")
        try FileManager.default.removeItem(at: file)
        let other = dir.deletingLastPathComponent().appendingPathComponent("other.json")
        try Data("outside".utf8).write(to: other)
        try FileManager.default.createSymbolicLink(at: file, withDestinationURL: other)
        XCTAssertThrowsError(try store.load())
        XCTAssertThrowsError(try store.save(.defaults))
        XCTAssertEqual(try String(contentsOf: other, encoding: .utf8), "outside")
    }

    func testCredentialBearingURLCannotBeSaved() throws {
        let dir = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: dir.deletingLastPathComponent()) }
        var settings = MacSettings.defaults
        settings.asrBaseURL = "https://user:password@example.test/v1"
        XCTAssertThrowsError(try SettingsStore(directory: dir).save(settings))
        XCTAssertFalse(FileManager.default.fileExists(atPath: dir.appendingPathComponent("settings.json").path))
    }

    func testCoreRecordingLimitAndCanonicalURL() throws {
        let dir = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: dir.deletingLastPathComponent()) }
        let store = SettingsStore(directory: dir)
        var settings = MacSettings.defaults
        settings.maxRecordSeconds = 180
        settings.asrBaseURL = "  HTTP://127.0.0.1:8080/v1  "
        settings.asrModel = "   "
        try store.save(settings)
        let loaded = try store.load()
        XCTAssertEqual(loaded.maxRecordSeconds, 180)
        XCTAssertEqual(loaded.asrBaseURL, "HTTP://127.0.0.1:8080/v1")
        XCTAssertEqual(loaded.asrModel, "whisper-1")

        let file = dir.appendingPathComponent("settings.json")
        var object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: file)) as? [String: Any])
        object["max_record_seconds"] = 181
        try JSONSerialization.data(withJSONObject: object).write(to: file)
        XCTAssertEqual(try store.load().maxRecordSeconds, 60)

        settings = MacSettings.defaults
        settings.llmBaseURL = "https://example.invalid/v1?key=leak"
        XCTAssertThrowsError(try store.save(settings))
        settings.llmBaseURL = "http://example.test/v1#fragment"
        XCTAssertThrowsError(try store.save(settings))
        settings.llmBaseURL = "http:example.com"
        XCTAssertThrowsError(try store.save(settings))
        XCTAssertEqual(ContractLimits.credentialTooLong, "输入内容过长")
        XCTAssertFalse(MacSettings.credentialTooLong(String(repeating: "k", count: 4000)))
        XCTAssertTrue(MacSettings.credentialTooLong(String(repeating: "k", count: 4001)))
        XCTAssertFalse(MacSettings.credentialTooLong(String(repeating: " ", count: 4000)))
        XCTAssertTrue(MacSettings.credentialTooLong(String(repeating: " ", count: 4001)))
        XCTAssertFalse(MacSettings.replacesStoredCredential(""))
        XCTAssertTrue(MacSettings.replacesStoredCredential(" "))
        XCTAssertEqual(helperConnectedLabel(), "VoiceRewrite / JSONL v1")
        XCTAssertTrue(helperHelloAccepted(ok: true, name: ContractLimits.helperName, version: ContractLimits.protocolVersion))
        XCTAssertFalse(helperHelloAccepted(ok: true, name: "other", version: ContractLimits.protocolVersion))
        XCTAssertFalse(helperHelloAccepted(ok: false, name: ContractLimits.helperName, version: ContractLimits.protocolVersion))
        XCTAssertFalse(helperHelloAccepted(ok: true, name: ContractLimits.helperName, version: ContractLimits.protocolVersion + 1))
        XCTAssertTrue(MacSettings.acceptsBaseURL("http://example.test/v1?"))
        XCTAssertFalse(MacSettings.acceptsBaseURL("file:///tmp/x"))
    }
}
