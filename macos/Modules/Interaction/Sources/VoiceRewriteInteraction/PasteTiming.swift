// Source: voice-rewrite/internal/core/clipboard
import Foundation

public enum PasteTiming {
    public static let quietMilliseconds = 200
    public static let minimumMilliseconds = 700
    public static let maxMilliseconds = 3000
    public static let pollMilliseconds = 100
    public static func restoreReady(elapsed: TimeInterval, sinceLastRead: TimeInterval?, hasRead: Bool) -> Bool {
        elapsed >= Double(maxMilliseconds) / 1000 ||
            (elapsed >= Double(minimumMilliseconds) / 1000 && hasRead &&
             (sinceLastRead ?? 0) >= Double(quietMilliseconds) / 1000)
    }
}
