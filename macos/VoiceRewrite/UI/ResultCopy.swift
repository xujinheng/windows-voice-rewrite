import Foundation

/// Result-card and history titles. The strings match internal/core/ux.
enum ResultCopy {
    static let dictationHistory = "听写结果"
    static let dictationNotInserted = "听写结果 · 未自动插入"
    static let dictationCopyManually = "听写结果 · 请手动复制"

    static func cardTitle(intent: String) -> String {
        switch intent {
        case "edit": return "改写预览"
        case "summarize": return "AI 摘要"
        case "translate": return "翻译结果"
        case "ambiguous": return "AI 回答"
        default: return "AI 解释"
        }
    }

    static func historyTitle(intent: String) -> String {
        let trimmed = intent.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? "AI 结果" : "AI " + trimmed
    }

    static func historyRestoreTitle(_ storedTitle: String) -> String {
        storedTitle.trimmingCharacters(in: .whitespacesAndNewlines) + " · 历史结果"
    }
}
