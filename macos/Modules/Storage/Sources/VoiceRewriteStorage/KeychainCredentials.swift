import Foundation
#if canImport(Security)
import Security
#endif

public enum CredentialAccount: String, CaseIterable, Sendable {
    case asr = "asr_api_key"
    case llm = "llm_api_key"
}

public enum CredentialError: Error, Equatable, CustomStringConvertible, LocalizedError {
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

    public var errorDescription: String? {
        switch self {
        case .notFound: return "未找到 Keychain 凭据"
        case .duplicate: return "Keychain 中已存在此凭据"
        case .lockedOrInteractionRequired: return "Keychain 已锁定或需要解锁后重试"
        case .accessDenied: return "Keychain 拒绝访问；请检查应用签名及钥匙串权限"
        case .unavailable: return "Keychain 当前不可用"
        case .invalidStoredData: return "Keychain 中的凭据数据无效"
        case .osStatus(let status): return "Keychain 操作失败（OSStatus \(status)）"
        case .unsupportedPlatform: return "当前平台不支持 macOS Keychain"
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
public enum HelperOperation: Sendable {
    case transcribe
    case selectedTask
    case spokenSelectedTask
}

public struct HelperConfigFactory {
    private let credentials: any CredentialReading

    public init(credentials: any CredentialReading) {
        self.credentials = credentials
    }

    private func credentialOrEmpty(_ account: CredentialAccount) throws -> String {
        do {
            return try credentials.read(account)
        } catch CredentialError.notFound {
            // The Go Core accepts local API endpoints without an Authorization header.
            return ""
        }
    }

    public func withEphemeralConfig<Result>(
        settings: MacSettings,
        operation: HelperOperation,
        _ performRequest: ([String: Any]) throws -> Result
    ) throws -> Result {
        var settings = settings
        try settings.canonicalize()
        // Do not read or transmit a credential that the requested operation will not use.
        let asr: String
        let llm: String
        switch operation {
        case .transcribe:
            asr = try credentialOrEmpty(.asr)
            llm = ""
        case .selectedTask:
            asr = ""
            llm = try credentialOrEmpty(.llm)
        case .spokenSelectedTask:
            asr = try credentialOrEmpty(.asr)
            llm = try credentialOrEmpty(.llm)
        }
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
