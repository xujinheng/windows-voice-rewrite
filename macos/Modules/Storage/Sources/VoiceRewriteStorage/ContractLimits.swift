// Source: voice-rewrite/internal/core/config, meeting, shortcut, jsonl, and logfile
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
    public static let meetingASRMultipart = "multipart"
    public static let meetingASRLlamaPrefix = "llama-prefix"
    public static let defaultMeetingASRMode = "llama-prefix"
    public static let meetingASRMultipartTitle = "分段音频识别"
    public static let meetingASRLlamaPrefixTitle = "累积音频 / 前缀续写"
    public static let meetingASRModes = [meetingASRMultipart, meetingASRLlamaPrefix]
    public static let meetingButtonCaptions = 7101
    public static let meetingButtonExpression = 7115
    public static let meetingCaptionsFocusTitle = "专注字幕"
    public static let meetingCaptionsFullTitle = "完整视图"
    public static let meetingExpressionExpandTitle = "放大英文"
    public static let meetingExpressionCollapseTitle = "收起英文"
    public static let macDictationShortcut = "Ctrl+Alt+R"
    public static let macRewriteShortcut = "Ctrl+Alt+E"
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

    public static func baseURLEscapesOK(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        if !percentBytesOK(bytes, hostRule: false) { return false }
        return percentBytesOK(Array(baseURLHost(value).utf8), hostRule: true)
    }

    private static func baseURLHost(_ value: String) -> String {
        guard let marker = value.range(of: "://") else { return "" }
        var rest = String(value[marker.upperBound...])
        if let cut = rest.firstIndex(where: { "/?#".contains($0) }) {
            rest = String(rest[..<cut])
        }
        if let at = rest.lastIndex(of: "@") {
            rest = String(rest[rest.index(after: at)...])
        }
        return rest
    }

    private static func percentBytesOK(_ bytes: [UInt8], hostRule: Bool) -> Bool {
        var index = 0
        while index < bytes.count {
            if bytes[index] != UInt8(ascii: "%") {
                index += 1
                continue
            }
            if index + 2 >= bytes.count || hexNibble(bytes[index + 1]) < 0 || hexNibble(bytes[index + 2]) < 0 {
                return false
            }
            if hostRule && String(bytes: bytes[index...index + 2], encoding: .utf8) != "%25" && hexNibble(bytes[index + 1]) < 8 {
                return false
            }
            index += 3
        }
        return true
    }

    private static func hexNibble(_ byte: UInt8) -> Int {
        switch byte {
        case UInt8(ascii: "0")...UInt8(ascii: "9"):
            return Int(byte - UInt8(ascii: "0"))
        case UInt8(ascii: "a")...UInt8(ascii: "f"):
            return Int(byte - UInt8(ascii: "a")) + 10
        case UInt8(ascii: "A")...UInt8(ascii: "F"):
            return Int(byte - UInt8(ascii: "A")) + 10
        default:
            return -1
        }
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
        guard ContractLimits.baseURLEscapesOK(trimmed),
              let url = URLComponents(string: trimmed),
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
