// Source: voice-rewrite/internal/core/config.CanonicalEndpoints
import XCTest
@testable import VoiceRewriteStorage

final class CanonicalEndpointVectors: XCTestCase {
    func testGoCanonicalEndpoints() {
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: " http://127.0.0.1:8081/v1/ ", asrModel: " qwen3-asr ", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "")?.asrBaseURL, "http://127.0.0.1:8081/v1")
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: " http://127.0.0.1:8081/v1/ ", asrModel: " qwen3-asr ", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "")?.asrModel, "qwen3-asr")
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: " http://127.0.0.1:8081/v1/ ", asrModel: " qwen3-asr ", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "")?.llmBaseURL, "http://127.0.0.1:1234/v1")
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: " http://127.0.0.1:8081/v1/ ", asrModel: " qwen3-asr ", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "")?.textModel, "auto")
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "http://localhost/v1?key=1", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "http://user:pass@127.0.0.1/v1", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "http://127.0.0.1/v1#frag", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "file:///tmp/x", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "http:example.com", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "http://localhost/v1%", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "http://localhost/invalid%ZZ", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertNil(ContractLimits.canonicalEndpoints(asrBaseURL: "http://ex%41mple.com/v1", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto"))
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: "http://localhost/v1%20ok", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto")?.asrBaseURL, "http://localhost/v1%20ok")
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: "http://localhost/v1%20ok", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto")?.asrModel, "m")
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: "http://localhost/v1%20ok", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto")?.llmBaseURL, "http://127.0.0.1:1234/v1")
        XCTAssertEqual(ContractLimits.canonicalEndpoints(asrBaseURL: "http://localhost/v1%20ok", asrModel: "m", llmBaseURL: "http://127.0.0.1:1234/v1", textModel: "auto")?.textModel, "auto")
        XCTAssertEqual(ContractLimits.storedCredentialTooLong(String(repeating: " ", count: 4000)), false)
        XCTAssertEqual(ContractLimits.storedCredentialTooLong(String(repeating: " ", count: 4001)), true)
    }
}
