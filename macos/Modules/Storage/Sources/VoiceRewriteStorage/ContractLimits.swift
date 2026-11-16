// Source: voice-rewrite/internal/core/config, shortcut, jsonl, and logfile
import Foundation

public enum ContractLimits {
    public static let recordSecondsMin = 1
    public static let recordSecondsMax = 180
    public static let defaultRecordSeconds = 60
    public static let maxEndpointBytes = 500
    public static let maxModelIDBytes = 150
    public static let maxCredentialBytes = 4000
    public static let defaultASRBaseURL = "http://127.0.0.1:8080/v1"
    public static let defaultLLMBaseURL = "http://127.0.0.1:1234/v1"
    public static let defaultASRModel = "whisper-1"
    public static let defaultTextModel = "auto"
    public static let macDictationShortcut = "Ctrl+Alt+R"
    public static let macRewriteShortcut = "Ctrl+Alt+E"
    public static let macTextEditShortcut = "Ctrl+Alt+T"
    public static let protocolVersion = 1
    public static let maxRequestBytes = 8 << 20
    public static let maxRequestIDBytes = 128
    public static let maxSeenRequestIDs = 65536
    public static let logMaxBytes = 4 << 20
    public static let logBackups = 3
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
