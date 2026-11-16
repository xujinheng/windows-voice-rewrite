import Foundation

/// In-memory session results. Same bounds and menu text as internal/core/history.
struct SessionHistoryEntry: Identifiable, Equatable {
    let id: UInt64
    let title: String
    let text: String
    let createdAt: Date
}

struct SessionHistory {
    static let limit = 10
    static let maxBytes = 4 << 20

    private(set) var entries: [SessionHistoryEntry] = []
    private var nextID: UInt64 = 0

    mutating func remember(title: String, value: String) {
        let text = value
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              text.utf8.count <= Self.maxBytes else { return }
        let trimmedTitle = title.trimmingCharacters(in: .whitespacesAndNewlines)
        nextID += 1
        entries.insert(SessionHistoryEntry(
            id: nextID,
            title: trimmedTitle.isEmpty ? "语音结果" : trimmedTitle,
            text: text,
            createdAt: Date()
        ), at: 0)
        var bytes = 0
        var keep = 0
        for entry in entries {
            let size = entry.text.utf8.count
            if keep >= Self.limit || bytes + size > Self.maxBytes { break }
            bytes += size
            keep += 1
        }
        if keep < entries.count { entries.removeSubrange(keep...) }
    }

    func entry(id: UInt64) -> SessionHistoryEntry? {
        entries.first { $0.id == id }
    }

    mutating func clear() { entries.removeAll() }

    static func kind(title: String) -> String {
        title.contains("听写") ? "听写" : "AI 结果"
    }

    static func menuLine(index: Int, title: String, createdAt: Date) -> String {
        let clock = DateFormatter()
        clock.locale = Locale(identifier: "en_US_POSIX")
        clock.dateFormat = "HH:mm"
        return "\(index + 1). \(kind(title: title)) · \(clock.string(from: createdAt))"
    }
}
