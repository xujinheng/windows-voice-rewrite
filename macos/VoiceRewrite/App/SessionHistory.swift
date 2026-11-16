// Source: voice-rewrite/internal/core/history
import Foundation

struct SessionHistoryEntry: Identifiable, Equatable {
    let id: UInt64
    let title: String
    let text: String
    let createdAt: Date
}

struct SessionHistory {
    static let limit = 10
    static let maxBytes = 4 << 20
    static let emptySession = "当前会话还没有可恢复的语音结果。"
    static let clearSession = "清空本次会话历史"
    static var sectionTitle: String { "最近 \(limit) 次结果（仅当前会话）" }

    private(set) var entries: [SessionHistoryEntry] = []
    private var nextID: UInt64 = 0

    static func accept(title: String, value: String) -> String? {
        if value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || value.utf8.count > Self.maxBytes {
            return nil
        }
        if title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return "语音结果"
        }
        return title
    }

    static func keepCount(_ texts: [String]) -> Int {
        var bytes = 0
        var keep = 0
        for text in texts {
            if keep >= Self.limit || bytes + text.utf8.count > Self.maxBytes { return keep }
            bytes += text.utf8.count
            keep += 1
        }
        return keep
    }

    mutating func remember(title: String, value: String) {
        guard let storedTitle = Self.accept(title: title, value: value) else { return }
        nextID += 1
        entries.insert(SessionHistoryEntry(
            id: nextID,
            title: storedTitle,
            text: value,
            createdAt: Date()
        ), at: 0)
        let keep = Self.keepCount(entries.map(\.text))
        if keep < entries.count { entries.removeSubrange(keep...) }
    }

    func entry(id: UInt64) -> SessionHistoryEntry? {
        entries.first { $0.id == id }
    }

    mutating func clear() { entries.removeAll() }

    static func kind(title: String) -> String {
        if title.contains("听写") {
            return "听写"
        }
        return "AI 结果"
    }

    static func menuLine(index: Int, title: String, createdAt: Date) -> String {
        let clock = DateFormatter()
        clock.locale = Locale(identifier: "en_US_POSIX")
        clock.dateFormat = "HH:mm"
        return "\(index + 1). \(kind(title: title)) · \(clock.string(from: createdAt))"
    }
}
