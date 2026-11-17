import Foundation

/// Only non-secret, macOS-owned overrides. Core supplies all other defaults.
/// Do not add API keys or request payloads to this Codable type.
public struct MacSettings: Codable, Equatable, Sendable {
    public var asrBaseURL: String
    public var llmBaseURL: String
    public var asrModel: String
    public var textModel: String
    public var meetingASRMode: String
    public var microphoneUID: String
    public var maxRecordSeconds: Int
    public var dictationShortcut: String
    public var rewriteShortcut: String

    enum CodingKeys: String, CodingKey, CaseIterable {
        case asrBaseURL = "asr_base_url"
        case llmBaseURL = "llm_base_url"
        case asrModel = "asr_model"
        case textModel = "text_model"
        case meetingASRMode = "meeting_asr_mode"
        case microphoneUID = "microphone_uid"
        case maxRecordSeconds = "max_record_seconds"
        case dictationShortcut = "dictation_shortcut"
        case rewriteShortcut = "rewrite_shortcut"
    }

    public init(
        asrBaseURL: String = ContractLimits.defaultASRBaseURL,
        llmBaseURL: String = ContractLimits.defaultLLMBaseURL,
        asrModel: String = ContractLimits.defaultASRModel,
        textModel: String = ContractLimits.defaultTextModel,
        meetingASRMode: String = ContractLimits.defaultMeetingASRMode,
        maxRecordSeconds: Int = ContractLimits.defaultRecordSeconds,
        microphoneUID: String = "",
        dictationShortcut: String = ContractLimits.macDictationShortcut,
        rewriteShortcut: String = ContractLimits.macRewriteShortcut
    ) {
        self.asrBaseURL = asrBaseURL
        self.llmBaseURL = llmBaseURL
        self.asrModel = asrModel
        self.textModel = textModel
        self.meetingASRMode = meetingASRMode
        self.maxRecordSeconds = maxRecordSeconds
        self.microphoneUID = microphoneUID
        self.dictationShortcut = dictationShortcut
        self.rewriteShortcut = rewriteShortcut
    }

    public static let defaults = MacSettings()
    public static let recordSeconds = ContractLimits.recordSecondsMin...ContractLimits.recordSecondsMax
    public static let maxCredentialBytes = ContractLimits.maxCredentialBytes

    public static func credentialTooLong(_ value: String) -> Bool {
        ContractLimits.storedCredentialTooLong(value)
    }

    /// An empty field keeps the Keychain value. Spaces are a typed value and replace it.
    public static func replacesStoredCredential(_ value: String) -> Bool {
        !value.isEmpty
    }

    /// Applies the core recording-length and base-URL rules before the values
    /// are stored or sent to the helper.
    public mutating func canonicalize() throws {
        guard let endpoints = ContractLimits.canonicalEndpoints(
            asrBaseURL: asrBaseURL,
            asrModel: asrModel,
            llmBaseURL: llmBaseURL,
            textModel: textModel) else {
            throw SettingsError.invalidValues
        }
        asrBaseURL = endpoints.asrBaseURL
        llmBaseURL = endpoints.llmBaseURL
        asrModel = endpoints.asrModel
        textModel = endpoints.textModel
        guard ContractLimits.meetingASRModes.contains(meetingASRMode) else {
            throw SettingsError.invalidValues
        }
        guard microphoneUID.utf8.count <= 512 else { throw SettingsError.invalidValues }
        if !Self.recordSeconds.contains(maxRecordSeconds) {
            maxRecordSeconds = ContractLimits.defaultRecordSeconds
        }
        dictationShortcut = normalizedShortcut(dictationShortcut, fallback: ContractLimits.macDictationShortcut)
        rewriteShortcut = normalizedShortcut(rewriteShortcut, fallback: ContractLimits.macRewriteShortcut)
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
        values.removeValue(forKey: "text_edit_shortcut")
        let shortcutDefaults = [
            "dictation_shortcut": ContractLimits.macDictationShortcut,
            "rewrite_shortcut": ContractLimits.macRewriteShortcut,
        ]
        if values["microphone_uid"] == nil { values["microphone_uid"] = "" }
        if values["meeting_asr_mode"] == nil {
            values["meeting_asr_mode"] = ContractLimits.defaultMeetingASRMode
        }
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
