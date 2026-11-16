import AppKit
import Foundation

/// Bounded diagnostic log. Same 4 MiB active file and three backups as Windows.
enum DiagnosticLog {
    static let maxBytes: Int64 = 4 << 20
    static let backups = 3

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

    static func reveal() {
        prepare()
        let url = fileURL
        if !FileManager.default.fileExists(atPath: url.path) {
            try? Data().write(to: url)
        }
        NSWorkspace.shared.activateFileViewerSelecting([url])
    }

    static func rotate(_ path: String, maxBytes: Int64, backups: Int) throws {
        guard backups >= 1, maxBytes >= 1 else { throw CocoaError(.fileReadUnknown) }
        let manager = FileManager.default
        guard manager.fileExists(atPath: path) else { return }
        let info = try manager.attributesOfItem(atPath: path)
        if (info[.type] as? FileAttributeType) == .typeDirectory { throw CocoaError(.fileReadUnknown) }
        let size = (info[.size] as? NSNumber)?.int64Value ?? 0
        if size < maxBytes { return }
        if backups > 1 {
            for index in stride(from: backups, to: 1, by: -1) {
                let previous = "\(path).\(index - 1)"
                let next = "\(path).\(index)"
                guard manager.fileExists(atPath: previous) else { continue }
                if manager.fileExists(atPath: next) {
                    try manager.removeItem(atPath: next)
                }
                try manager.moveItem(atPath: previous, toPath: next)
            }
        }
        let oldest = path + ".1"
        if manager.fileExists(atPath: oldest) {
            try manager.removeItem(atPath: oldest)
        }
        try manager.moveItem(atPath: path, toPath: oldest)
    }
}
