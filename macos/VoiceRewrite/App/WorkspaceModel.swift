import AppKit
import Combine
import Foundation
import Microphone
import VoiceRewriteInteraction
import VoiceRewriteStorage

@MainActor
final class WorkspaceModel: ObservableObject {
    enum Connection: Equatable {
        case disconnected
        case connecting
        case connected(String)
        case failed(String)
    }

    @Published var settings: MacSettings = .defaults
    @Published var asrKeyInput = ""
    @Published var llmKeyInput = ""
    @Published var taskInstruction = ""
    @Published private(set) var connection: Connection = .disconnected
    @Published private(set) var recordingStatus = "未录音"
    @Published private(set) var serviceStatus = "尚未连接"
    @Published private(set) var interactionStatus = "全局快捷键尚未注册"
    @Published private(set) var settingsStatus = ""
    @Published private(set) var transcription: String?
    @Published private(set) var processedText: String?
    @Published private(set) var isRecording = false
    @Published private(set) var isStarting = false
    @Published private(set) var isProcessing = false
    @Published private(set) var accessibilityAuthorized = false

    private let microphone = MicrophoneRecorder()
    private let credentials = KeychainCredentials()
    private let interaction = InteractionSession.macOS()
    private var store: SettingsStore?
    private var bridge: BridgeClient?
    private var helloGeneration = ResultGeneration()
    private var resultGeneration = ResultGeneration()
    private var activeRequestID: String?
    private var activeInsert: (snapshot: SelectionSnapshot, generation: UInt64)?
    private var hotKeyDriver: CarbonHotKeyDriver?
    private var hotKeys: HotKeyRegistry?

    private let recordShortcut = HotKeyShortcut(keyCode: 15, modifiers: [.control, .option])
    private let editShortcut = HotKeyShortcut(keyCode: 17, modifiers: [.control, .option])

    init() {
        do {
            let s = try SettingsStore()
            settings = try s.load()
            store = s
            settingsStatus = "已加载本地配置（密钥仅保存在 Keychain）"
        } catch {
            settingsStatus = "读取配置失败；请在设置中保存新的有效配置"
        }
    }

    func launch() {
        if case .disconnected = connection { connect() }
        if hotKeys == nil { registerHotKeys() }
        accessibilityAuthorized = InteractionSession.accessibilityPermissionGranted()
    }

