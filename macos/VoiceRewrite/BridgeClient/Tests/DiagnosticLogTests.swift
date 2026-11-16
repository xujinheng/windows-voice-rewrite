import XCTest
@testable import VoiceRewrite

final class DiagnosticLogTests: XCTestCase {
    func testRotationKeepsThreeBackups() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoiceRewriteLog-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let path = directory.appendingPathComponent("VoiceRewrite.log").path
        func write(_ name: String, _ text: String) throws {
            try Data(text.utf8).write(to: URL(fileURLWithPath: name))
        }
        func read(_ name: String) throws -> String {
            try String(contentsOf: URL(fileURLWithPath: name), encoding: .utf8)
        }
        try write(path, "aaaaa")
        try DiagnosticLog.rotate(path, maxBytes: 5, backups: 3)
        XCTAssertEqual(try read(path + ".1"), "aaaaa")
        try write(path, "bbbbb")
        try DiagnosticLog.rotate(path, maxBytes: 5, backups: 3)
        XCTAssertEqual(try read(path + ".1"), "bbbbb")
        XCTAssertEqual(try read(path + ".2"), "aaaaa")
        try write(path, "ccccc")
        try DiagnosticLog.rotate(path, maxBytes: 5, backups: 3)
        try write(path, "ddddd")
        try DiagnosticLog.rotate(path, maxBytes: 5, backups: 3)
        XCTAssertEqual(try read(path + ".1"), "ddddd")
        XCTAssertEqual(try read(path + ".2"), "ccccc")
        XCTAssertEqual(try read(path + ".3"), "bbbbb")
        XCTAssertFalse(FileManager.default.fileExists(atPath: path + ".4"))
    }

    func testSmallFileStaysInPlace() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoiceRewriteLog-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let path = directory.appendingPathComponent("VoiceRewrite.log").path
        try Data("kept".utf8).write(to: URL(fileURLWithPath: path))
        try DiagnosticLog.rotate(path, maxBytes: 1000, backups: 3)
        XCTAssertFalse(FileManager.default.fileExists(atPath: path + ".1"))
        XCTAssertEqual(try String(contentsOf: URL(fileURLWithPath: path), encoding: .utf8), "kept")
    }
}
