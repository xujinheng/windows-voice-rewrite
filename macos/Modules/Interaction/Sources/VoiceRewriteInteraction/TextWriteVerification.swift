import Foundation

/// Verifies a replacement against the full AXValue using UTF-16 offsets.
/// A clipboard paste is not considered successful merely because Cmd+V was posted.
public enum TextWriteVerification {
    public static func expectedValue(
        original: String?, selectedText: String, range: TextRange, replacement: String
    ) -> String? {
        guard let original else { return nil }
        guard range.location >= 0, range.length >= 0,
              range.length <= Int.max - range.location,
              range.location + range.length <= original.utf16.count,
              selectedText.utf16.count == range.length else { return nil }
        // Foundation can bridge an NSRange in the middle of a surrogate pair
        // by replacing the half-surrogates with U+FFFD. That must not authorize a paste.
        let units = Array(original.utf16)
        func splitsSurrogatePair(at offset: Int) -> Bool {
            guard offset > 0, offset < units.count else { return false }
            return (0xD800...0xDBFF).contains(units[offset - 1])
                && (0xDC00...0xDFFF).contains(units[offset])
        }
        guard !splitsSurrogatePair(at: range.location),
              !splitsSurrogatePair(at: range.location + range.length) else { return nil }
        let nsRange = NSRange(location: range.location, length: range.length)
        guard Range(nsRange, in: original) != nil else { return nil }
        let actual = (original as NSString).substring(with: nsRange)
        guard Array(actual.utf16) == Array(selectedText.utf16) else { return nil }
        return (original as NSString).replacingCharacters(in: nsRange, with: replacement)
    }

    public static func confirms(actual: String, expected: String) -> Bool {
        Array(actual.utf16) == Array(expected.utf16)
    }
}
