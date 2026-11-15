import Foundation
import XCTest
@testable import VoiceRewriteStorage

final class KeychainCredentialsTests: XCTestCase {
    func testStatusMappingIsExplicitAndDoesNotContainSecrets() {
        XCTAssertEqual(CredentialError.from(status: -25300), .notFound)
        XCTAssertEqual(CredentialError.from(status: -25299), .duplicate)
        XCTAssertEqual(CredentialError.from(status: -25308), .lockedOrInteractionRequired)
        XCTAssertEqual(CredentialError.from(status: -25293), .accessDenied)
        XCTAssertEqual(CredentialError.from(status: -34018), .accessDenied)
        XCTAssertEqual(CredentialError.from(status: -25291), .unavailable)
        XCTAssertEqual(CredentialError.from(status: -7), .osStatus(-7))
    }

    func testHelperConfigIsScopedAndNeverPersisted() throws {
        struct Fixtures: CredentialReading {
            func read(_ account: CredentialAccount) throws -> String {
                account == .asr ? "fixture-asr-only" : "fixture-llm-only"
            }
        }
        let settings = MacSettings.defaults
        let factory = HelperConfigFactory(credentials: Fixtures())
        try factory.withEphemeralConfig(settings: settings, operation: .transcribe) { config in
            XCTAssertEqual(config["asr_api_key"] as? String, "fixture-asr-only")
            XCTAssertEqual(config["llm_api_key"] as? String, "")
            XCTAssertEqual(config["max_record_seconds"] as? Int, 60)
        }
        try factory.withEphemeralConfig(settings: settings, operation: .selectedTask) { config in
            XCTAssertEqual(config["asr_api_key"] as? String, "")
            XCTAssertEqual(config["llm_api_key"] as? String, "fixture-llm-only")
        }
        let stored = String(decoding: try JSONEncoder().encode(settings), as: UTF8.self)
        XCTAssertFalse(stored.contains("api_key"))
        XCTAssertFalse(stored.contains("fixture-"))
    }

    func testMissingProviderKeysAllowLocalServersWithoutBearerTokens() throws {
        struct Missing: CredentialReading {
            func read(_ account: CredentialAccount) throws -> String { throw CredentialError.notFound }
        }
        let factory = HelperConfigFactory(credentials: Missing())
        for operation in [HelperOperation.transcribe, .selectedTask] {
            try factory.withEphemeralConfig(settings: .defaults, operation: operation) { config in
                XCTAssertEqual(config["asr_api_key"] as? String, "")
                XCTAssertEqual(config["llm_api_key"] as? String, "")
            }
        }
    }

    func testUnrelatedKeychainAccountCannotBlockAnOperation() throws {
        struct ForbidOther: CredentialReading {
            let forbidden: CredentialAccount
            func read(_ account: CredentialAccount) throws -> String {
                if account == forbidden { throw CredentialError.accessDenied }
                return account == .asr ? "fixture-asr" : "fixture-llm"
            }
        }
        try HelperConfigFactory(credentials: ForbidOther(forbidden: .llm))
            .withEphemeralConfig(settings: .defaults, operation: .transcribe) { config in
                XCTAssertEqual(config["asr_api_key"] as? String, "fixture-asr")
                XCTAssertEqual(config["llm_api_key"] as? String, "")
            }
        try HelperConfigFactory(credentials: ForbidOther(forbidden: .asr))
            .withEphemeralConfig(settings: .defaults, operation: .selectedTask) { config in
                XCTAssertEqual(config["asr_api_key"] as? String, "")
                XCTAssertEqual(config["llm_api_key"] as? String, "fixture-llm")
            }
    }