    func connect() {
        let token = helloGeneration.begin()
        bridge?.stop()
        bridge = nil
        connection = .connecting
        serviceStatus = "正在启动应用内 Go Helper"
        guard let executable = Bundle.main.executableURL?.deletingLastPathComponent()
            .appendingPathComponent("voicebridge") else {
            connection = .failed("无法定位 App Bundle")
            return
        }
        let client = BridgeClient(executableURL: executable) { [weak self] failure in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                self.connection = .failed(failure.localizedDescription)
                self.serviceStatus = "Go Helper 已断开"
                self.invalidatePending()
            }
        }
        bridge = client
        do { try client.start() }
        catch {
            connection = .failed("Go Helper 启动失败")
            serviceStatus = "不可用"
            return
        }
        client.send(operation: "hello") { [weak self] response in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                switch response {
                case .success(let reply):
                    guard reply.ok, reply.result?["name"]?.string == "VoiceRewrite",
                          reply.result?["protocol"]?.int == 1 else {
                        self.connection = .failed("无效的 Helper 协议响应")
                        self.serviceStatus = "握手失败"
                        return
                    }
                    self.connection = .connected("VoiceRewrite / JSONL v1")
                    self.serviceStatus = "ASR / 文本服务就绪"
                case .failure:
                    self.connection = .failed("Go Helper 握手失败")
                    self.serviceStatus = "不可用"
                }
            }
        }
    }

    func saveSettings() {
        defer {
            asrKeyInput = ""
            llmKeyInput = ""
        }
        do {
            guard let store else { throw SettingsError.insecureStorage }
            try store.save(settings)
            try saveCredential(asrKeyInput, account: .asr)
            try saveCredential(llmKeyInput, account: .llm)
            settingsStatus = "设置已保存，密钥不写入配置文件"
        } catch {
            settingsStatus = "无法安全保存设置：\(error.localizedDescription)"
        }
    }

    private func saveCredential(_ value: String, account: CredentialAccount) throws {
        guard !value.isEmpty else { return }
        do {
            try credentials.add(value, for: account)
        } catch CredentialError.duplicate {
            try credentials.update(value, for: account)
        }
    }

    func requestAccessibility() {
        accessibilityAuthorized = InteractionSession.accessibilityPermissionGranted(prompt: true)
        interactionStatus = accessibilityAuthorized
            ? "辅助功能已授权"
            : "请在系统设置的辅助功能权限中授权 VoiceRewrite"
    }

    private func registerHotKeys() {
        let driver = CarbonHotKeyDriver { [weak self] shortcut, pid in
            guard let self else { return }
            if shortcut == self.recordShortcut {
                self.toggleRecording(from: pid)
            } else if shortcut == self.editShortcut {
                self.processSelectedText(from: pid)
            }
        }
        let registry = HotKeyRegistry(driver: driver)
        do {
            try registry.register(recordShortcut)
            try registry.register(editShortcut)
            hotKeyDriver = driver
            hotKeys = registry
            interactionStatus = "Ctrl+Option+R 录音；Ctrl+Option+T 处理选区"
        } catch {
            registry.shutdown()
            driver.shutdown()
            interactionStatus = "无法注册全局快捷键，请检查系统快捷键冲突"
        }
    }

    private func requestConfig() throws -> JSONValue {
        let factory = HelperConfigFactory(credentials: credentials)
        return try factory.withEphemeralConfig(settings: settings) { config in
            var data: [String: JSONValue] = [:]
            for (key, value) in config {
                if let string = value as? String {
                    data[key] = .string(string)
                } else if let number = value as? Int {
                    data[key] = .number(Double(number))
                } else {
                    throw BridgeFailure.invalidRequest
                }
            }
            return .object(data)
        }
    }

    private func invalidatePending() {
        _ = resultGeneration.begin()
        if let id = activeRequestID { bridge?.cancel(id: id) }
        activeRequestID = nil
        if let old = activeInsert {
            Task { await interaction.cancelTask(generation: old.generation) }
        }
        activeInsert = nil
        isProcessing = false
    }

    func toggleRecording(from targetPID: Int32? = nil) {
        if isRecording { stopRecording() }
        else if !isStarting && !isProcessing { startRecording(targetPID: targetPID) }
    }

    private func startRecording(targetPID: Int32?) {
        invalidatePending()
        let token = resultGeneration.begin()
        isStarting = true
        recordingStatus = "正在申请麦克风权限并启动录音"
        Task { @MainActor [weak self] in
            guard let self else { return }
            if let pid = targetPID, pid > 0 {
                do {
                    let generation = try await interaction.beginTask(targetProcessID: pid)
                    let snapshot = try await interaction.captureSelection(
                        generation: generation, intent: .insertAtCaret)
                    if resultGeneration.accepts(token) {
                        activeInsert = (snapshot, generation)
                    }
                } catch {
                    interactionStatus = "未获得可验证的插入位置；仅显示识别结果"
                }
            }
            guard resultGeneration.accepts(token) else { return }
            do {
                try await microphone.start()
                guard resultGeneration.accepts(token) else { microphone.cancel(); return }
                isRecording = true
                recordingStatus = "录音中；再次按 Ctrl+Option+R 停止"
            } catch {
                recordingStatus = "无法启动麦克风：\(error.localizedDescription)"
            }
            isStarting = false
        }
    }

    func stopRecording() {
        guard isRecording else { return }
        isRecording = false
        isProcessing = true
        recordingStatus = "正在处理语音"
        let token = resultGeneration.latest
        let insertion = activeInsert
        activeInsert = nil
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let pcm = try await microphone.stop()
                guard resultGeneration.accepts(token) else { return }
                let config = try requestConfig()
                guard let bridge else { throw BridgeFailure.notRunning }
                let id = bridge.send(operation: "transcribe", fields: [
                    "config": config,
                    "pcm_base64": .string(pcm.base64EncodedString()),
                ]) { [weak self] reply in
                    Task { @MainActor [weak self] in
                        guard let self, self.resultGeneration.accepts(token) else { return }
                        self.activeRequestID = nil
                        self.isProcessing = false
                        switch reply {
                        case .success(let response):
                            guard response.ok, let text = response.result?["text"]?.string else {
                                self.recordingStatus = "ASR 请求失败：\(response.error?.code ?? "invalid_reply")"
                                return
                            }
                            self.transcription = text
                            self.recordingStatus = "语音识别已完成"
                            if let insertion {
                                Task { @MainActor [weak self] in
                                    guard let self, self.resultGeneration.accepts(token) else { return }
                                    do {
                                        try await self.interaction.insertIfCurrent(
                                            insertion.snapshot, text: text, generation: insertion.generation)
                                        self.interactionStatus = "已安全插入原目标"
                                    } catch {
                                        self.interactionStatus = "目标/焦点已变化，未写入；识别结果保留在窗口"
                                    }
                                }
                            }
                        case .failure(let failure):
                            self.recordingStatus = "识别失败：\(failure.localizedDescription)"
                        }
                    }
                }
                activeRequestID = id
            } catch {
                guard resultGeneration.accepts(token) else { return }
                isProcessing = false
                recordingStatus = "无法识别：\(error.localizedDescription)"
            }
        }
    }

    func processSelectedText(from targetPID: Int32?) {
        guard let pid = targetPID, pid > 0 else {
            interactionStatus = "无有效前台目标"
            return
        }
        guard !taskInstruction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            interactionStatus = "请先在应用中填写选区处理指令"
            return
        }
        invalidatePending()
        let token = resultGeneration.begin()
        isProcessing = true
        interactionStatus = "正在校验选区"
        let instruction = taskInstruction
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let generation = try await interaction.beginTask(targetProcessID: pid)
                let snapshot = try await interaction.captureSelection(
                    generation: generation, intent: .replaceSelection)
                guard resultGeneration.accepts(token) else {
                    await interaction.cancelTask(generation: generation)
                    return
                }
                let config = try requestConfig()
                guard let bridge else { throw BridgeFailure.notRunning }
                let id = bridge.send(operation: "selected_task", fields: [
                    "config": config,
                    "selected_text": .string(snapshot.originalSelectedText),
                    "instruction": .string(instruction),
                ]) { [weak self] reply in
                    Task { @MainActor [weak self] in
                        guard let self, self.resultGeneration.accepts(token) else { return }
                        self.isProcessing = false
                        self.activeRequestID = nil
                        switch reply {
                        case .success(let response):
                            guard response.ok, let text = response.result?["text"]?.string else {
                                self.interactionStatus = "模型处理失败：\(response.error?.code ?? "invalid_reply")"
                                return
                            }
                            self.processedText = text
                            let mayWrite = response.result?["may_attempt_replace"]?.boolValue == true
                            let previewOnly = response.result?["preview_only"]?.boolValue != false
                            guard mayWrite && !previewOnly else {
                                self.interactionStatus = "仅预览结果；未获模型路由修改授权"
                                Task { await self.interaction.cancelTask(generation: generation) }
                                return
                            }
                            Task { @MainActor [weak self] in
                                guard let self, self.resultGeneration.accepts(token) else { return }
                                do {
                                    try await self.interaction.replaceIfCurrent(snapshot, with: text,
                                                                               generation: generation)
                                    self.interactionStatus = "原选区验证通过并已替换"
                                } catch {
                                    self.interactionStatus = "目标/选区已变化或不支持写入；未替换"
                                }
                            }
                        case .failure:
                            self.interactionStatus = "文本任务失败或已取消"
                            Task { await self.interaction.cancelTask(generation: generation) }
                        }
                    }
                }
                activeRequestID = id
            } catch {
                guard resultGeneration.accepts(token) else { return }
                isProcessing = false
                interactionStatus = "选区任务未执行：\(error.localizedDescription)"
            }
        }
    }

    func cancelCurrent() {
        invalidatePending()
        microphone.cancel()
        isRecording = false
        isStarting = false
        recordingStatus = "已取消"
        interactionStatus = "已取消当前操作"
    }

    func shutdown() {
        _ = helloGeneration.begin()
        cancelCurrent()
        hotKeys?.shutdown()
        hotKeyDriver?.shutdown()
        hotKeys = nil
        hotKeyDriver = nil
        bridge?.stop()
        bridge = nil
        connection = .disconnected
        Task { await interaction.shutdown() }
    }
}
