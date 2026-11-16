import Foundation

/// Only non-secret, macOS-owned overrides. Core supplies all other defaults.
/// Do not add API keys or request payloads to this Codable type.
public struct MacSettings: Codable, Equatable, Sendable {
    public var asrBaseURL: String
    public var llmBaseURL: String
    public var asrModel: String
    public var textModel: String
    public var maxRecordSeconds: Int
    public var dictationShortcut: String
    public var rewriteShortcut: String
    public var textEditShortcut: String

    enum CodingKeys: String, CodingKey, CaseIterable {
        case asrBaseURL = "asr_base_url"
        case llmBaseURL = "llm_base_url"
        case asrModel = "asr_model"
        case textModel = "text_model"
        case maxRecordSeconds = "max_record_seconds"
        case dictationShortcut = "dictation_shortcut"
        case rewriteShortcut = "rewrite_shortcut"
        case textEditShortcut = "text_edit_shortcut"
    }

    public init(
        asrBaseURL: String = "http://127.0.0.1:8080/v1",
        llmBaseURL: String = "http://127.0.0.1:1234/v1",
        asrModel: String = "whisper-1",
        textModel: String = "auto",
        maxRecordSeconds: Int = 60,
        dictationShortcut: String = "Ctrl+Alt+R",
        rewriteShortcut: String = "Ctrl+Alt+E",
        textEditShortcut: String = "Ctrl+Alt+T"
    ) {
        self.asrBaseURL = asrBaseURL
        self.llmBaseURL = llmBaseURL
        self.asrModel = asrModel
        self.textModel = textModel
        self.maxRecordSeconds = maxRecordSeconds
        self.dictationShortcut = dictationShortcut
        self.rewriteShortcut = rewriteShortcut
        self.textEditShortcut = textEditShortcut
    }

    public static let defaults = MacSettings()
    /// Same inclusive range core Normalize keeps. Values outside it become 60.
    public static let recordSeconds = 1...180
    /// Same byte limit as core MaxCredentialBytes. A blank field still means keep the saved key.
    public static let maxCredentialBytes = 4000

    public static func credentialTooLong(_ value: String) -> Bool {
        value.trimmingCharacters(in: .whitespacesAndNewlines).utf8.count > maxCredentialBytes
    }

    /// Applies the core recording-length and base-URL rules before the values
    /// are stored or sent to the helper.
    public mutating func canonicalize() throws {
        asrBaseURL = trimEndpoint(asrBaseURL)
        llmBaseURL = trimEndpoint(llmBaseURL)
        asrModel = asrModel.trimmingCharacters(in: .whitespacesAndNewlines)
        textModel = textModel.trimmingCharacters(in: .whitespacesAndNewlines)
        if !Self.recordSeconds.contains(maxRecordSeconds) {
            maxRecordSeconds = 60
        }
        if asrModel.isEmpty { asrModel = "whisper-1" }
        if textModel.isEmpty { textModel = "auto" }
        dictationShortcut = normalizedShortcut(dictationShortcut, fallback: "Ctrl+Alt+R")
        rewriteShortcut = normalizedShortcut(rewriteShortcut, fallback: "Ctrl+Alt+E")
        textEditShortcut = normalizedShortcut(textEditShortcut, fallback: "Ctrl+Alt+T")
        guard asrBaseURL.utf8.count <= 500, llmBaseURL.utf8.count <= 500,
              asrModel.utf8.count <= 150, textModel.utf8.count <= 150,
              Self.acceptsBaseURL(asrBaseURL),
              Self.acceptsBaseURL(llmBaseURL) else {
            throw SettingsError.invalidValues
        }
    }

    public func validateForPersistence() throws {
        var copy = self
        try copy.canonicalize()
    }

    /// Matches core ValidateBaseURL: http(s), host, no userinfo, query, or fragment.
    public static func acceptsBaseURL(_ value: String) -> Bool {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.contains("://"),
              let url = URLComponents(string: trimmed),
              let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              let host = url.host, !host.isEmpty,
              url.user == nil, url.password == nil,
              url.percentEncodedQuery?.isEmpty != false,
              url.fragment == nil,
              url.url != nil else {
            return false
        }
        return true
    }

    private func trimEndpoint(_ value: String) -> String {
        var text = value.trimmingCharacters(in: .whitespacesAndNewlines)
        while text.hasSuffix("/") { text.removeLast() }
        return text
    }

    private func normalizedShortcut(_ value: String, fallback: String) -> String {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? fallback : trimmed
    }

    static func decodeStrict(_ data: Data) throws -> MacSettings {
        // JSONDecoder silently ignores unknown members; reject them, especially
        // secret names, before decoding, and require every declared field.
        guard var values = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw SettingsError.invalidJSON
        }
        let shortcutDefaults = [
            "dictation_shortcut": "Ctrl+Alt+R",
            "rewrite_shortcut": "Ctrl+Alt+E",
            "text_edit_shortcut": "Ctrl+Alt+T",
        ]
        for (key, value) in shortcutDefaults where values[key] == nil {
            values[key] = value
        }
        guard Set(values.keys) == Set(CodingKeys.allCases.map(\.rawValue)),
              let encoded = try? JSONSerialization.data(withJSONObject: values),
              var settings = try? JSONDecoder().decode(MacSettings.self, from: encoded) else {
            throw SettingsError.invalidJSON
        }
        try settings.canonicalize()
        return settings
    }
}

public enum SettingsError: Error, Equatable {
    case invalidJSON
    case invalidValues
    case insecureStorage
    case fileTooLarge
    /// POSIX error only: never contains file contents, credentials, or request data.
    case io(operation: String, code: Int32)
}