    func testRequiredKeychainErrorsStillFailClosed() {
        struct Failing: CredentialReading {
            let error: CredentialError
            func read(_ account: CredentialAccount) throws -> String { throw error }
        }
        for operation in [HelperOperation.transcribe, .selectedTask] {
            for failure in [CredentialError.accessDenied, .lockedOrInteractionRequired,
                            .unavailable, .invalidStoredData, .osStatus(-7)] {
                var requestWasConstructed = false
                XCTAssertThrowsError(try HelperConfigFactory(credentials: Failing(error: failure))
                    .withEphemeralConfig(settings: .defaults, operation: operation) { _ in
                        requestWasConstructed = true
                    }) { XCTAssertEqual($0 as? CredentialError, failure) }
                XCTAssertFalse(requestWasConstructed)
            }
        }
        XCTAssertTrue(CredentialError.accessDenied.localizedDescription.contains("Keychain"))
    }

    #if canImport(Security)
    func testSimulatedSecurityErrorsAndSeparateAccounts() throws {
        let backend = StubKeychainOperations()
        let store = KeychainCredentials(service: "com.xujinheng.VoiceRewrite.tests.stub", operations: backend)
        try store.add("fixture-asr", for: .asr)
        try store.add("fixture-llm", for: .llm)
        XCTAssertEqual(backend.addedAccounts, ["asr_api_key", "llm_api_key"])
        backend.copyStatus = -25300
        XCTAssertThrowsError(try store.read(.asr)) { XCTAssertEqual($0 as? CredentialError, .notFound) }
        backend.copyStatus = -25293
        XCTAssertThrowsError(try store.read(.llm)) { XCTAssertEqual($0 as? CredentialError, .accessDenied) }
        backend.copyStatus = -25308
        XCTAssertThrowsError(try store.read(.asr)) { XCTAssertEqual($0 as? CredentialError, .lockedOrInteractionRequired) }
        backend.copyStatus = 0
        backend.copyData = Data("fixture-success".utf8)
        XCTAssertEqual(try store.read(.asr), "fixture-success")
        backend.updateStatus = -25293
        XCTAssertThrowsError(try store.update("fixture-update", for: .asr)) { XCTAssertEqual($0 as? CredentialError, .accessDenied) }
        backend.deleteStatus = -25300
        XCTAssertThrowsError(try store.delete(.llm)) { XCTAssertEqual($0 as? CredentialError, .notFound) }
    }

    func testRealKeychainCRUDWhenExplicitlyEnabled() throws {
        guard ProcessInfo.processInfo.environment["VOICE_REWRITE_TEST_REAL_KEYCHAIN"] == "1" else {
            throw XCTSkip("Real macOS Keychain test requires disposable signed runner opt-in")
        }
        let service = "com.xujinheng.VoiceRewrite.tests.\(UUID().uuidString)"
        let keychain = KeychainCredentials(service: service)
        defer {
            for account in CredentialAccount.allCases { try? keychain.delete(account) }
        }
        try keychain.add("fixture-asr", for: .asr)
        try keychain.add("fixture-llm", for: .llm)
        XCTAssertEqual(try keychain.read(.asr), "fixture-asr")
        XCTAssertEqual(try keychain.read(.llm), "fixture-llm")
        try keychain.update("fixture-asr-updated", for: .asr)
        XCTAssertEqual(try keychain.read(.asr), "fixture-asr-updated")
        try keychain.delete(.asr)
        XCTAssertThrowsError(try keychain.read(.asr)) { XCTAssertEqual($0 as? CredentialError, .notFound) }
        try keychain.delete(.llm)
        XCTAssertThrowsError(try keychain.read(.llm)) { XCTAssertEqual($0 as? CredentialError, .notFound) }
    }
    #endif
}

#if canImport(Security)
import Security

private final class StubKeychainOperations: KeychainOperations {
    var addedAccounts: [String] = []
    var copyStatus: OSStatus = 0
    var copyData: Data? = nil
    var updateStatus: OSStatus = 0
    var deleteStatus: OSStatus = 0

    func add(_ attributes: [String: Any]) -> OSStatus {
        addedAccounts.append(attributes[kSecAttrAccount as String] as? String ?? "")
        return 0
    }
    func copy(_ query: [String: Any]) -> (OSStatus, Data?) { (copyStatus, copyData) }
    func update(_ query: [String: Any], with attributes: [String: Any]) -> OSStatus { updateStatus }
    func delete(_ query: [String: Any]) -> OSStatus { deleteStatus }
}
#endif
