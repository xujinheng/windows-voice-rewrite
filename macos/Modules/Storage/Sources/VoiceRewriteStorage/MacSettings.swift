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
        asrBaseURL: String = ContractLimits.defaultASRBaseURL,
        llmBaseURL: String = ContractLimits.defaultLLMBaseURL,
        asrModel: String = ContractLimits.defaultASRModel,
        textModel: String = ContractLimits.defaultTextModel,
        maxRecordSeconds: Int = ContractLimits.defaultRecordSeconds,
        dictationShortcut: String = ContractLimits.macDictationShortcut,
        rewriteShortcut: String = ContractLimits.macRewriteShortcut,
        textEditShortcut: String = ContractLimits.macTextEditShortcut
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
    public static let recordSeconds = ContractLimits.recordSecondsMin...ContractLimits.recordSecondsMax
    public static let maxCredentialBytes = ContractLimits.maxCredentialBytes

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
            maxRecordSeconds = ContractLimits.defaultRecordSeconds
        }
        if asrModel.isEmpty { asrModel = ContractLimits.defaultASRModel }
        if textModel.isEmpty { textModel = ContractLimits.defaultTextModel }
        dictationShortcut = normalizedShortcut(dictationShortcut, fallback: ContractLimits.macDictationShortcut)
        rewriteShortcut = normalizedShortcut(rewriteShortcut, fallback: ContractLimits.macRewriteShortcut)
        textEditShortcut = normalizedShortcut(textEditShortcut, fallback: ContractLimits.macTextEditShortcut)
        guard asrBaseURL.utf8.count <= ContractLimits.maxEndpointBytes,
              llmBaseURL.utf8.count <= ContractLimits.maxEndpointBytes,
              asrModel.utf8.count <= ContractLimits.maxModelIDBytes,
              textModel.utf8.count <= ContractLimits.maxModelIDBytes,
              Self.acceptsBaseURL(asrBaseURL),
              Self.acceptsBaseURL(llmBaseURL) else {
            throw SettingsError.invalidValues
        }
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
            "dictation_shortcut": ContractLimits.macDictationShortcut,
            "rewrite_shortcut": ContractLimits.macRewriteShortcut,
            "text_edit_shortcut": ContractLimits.macTextEditShortcut,
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
