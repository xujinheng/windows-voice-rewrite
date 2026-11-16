// Source: voice-rewrite/internal/core/textedit
import Foundation

public enum SelectionLimit {
    public static let defaultMaxScalars = 12000

    public static func tooLongMessage(limit: Int = defaultMaxScalars, selected: String) -> String? {
        let bounded = limit > 0 ? limit : defaultMaxScalars
        guard selected.unicodeScalars.count > bounded else { return nil }
        return "选中文字超过 \(bounded) 字限制"
    }
}
