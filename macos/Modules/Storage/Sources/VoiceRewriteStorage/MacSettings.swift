import Foundation

/// Only non-secret, macOS-owned overrides. Core supplies all other defaults.
/// Do not add API keys or request payloads to this Codable type.
public struct MacSettings: Codable, Equatable, Sendable {
    public var asrBaseURL: String
    public var llmBaseURL: String
    public var asrModel: String
    public var textModel: String
    public var maxRecordSeconds: Int

    enum CodingKeys: String, CodingKey, CaseIterable {
        case asrBaseURL = "asr_base_url"
        case llmBaseURL = "llm_base_url"
        case asrModel = "asr_model"
        case textModel = "text_model"
        case maxRecordSeconds = "max_record_seconds"
    }

    public init(
        asrBaseURL: String = "http://127.0.0.1:8080/v1",
        llmBaseURL: String = "http://127.0.0.1:1234/v1",
        asrModel: String = "whisper-1",
        textModel: String = "auto",
        maxRecordSeconds: Int = 60
    ) {
        self.asrBaseURL = asrBaseURL
        self.llmBaseURL = llmBaseURL
        self.asrModel = asrModel
        self.textModel = textModel
        self.maxRecordSeconds = maxRecordSeconds
    }

    public static let defaults = MacSettings()

    /// Minimal safety checks for persisted values, not a copy of Core.Normalize.
    public func validateForPersistence() throws {
        guard maxRecordSeconds > 0, maxRecordSeconds <= 180,
              !asrModel.isEmpty, !textModel.isEmpty else {
            throw SettingsError.invalidValues
        }
        for value in [asrBaseURL, llmBaseURL] {
            guard let url = URLComponents(string: value),
                  ["http", "https"].contains(url.scheme ?? ""),
                  let host = url.host, !host.isEmpty,
                  url.user == nil, url.password == nil,
                  url.query == nil, url.fragment == nil,
                  url.url != nil else {
                throw SettingsError.invalidValues
            }
        }
    }

    static func decodeStrict(_ data: Data) throws -> MacSettings {
        // JSONDecoder silently ignores unknown members; reject them, especially
        // secret names, before decoding, and require every declared field.
        guard let object = try? JSONSerialization.jsonObject(with: data),
              let values = object as? [String: Any],
              Set(values.keys) == Set(CodingKeys.allCases.map(\.rawValue)),
              let settings = try? JSONDecoder().decode(MacSettings.self, from: data) else {
            throw SettingsError.invalidJSON
        }
        try settings.validateForPersistence()
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
