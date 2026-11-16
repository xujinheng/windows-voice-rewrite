import Foundation

/// The same rune limit core `SelectedLengthError` uses. An empty selection is
/// not an error here; the capture path rejects that separately.
public enum SelectionLimit {
    public static let defaultMaxScalars = 12000

    public static func tooLongMessage(limit: Int = defaultMaxScalars, selected: String) -> String? {
        let bounded = limit > 0 ? limit : defaultMaxScalars
        guard selected.unicodeScalars.count > bounded else { return nil }
        return "选中文字超过 \(bounded) 字限制"
    }
}
