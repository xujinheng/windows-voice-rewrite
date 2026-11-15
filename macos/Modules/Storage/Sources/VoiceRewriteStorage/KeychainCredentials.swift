import Foundation
#if canImport(Security)
import Security
#endif

public enum CredentialAccount: String, CaseIterable, Sendable {
    case asr = "asr_api_key"
    case llm = "llm_api_key"
}

public enum CredentialError: Error, Equatable, CustomStringConvertible {
    case notFound
    case duplicate
    case lockedOrInteractionRequired
    case accessDenied
    case unavailable
    case invalidStoredData
    case osStatus(Int32)
    case unsupportedPlatform

    public var description: String {
        switch self {
        case .notFound: return "Keychain item not found"
        case .duplicate: return "Keychain item already exists"
        case .lockedOrInteractionRequired: return "Keychain locked or interaction required"
        case .accessDenied: return "Keychain access denied"
        case .unavailable: return "Keychain unavailable"
        case .invalidStoredData: return "Keychain returned invalid credential data"
        case .osStatus(let status): return "Keychain OSStatus \(status)"
        case .unsupportedPlatform: return "Security.framework requires macOS"
        }
    }

    internal static func from(status: Int32) -> CredentialError {
        switch status {
        case -25300: return .notFound           // errSecItemNotFound
        case -25299: return .duplicate          // errSecDuplicateItem
        case -25308: return .lockedOrInteractionRequired // errSecInteractionNotAllowed
        case -25293, -34018, -128: return .accessDenied   // auth failed / missing entitlement / canceled
        case -25291: return .unavailable        // errSecNotAvailable
        default: return .osStatus(status)
        }
    }
}

public protocol CredentialReading {
    func read(_ account: CredentialAccount) throws -> String
}

#if canImport(Security)
// Protocol is internal: tests inject statuses without claiming they verify the OS Keychain.
internal protocol KeychainOperations {
    func add(_ attributes: [String: Any]) -> OSStatus
    func copy(_ query: [String: Any]) -> (OSStatus, Data?)
    func update(_ query: [String: Any], with attributes: [String: Any]) -> OSStatus
    func delete(_ query: [String: Any]) -> OSStatus
}

private struct SecurityOperations: KeychainOperations {
    func add(_ attributes: [String: Any]) -> OSStatus {
        SecItemAdd(attributes as CFDictionary, nil)
    }
    func copy(_ query: [String: Any]) -> (OSStatus, Data?) {
        var value: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &value)
        return (status, value as? Data)
    }
    func update(_ query: [String: Any], with attributes: [String: Any]) -> OSStatus {
        SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
    }
    func delete(_ query: [String: Any]) -> OSStatus {
        SecItemDelete(query as CFDictionary)
    }
}
#endif

/// Real generic-password Keychain; never falls back to an unprotected store.
public final class KeychainCredentials: CredentialReading {
    public static let service = "com.xujinheng.VoiceRewrite"
    private let serviceName: String
    #if canImport(Security)
    private let operations: any KeychainOperations

    public init(service: String = KeychainCredentials.service) {
        self.serviceName = service
        self.operations = SecurityOperations()
    }

    internal init(service: String, operations: any KeychainOperations) {
        self.serviceName = service
        self.operations = operations
    }

    private func query(_ account: CredentialAccount) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: serviceName,
         kSecAttrAccount as String: account.rawValue,
         kSecUseDataProtectionKeychain as String: true]
    }

    public func add(_ secret: String, for account: CredentialAccount) throws {
        var attributes = query(account)
        attributes[kSecValueData as String] = Data(secret.utf8)
        attributes[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        let status = operations.add(attributes)
        guard status == errSecSuccess else { throw CredentialError.from(status: status) }
    }

    public func read(_ account: CredentialAccount) throws -> String {
        var attributes = query(account)
        attributes[kSecReturnData as String] = true
        attributes[kSecMatchLimit as String] = kSecMatchLimitOne
        attributes[kSecUseAuthenticationUI as String] = kSecUseAuthenticationUIFail
        let (status, data) = operations.copy(attributes)
        guard status == errSecSuccess else { throw CredentialError.from(status: status) }
        guard let data, let secret = String(data: data, encoding: .utf8) else {
            throw CredentialError.invalidStoredData
        }
        return secret
    }

    /// Update requires an existing account; it never silently creates one.
    public func update(_ secret: String, for account: CredentialAccount) throws {
        let status = operations.update(query(account), with: [kSecValueData as String: Data(secret.utf8)])
        guard status == errSecSuccess else { throw CredentialError.from(status: status) }
    }

    public func delete(_ account: CredentialAccount) throws {
        let status = operations.delete(query(account))
        guard status == errSecSuccess else { throw CredentialError.from(status: status) }
    }
    #else
    public init(service: String = KeychainCredentials.service) { self.serviceName = service }
    public func add(_ secret: String, for account: CredentialAccount) throws { throw CredentialError.unsupportedPlatform }
    public func read(_ account: CredentialAccount) throws -> String { throw CredentialError.unsupportedPlatform }
    public func update(_ secret: String, for account: CredentialAccount) throws { throw CredentialError.unsupportedPlatform }
    public func delete(_ account: CredentialAccount) throws { throw CredentialError.unsupportedPlatform }
    #endif
}

/// Construct only at the IPC boundary. This class is deliberately not Codable,
/// and SettingsStore accepts only MacSettings (never this request dictionary).
public struct HelperConfigFactory {
    private let credentials: any CredentialReading

    public init(credentials: any CredentialReading) {
        self.credentials = credentials
    }

    public func withEphemeralConfig<Result>(
        settings: MacSettings,
        _ performRequest: ([String: Any]) throws -> Result
    ) throws -> Result {
        try settings.validateForPersistence()
        let asr = try credentials.read(.asr)
        let llm = try credentials.read(.llm)
        return try performRequest([
            "asr_base_url": settings.asrBaseURL,
            "llm_base_url": settings.llmBaseURL,
            "asr_model": settings.asrModel,
            "text_model": settings.textModel,
            "max_record_seconds": settings.maxRecordSeconds,
            "asr_api_key": asr,
            "llm_api_key": llm,
        ])
    }
}
