import Foundation

/// Result-card and history titles. The strings match internal/core/ux.
enum ResultCopy {
    static let dictationHistory = "听写结果"
    static let dictationNotInserted = "听写结果 · 未自动插入"
    static let dictationCopyManually = "听写结果 · 请手动复制"
    static let copySucceeded = "已复制结果到剪贴板"
    static let copyFailed = "复制失败，请尝试选中正文复制"
    static let passwordDictation = "密码输入框不接受自动听写，请使用普通输入方式"
    static let passwordSelection = "密码输入框不允许读取选中文字或发送给 AI"
    static let passwordRewrite = "已拒绝在密码输入框中自动改写"

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
