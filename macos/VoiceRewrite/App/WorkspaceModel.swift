import Foundation
import Combine

@MainActor
final class WorkspaceModel: ObservableObject {
    enum Connection: Equatable {
        case disconnected
        case connecting
        case connected(String)
        case failed(String)
    }

    // Transitional, in-memory values. D's settings/Keychain package will own persistence.
    @Published var asrURL = "http://127.0.0.1:8000/v1/audio/transcriptions"
    @Published var llmURL = "http://127.0.0.1:11434/v1/chat/completions"
    @Published var modelName = ""
    @Published private(set) var connection: Connection = .disconnected
    @Published private(set) var recordingStatus = "未接入录音模块"
    @Published private(set) var serviceStatus = "尚未检测"
    @Published private(set) var transcription: String?
    @Published private(set) var processedText: String?

    private var bridge: BridgeClient?
    private var helloGeneration = ResultGeneration()
    private var resultGeneration = ResultGeneration()

    func connect() {
        let token = helloGeneration.begin()
        bridge?.stop()
        bridge = nil
        connection = .connecting
        serviceStatus = "正在启动 Bundle 内的 voicebridge…"
        guard let executable = Bundle.main.executableURL?.deletingLastPathComponent()
            .appendingPathComponent("voicebridge") else {
            connection = .failed("无法定位 App Bundle")
            serviceStatus = "不可用"
            return
        }
        let client = BridgeClient(executableURL: executable) { [weak self] failure in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                self.connection = .failed(failure.localizedDescription)
                self.serviceStatus = "Helper 已断开"
            }
        }
        bridge = client
        do { try client.start() }
        catch {
            connection = .failed(error.localizedDescription)
            serviceStatus = "Helper 启动失败"
            return
        }
        client.send(operation: "hello") { [weak self] response in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                switch response {
                case .success(let reply):
                    guard reply.ok, reply.result?["name"]?.string == "VoiceRewrite",
                          reply.result?["protocol"]?.int == 1 else {
                        self.connection = .failed(reply.error?.message ?? "无效的 hello 回执")
                        self.serviceStatus = "握手失败"
                        return
                    }
                    self.connection = .connected("VoiceRewrite / JSONL v1")
                    self.serviceStatus = "Go Helper 就绪（仅握手）"
                case .failure(let failure):
                    self.connection = .failed(failure.localizedDescription)
                    self.serviceStatus = "握手失败"
                }
            }
        }
    }

    /// Future feature integrations must call this when an input changes, and verify
    /// the returned generation before publishing actual helper text to the UI.
    @discardableResult
    func beginResultRequest() -> UInt64 { resultGeneration.begin() }

    func acceptResult(generation: UInt64, transcription newTranscription: String?,
                      processed newProcessed: String?) {
        guard resultGeneration.accepts(generation) else { return }
        transcription = newTranscription
        processedText = newProcessed
    }

    func shutdown() {
        _ = helloGeneration.begin()
        _ = resultGeneration.begin()
        bridge?.stop()
        bridge = nil
        connection = .disconnected
        serviceStatus = "已停止"
    }
}
