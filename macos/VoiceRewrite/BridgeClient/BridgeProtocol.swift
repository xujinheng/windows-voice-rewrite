import Foundation

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
        guard !id.isEmpty, id.utf8.count <= 128, !operation.isEmpty,
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
}

struct BridgeReply: Decodable, Equatable {
    let v: Int
    let id: String
    let ok: Bool
    let result: [String: JSONValue]?
    let error: BridgeErrorMessage?

    func validate() throws {
        guard v == 1, !id.isEmpty, id.utf8.count <= 128,
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
        case .frameTooLarge: return "JSONL 帧超过 8 MiB"
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
    static let maximumFrameBytes = 8 * 1024 * 1024
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
