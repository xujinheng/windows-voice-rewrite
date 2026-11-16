// Source: voice-rewrite/internal/core/config, shortcut, jsonl, and logfile
import Foundation

public enum ContractLimits {
    public static let recordSecondsMin = 1
    public static let recordSecondsMax = 180
    public static let defaultRecordSeconds = 60
    public static let maxEndpointBytes = 500
    public static let maxModelIDBytes = 150
    public static let maxCredentialBytes = 4000
    public static let credentialTooLong = "输入内容过长"
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
    public static let helperName = "VoiceRewrite"

    public static func storedCredentialTooLong(_ value: String) -> Bool {
        value.utf8.count > maxCredentialBytes
    }

    public static func canonicalEndpoints(asrBaseURL: String, asrModel: String, llmBaseURL: String, textModel: String) -> CanonicalEndpointFields? {
        let asrURL = trimEndpoint(asrBaseURL)
        let llmURL = trimEndpoint(llmBaseURL)
        var asr = asrModel.trimmingCharacters(in: .whitespacesAndNewlines)
        var text = textModel.trimmingCharacters(in: .whitespacesAndNewlines)
        if asrURL.utf8.count > maxEndpointBytes || llmURL.utf8.count > maxEndpointBytes || asr.utf8.count > maxModelIDBytes || text.utf8.count > maxModelIDBytes {
            return nil
        }
        if !MacSettings.acceptsBaseURL(asrURL) || !MacSettings.acceptsBaseURL(llmURL) {
            return nil
        }
        if asr.isEmpty { asr = defaultASRModel }
        if text.isEmpty { text = defaultTextModel }
        return CanonicalEndpointFields(asrBaseURL: asrURL, asrModel: asr, llmBaseURL: llmURL, textModel: text)
    }

    private static func trimEndpoint(_ value: String) -> String {
        var text = value.trimmingCharacters(in: .whitespacesAndNewlines)
        while text.hasSuffix("/") { text.removeLast() }
        return text
    }
}

public struct CanonicalEndpointFields: Equatable {
    public var asrBaseURL: String
    public var asrModel: String
    public var llmBaseURL: String
    public var textModel: String
}

extension MacSettings {
    public static func acceptsBaseURL(_ value: String) -> Bool {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URLComponents(string: trimmed),
              let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              let host = url.host, !host.isEmpty,
              url.user == nil, url.password == nil,
              (url.percentEncodedQuery ?? "").isEmpty,
              (url.fragment ?? "").isEmpty,
              trimmed.contains("://"),
              url.url != nil else {
            return false
        }
        return true
    }
}
