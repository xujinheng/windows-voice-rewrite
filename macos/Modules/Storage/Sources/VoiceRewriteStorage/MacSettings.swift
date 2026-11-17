import Foundation

/// Only non-secret, macOS-owned overrides. Core supplies all other defaults.
/// Do not add API keys or request payloads to this Codable type.
public struct MacSettings: Codable, Equatable, Sendable {
    public var asrBaseURL: String
    public var llmBaseURL: String
    public var asrModel: String
    public var textModel: String
    public var meetingASRMode: String
    public var meetingBackground: String
    public var meetingTerms: String
    public var microphoneUID: String
    public var maxRecordSeconds: Int
    public var dictationShortcut: String
    public var rewriteShortcut: String
    public var recordMode: String
    public var exitShortcut: String
    public var systemPrompt: String
    public var maxResponseTokens: Int
    public var maxSelectedRunes: Int
    public var selectionCopyAttempts: Int
    public var selectionCopyTimeoutMS: Int

    enum CodingKeys: String, CodingKey, CaseIterable {
        case asrBaseURL = "asr_base_url"
        case llmBaseURL = "llm_base_url"
        case asrModel = "asr_model"
        case textModel = "text_model"
        case meetingASRMode = "meeting_asr_mode"
        case meetingBackground = "meeting_background"
        case meetingTerms = "meeting_terms"
        case microphoneUID = "microphone_uid"
        case maxRecordSeconds = "max_record_seconds"
        case dictationShortcut = "dictation_shortcut"
        case rewriteShortcut = "rewrite_shortcut"
        case recordMode = "record_mode"
        case exitShortcut = "hotkey_exit"
        case systemPrompt = "system_prompt"
        case maxResponseTokens = "max_response_tokens"
        case maxSelectedRunes = "max_selected_characters"
        case selectionCopyAttempts = "selection_copy_attempts"
        case selectionCopyTimeoutMS = "selection_copy_timeout_ms"
    }

