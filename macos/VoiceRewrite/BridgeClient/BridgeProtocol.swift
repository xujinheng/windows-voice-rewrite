import Foundation
import VoiceRewriteStorage

/// JSONL v1 shared with voice-rewrite/cmd/voicebridge. The host does not implement ASR or LLM logic.
enum JSONValue: Codable, Equatable {
    case string(String)
    case number(Double)
    case boolean(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    init(from decoder: Decoder) throws {
        let value = try decoder.singleValueContainer()
        if value.decodeNil() { self = .null }
        else if let bool = try? value.decode(Bool.self) { self = .boolean(bool) }
        else if let string = try? value.decode(String.self) { self = .string(string) }
        else if let number = try? value.decode(Double.self) { self = .number(number) }
        else if let object = try? value.decode([String: JSONValue].self) { self = .object(object) }
        else if let array = try? value.decode([JSONValue].self) { self = .array(array) }
        else { throw DecodingError.dataCorruptedError(in: value, debugDescription: "Unsupported JSON value") }
    }

    func encode(to encoder: Encoder) throws {
        var value = encoder.singleValueContainer()
        switch self {
        case .string(let text): try value.encode(text)
        case .number(let number): try value.encode(number)
        case .boolean(let flag): try value.encode(flag)
        case .object(let object): try value.encode(object)
        case .array(let array): try value.encode(array)
        case .null: try value.encodeNil()
        }
    }

    var string: String? {
        if case .string(let value) = self { return value }
        return nil
    }

    var boolValue: Bool? {
        if case .boolean(let value) = self { return value }
        return nil
    }

    var int: Int? {
        if case .number(let value) = self, value.isFinite, value.rounded() == value,
           value >= Double(Int.min), value < Double(Int.max) { return Int(value) }
        return nil
    }
}

struct BridgeRequest {
    let id: String
    let operation: String
    var fields: [String: JSONValue] = [:]

    func line() throws -> Data {
        guard !id.isEmpty, id.utf8.count <= ContractLimits.maxRequestIDBytes, !operation.isEmpty,
              fields.keys.allSatisfy({ !["v", "id", "op"].contains($0) }) else {
            throw BridgeFailure.invalidRequest
        }
        var payload = fields
        payload["v"] = .number(1)
        payload["id"] = .string(id)
        payload["op"] = .string(operation)
        var data = try JSONEncoder().encode(payload)
        guard data.count <= JSONLFramer.maximumFrameBytes else { throw BridgeFailure.frameTooLarge }
        data.append(0x0a)
        return data
    }
}

struct BridgeErrorMessage: Decodable, Equatable {
    let code: String
    let message: String

    /// Catalog recovery text from core when present; otherwise the stable error code.
    var userText: String {
        let trimmed = message.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? code : trimmed
    }
}

struct BridgeReply: Decodable, Equatable {
    let v: Int
    let id: String
    let ok: Bool
    let result: [String: JSONValue]?
    let error: BridgeErrorMessage?

    func validate() throws {
        guard v == ContractLimits.protocolVersion, !id.isEmpty, id.utf8.count <= ContractLimits.maxRequestIDBytes,
              ok ? (result != nil && error == nil) : (error != nil && result == nil) else {
            throw BridgeFailure.invalidReply
        }
    }
}

enum BridgeFailure: Error, Equatable, LocalizedError {
    case helperMissing
    case alreadyRunning
    case notRunning
    case invalidRequest
    case duplicateID
    case frameTooLarge
    case invalidReply
    case truncatedReply
    case writeFailed
    case processExited(Int32)
    case cancelled
    case launchFailed(String)

    var errorDescription: String? {
        switch self {
        case .helperMissing: return "Bundle 中缺少可执行的 voicebridge"
        case .alreadyRunning: return "Helper 已经运行"
        case .notRunning: return "Helper 未运行"
        case .invalidRequest: return "无效的桥接请求"
        case .duplicateID: return "请求 ID 重复"
        case .frameTooLarge: return "JSONL 帧超过 \(ContractLimits.maxRequestBytes >> 20) MiB"
        case .invalidReply: return "Helper 返回无效的 JSONL 响应"
        case .truncatedReply: return "Helper 在 JSONL 帧完成前退出"
        case .writeFailed: return "向 Helper 写入失败"
        case .processExited(let status): return "Helper 已退出（状态码 \(status)）"
        case .cancelled: return "请求已取消"
        case .launchFailed(let detail): return "Helper 无法启动：\(detail)"
        }
    }
}

/// Incremental byte framing: handles split UTF-8, several replies per read and bounded memory.
struct JSONLFramer {
    static let maximumFrameBytes = ContractLimits.maxRequestBytes
    private var partial = Data()
    var hasPartialFrame: Bool { !partial.isEmpty }

