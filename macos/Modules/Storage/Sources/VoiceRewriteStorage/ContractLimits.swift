// Source: voice-rewrite/internal/core/config
import Foundation

public enum ContractLimits {
    public static let recordSecondsMin = 1
    public static let recordSecondsMax = 180
    public static let defaultRecordSeconds = 60
    public static let maxEndpointBytes = 500
    public static let maxModelIDBytes = 150
    public static let maxCredentialBytes = 4000
}

extension MacSettings {
    public static func acceptsBaseURL(_ value: String) -> Bool {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.contains("://"),
              let url = URLComponents(string: trimmed),
              let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              let host = url.host, !host.isEmpty,
              url.user == nil, url.password == nil,
              (url.percentEncodedQuery ?? "").isEmpty,
              (url.fragment ?? "").isEmpty,
              url.url != nil else {
            return false
        }
        return true
    }
}