    public init(
        asrBaseURL: String = ContractLimits.defaultASRBaseURL,
        llmBaseURL: String = ContractLimits.defaultLLMBaseURL,
        asrModel: String = ContractLimits.defaultASRModel,
        textModel: String = ContractLimits.defaultTextModel,
        meetingASRMode: String = ContractLimits.defaultMeetingASRMode,
        meetingBackground: String = "",
        meetingTerms: String = "",
        maxRecordSeconds: Int = ContractLimits.defaultRecordSeconds,
        microphoneUID: String = "",
        dictationShortcut: String = ContractLimits.macDictationShortcut,
        rewriteShortcut: String = ContractLimits.macRewriteShortcut,
        recordMode: String = ContractLimits.defaultRecordMode,
        exitShortcut: String = ContractLimits.defaultExitShortcut,
        systemPrompt: String = "",
        maxResponseTokens: Int = ContractLimits.defaultMaxResponseTokens,
        maxSelectedRunes: Int = ContractLimits.defaultMaxSelectedRunes,
        selectionCopyAttempts: Int = ContractLimits.defaultCopyAttempts,
        selectionCopyTimeoutMS: Int = ContractLimits.defaultCopyTimeoutMS
    ) {
        self.asrBaseURL = asrBaseURL
        self.llmBaseURL = llmBaseURL
        self.asrModel = asrModel
        self.textModel = textModel
        self.meetingASRMode = meetingASRMode
        self.meetingBackground = meetingBackground
        self.meetingTerms = meetingTerms
        self.maxRecordSeconds = maxRecordSeconds
        self.microphoneUID = microphoneUID
        self.dictationShortcut = dictationShortcut
        self.rewriteShortcut = rewriteShortcut
        self.recordMode = recordMode
        self.exitShortcut = exitShortcut
        self.systemPrompt = systemPrompt
        self.maxResponseTokens = maxResponseTokens
        self.maxSelectedRunes = maxSelectedRunes
        self.selectionCopyAttempts = selectionCopyAttempts
        self.selectionCopyTimeoutMS = selectionCopyTimeoutMS
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
        guard meetingBackground.unicodeScalars.count <= 4000,
              meetingTerms.unicodeScalars.count <= 2000 else { throw SettingsError.invalidValues }
        meetingBackground = meetingBackground.trimmingCharacters(in: .whitespacesAndNewlines)
        meetingTerms = meetingTerms.trimmingCharacters(in: .whitespacesAndNewlines)
        guard microphoneUID.utf8.count <= 512 else { throw SettingsError.invalidValues }
        if !Self.recordSeconds.contains(maxRecordSeconds) {
            maxRecordSeconds = ContractLimits.defaultRecordSeconds
        }
        dictationShortcut = normalizedShortcut(dictationShortcut, fallback: ContractLimits.macDictationShortcut)
        rewriteShortcut = normalizedShortcut(rewriteShortcut, fallback: ContractLimits.macRewriteShortcut)
        exitShortcut = normalizedShortcut(exitShortcut, fallback: ContractLimits.defaultExitShortcut)
        guard recordMode == "hold" || recordMode == "toggle",
              systemPrompt.utf8.count <= ContractLimits.maxSystemPromptBytes else { throw SettingsError.invalidValues }
        if !(ContractLimits.minResponseTokens...ContractLimits.maxResponseTokens).contains(maxResponseTokens) {
            maxResponseTokens = ContractLimits.defaultMaxResponseTokens
        }
        if !(1...ContractLimits.maxSelectedRunes).contains(maxSelectedRunes) {
            maxSelectedRunes = ContractLimits.defaultMaxSelectedRunes
        }
        if !(ContractLimits.minCopyAttempts...ContractLimits.maxCopyAttempts).contains(selectionCopyAttempts) {
            selectionCopyAttempts = ContractLimits.defaultCopyAttempts
        }
        if !(ContractLimits.minCopyTimeoutMS...ContractLimits.maxCopyTimeoutMS).contains(selectionCopyTimeoutMS) {
            selectionCopyTimeoutMS = ContractLimits.defaultCopyTimeoutMS
        }
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
        if values["meeting_background"] == nil { values["meeting_background"] = "" }
        if values["meeting_terms"] == nil { values["meeting_terms"] = "" }
        if values["record_mode"] == nil { values["record_mode"] = ContractLimits.defaultRecordMode }
        if values["hotkey_exit"] == nil { values["hotkey_exit"] = ContractLimits.defaultExitShortcut }
        if values["system_prompt"] == nil { values["system_prompt"] = "" }
        if values["max_response_tokens"] == nil { values["max_response_tokens"] = ContractLimits.defaultMaxResponseTokens }
        if values["max_selected_characters"] == nil { values["max_selected_characters"] = ContractLimits.defaultMaxSelectedRunes }
        if values["selection_copy_attempts"] == nil { values["selection_copy_attempts"] = ContractLimits.defaultCopyAttempts }
        if values["selection_copy_timeout_ms"] == nil { values["selection_copy_timeout_ms"] = ContractLimits.defaultCopyTimeoutMS }
        if values["meeting_asr_mode"] == nil {
            values["meeting_asr_mode"] = ContractLimits.defaultMeetingASRMode
        }
        for (key, value) in shortcutDefaults where values[key] == nil {
            values[key] = value
        }
        // Upgrade only the old untouched default pair. Custom shortcuts, or
        // an explicitly chosen Toggle mode, are not silently overwritten.
        if values["dictation_shortcut"] as? String == "Ctrl+Alt+R",
           values["rewrite_shortcut"] as? String == "Ctrl+Alt+E",
           values["record_mode"] as? String == "hold" {
            values["dictation_shortcut"] = ContractLimits.macDictationShortcut
            values["rewrite_shortcut"] = ContractLimits.macRewriteShortcut
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