    mutating func append(_ chunk: Data) throws -> [Data] {
        var lines: [Data] = []
        var cursor = chunk.startIndex
        while cursor < chunk.endIndex {
            let newline = chunk[cursor...].firstIndex(of: 0x0a)
            let end = newline ?? chunk.endIndex
            let segment = chunk[cursor..<end]
            guard partial.count + segment.count <= Self.maximumFrameBytes + 1 else {
                throw BridgeFailure.frameTooLarge
            }
            partial.append(contentsOf: segment)
            if let newline {
                if partial.last == 0x0d { partial.removeLast() }
                guard partial.count <= Self.maximumFrameBytes else { throw BridgeFailure.frameTooLarge }
                lines.append(partial)
                partial.removeAll(keepingCapacity: true)
                cursor = chunk.index(after: newline)
            } else {
                cursor = chunk.endIndex
            }
        }
        return lines
    }
}

/// Used only on BridgeClient's private serial state queue.
final class ReplyRegistry {
    typealias Completion = (Result<BridgeReply, BridgeFailure>) -> Void
    private var handlers: [String: Completion] = [:]
    // Do not recycle finished/canceled IDs: a delayed reply cannot match a newer request.
    private var usedIDs: Set<String> = []
    var count: Int { handlers.count }
    func contains(id: String) -> Bool { handlers[id] != nil }

    /// Retire a failed local encoding without killing unrelated requests.
    func fail(id: String, reason: BridgeFailure) {
        guard let callback = handlers.removeValue(forKey: id) else { return }
        DispatchQueue.main.async { callback(.failure(reason)) }
    }

    func register(id: String, completion: @escaping Completion) throws {
        guard usedIDs.insert(id).inserted else { throw BridgeFailure.duplicateID }
        handlers[id] = completion
    }

    @discardableResult
    func resolve(_ reply: BridgeReply) -> Bool {
        guard let callback = handlers.removeValue(forKey: reply.id) else { return false }
        DispatchQueue.main.async { callback(.success(reply)) }
        return true
    }

    @discardableResult
    func cancel(id: String) -> Bool {
        guard let callback = handlers.removeValue(forKey: id) else { return false }
        DispatchQueue.main.async { callback(.failure(.cancelled)) }
        return true
    }

    func failAll(_ reason: BridgeFailure) {
        let callbacks = Array(handlers.values)
        handlers.removeAll()
        for callback in callbacks {
            DispatchQueue.main.async { callback(.failure(reason)) }
        }
    }
}

/// The generation belongs to the view-model, not to the helper's transport IDs.
struct ResultGeneration {
    private(set) var latest: UInt64 = 0
    mutating func begin() -> UInt64 {
        latest &+= 1
        return latest
    }
    func accepts(_ value: UInt64) -> Bool { value == latest }
}

 
/// Typed JSONL v1 task effects from Go Core. Swift only validates identity and
/// executes the OS boundary; it must not recalculate portable task policy.
struct VoiceFlowEffectV1: Decodable, Equatable {
    enum Kind: String, Decodable {
        case startNativeCapture = "start_native_capture"
        case stopNativeCapture = "stop_native_capture"
        case sendASR = "send_asr"
        case sendModel = "send_model"
        case attemptNativeWrite = "attempt_native_write"
        case showPreview = "show_preview"
        case setPhase = "set_phase"
        case appendHistory = "append_history"
        case clearTask = "clear_task"
    }

    let type: Kind
    let session_id: String
    let generation: UInt64
    let command_id: String
    let request_id: String
    let phase: String?
    let text: String?
    let timeout_origin: String?
}

struct VoiceFlowTransitionV1: Decodable, Equatable {
    let accepted: Bool
    let duplicate: Bool
    let reason: String?
    let session_id: String
    let generation: UInt64
    let phase: String
    let effects: [VoiceFlowEffectV1]

    static func decode(_ reply: BridgeReply) throws -> VoiceFlowTransitionV1 {
        try reply.validate()
        guard reply.ok, let result = reply.result,
              let data = try? JSONEncoder().encode(result),
              let value = try? JSONDecoder().decode(Self.self, from: data),
              !value.session_id.isEmpty, value.generation > 0,
              value.effects.allSatisfy({
                  $0.session_id == value.session_id &&
                  $0.generation > 0 && !$0.command_id.isEmpty &&
                  !$0.request_id.isEmpty &&
                  // A newer arm may cancel the prior epoch; never accept a
                  // future-generation effect under an earlier transition.
                  $0.generation <= value.generation
              }) else {
            throw BridgeFailure.invalidReply
        }
        return value
    }
}
