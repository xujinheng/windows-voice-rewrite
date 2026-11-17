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

    func testLatestErrorSkipsNormalLinesAndBoundsDisplay() {
        let log = """
        2026-11-17T01:00:00Z 应用已启动
        2026-11-17T01:01:00Z stage=dictation_delivery status=failed code=focus_lost
        2026-11-17T01:02:00Z 设置已保存
        """
        XCTAssertEqual(DiagnosticLog.latestError(in: log),
                       "2026-11-17T01:01:00Z stage=dictation_delivery status=failed code=focus_lost")
        XCTAssertNil(DiagnosticLog.latestError(in: "应用已启动\n设置已保存"))
        XCTAssertEqual(DiagnosticLog.latestError(in: String(repeating: "错误", count: 300))?.count, 400)
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
    func testRecentErrorSurvivesRotationAndPrefersActiveLog() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoiceRewriteRotatedError-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let active = directory.appendingPathComponent("VoiceRewrite.log")
        try Data("应用已启动\n".utf8).write(to: active)
        try Data("2026-11-17 status=failed code=permission_denied\n".utf8)
            .write(to: URL(fileURLWithPath: active.path + ".1"))
        XCTAssertEqual(DiagnosticLog.latestError(at: active, backupCount: 3),
                       "2026-11-17 status=failed code=permission_denied")
        try Data("2026-11-17 status=error code=asr_failed\n".utf8).write(to: active)
        XCTAssertEqual(DiagnosticLog.latestError(at: active, backupCount: 3),
                       "2026-11-17 status=error code=asr_failed")
    }

}
