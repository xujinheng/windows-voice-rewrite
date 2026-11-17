import AppKit
import Foundation
import VoiceRewriteStorage

/// Bounded diagnostic log. Size, backup count, and rotation steps come from the shared log policy.
enum DiagnosticLog {
    static let maxBytes = Int64(ContractLimits.logMaxBytes)
    static let backups = ContractLimits.logBackups

    static var fileURL: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("VoiceRewrite", isDirectory: true)
            .appendingPathComponent("VoiceRewrite.log")
    }

    static func prepare() {
        let url = fileURL
        try? FileManager.default.createDirectory(
            at: url.deletingLastPathComponent(),
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        try? rotate(url.path, maxBytes: maxBytes, backups: backups)
    }

    static func note(_ message: String) {
        let line = message.replacingOccurrences(of: "\n", with: " ").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !line.isEmpty else { return }
        prepare()
        let url = fileURL
        let stamp = ISO8601DateFormatter().string(from: Date())
        let text = "\(stamp) \(line)\n"
        if let handle = try? FileHandle(forWritingTo: url) {
            defer { try? handle.close() }
            _ = try? handle.seekToEnd()
            try? handle.write(contentsOf: Data(text.utf8))
        } else {
            try? Data(text.utf8).write(to: url, options: [.atomic])
        }
    }

    static func latestError() -> String? {
        guard let contents = try? String(contentsOf: fileURL, encoding: .utf8) else { return nil }
        return latestError(in: contents)
    }

    static func latestError(in log: String) -> String? {
        for line in log.split(whereSeparator: \.isNewline).reversed() {
            let value = line.trimmingCharacters(in: .whitespacesAndNewlines)
            let lower = value.lowercased()
            if lower.contains("status=failed") || lower.contains("status=error") ||
                lower.contains("failed") || lower.contains("error") ||
                value.contains("失败") || value.contains("错误") ||
                value.contains("无法") || value.contains("拒绝") {
                return String(value.prefix(400))
            }
        }
        return nil
    }

    static func reveal() {
        prepare()
        let url = fileURL
        if !FileManager.default.fileExists(atPath: url.path) {
            try? Data().write(to: url)
        }
        NSWorkspace.shared.activateFileViewerSelecting([url])
    }

    // Source: voice-rewrite/internal/core/logfile/rotate.go
    static func rotate(_ path: String, maxBytes: Int64, backups: Int) throws {
        let manager = FileManager.default
        if backups < 1 || maxBytes < 1 {
            throw NSError(domain: "VoiceRewriteLog", code: 1, userInfo: [NSLocalizedDescriptionKey: "invalid log rotation policy"])
        }
        guard manager.fileExists(atPath: path) else { return }
        let info = try manager.attributesOfItem(atPath: path)
        if (info[.type] as? FileAttributeType) == .typeDirectory {
            throw NSError(domain: "VoiceRewriteLog", code: 1, userInfo: [NSLocalizedDescriptionKey: "log path is a directory: \(path)"])
        }
        let size = (info[.size] as? NSNumber)?.int64Value ?? 0
        if size < maxBytes {
            return
        }
        var i = backups
        while i > 1 {
            let previous = "\(path).\(i - 1)"
            let next = "\(path).\(i)"
            if !manager.fileExists(atPath: previous) {
                i -= 1
                continue
            }
            if manager.fileExists(atPath: next) {
                try manager.removeItem(atPath: next)
            }
            try manager.moveItem(atPath: previous, toPath: next)
            i -= 1
        }
        let oldest = path + ".1"
        if manager.fileExists(atPath: oldest) {
            try manager.removeItem(atPath: oldest)
        }
        try manager.moveItem(atPath: path, toPath: oldest)
    }
}
