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
        try store.save(settings)
        XCTAssertEqual(try store.load(), settings)
        let file = dir.appendingPathComponent("settings.json")
        let data = try Data(contentsOf: file)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(Set(json.keys), ["asr_base_url", "llm_base_url", "asr_model", "text_model", "max_record_seconds"])
        XCTAssertEqual((try FileManager.default.attributesOfItem(atPath: dir.path)[.posixPermissions] as? NSNumber)?.intValue, 0o700)
        XCTAssertEqual((try FileManager.default.attributesOfItem(atPath: file.path)[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: dir.path), ["settings.json"])
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
}
