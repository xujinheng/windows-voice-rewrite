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
    @Published private(set) var asrProbeStatus = "尚未检测"
    @Published private(set) var llmProbeStatus = "尚未检测"
    @Published private(set) var isCheckingServices = false
    @Published private(set) var interactionStatus = "全局快捷键尚未注册"
    @Published private(set) var sideMouseStatus = "尚未检查鼠标侧键监听"
    @Published private(set) var settingsStatus = ""
    @Published private(set) var transcription: String?
    @Published private(set) var processedText: String?
    @Published private(set) var isRecording = false
    @Published private(set) var isVoiceSelectionRecording = false
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
    private var probeGeneration = ResultGeneration()
    private var activeProbeIDs: [String: String] = [:]
    private var activeRequestID: String?
    private var activeInsert: (snapshot: SelectionSnapshot, generation: UInt64)?
    private var activeVoiceSelection: (snapshot: SelectionSnapshot, generation: UInt64)?
    private var hotKeyDriver: CarbonHotKeyDriver?
    private var hotKeys: HotKeyRegistry?
    private var sideMouse: SideMouseButtonDriver?
    private var activeSideAction: SideMouseAction?
    private var pendingSideRelease: SideMouseAction?

    private let recordShortcut = HotKeyShortcut(keyCode: 15, modifiers: [.control, .option])
    private let editShortcut = HotKeyShortcut(keyCode: 17, modifiers: [.control, .option])
    private let spokenEditShortcut = HotKeyShortcut(keyCode: 14, modifiers: [.control, .option])

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
        refreshSideMouse()
    }

    func connect() {
        let token = helloGeneration.begin()
        cancelProbes()
        asrProbeStatus = "尚未检测"
        llmProbeStatus = "尚未检测"
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
                self.cancelProbes()
                self.asrProbeStatus = "未连接"
                self.llmProbeStatus = "未连接"
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
                    self.serviceStatus = "Go Helper 已连接"
                    self.checkServices()
                case .failure:
                    self.connection = .failed("Go Helper 握手失败")
                    self.serviceStatus = "不可用"
                }
            }
        }
    }

    func saveSettings() {
        cancelProbes()
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
            checkServices()
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
        refreshSideMouse()
    }

    private func registerHotKeys() {
        let driver = CarbonHotKeyDriver { [weak self] shortcut, pid in
            guard let self else { return }
            if shortcut == self.recordShortcut {
                self.toggleRecording(from: pid)
            } else if shortcut == self.editShortcut {
                self.processSelectedText(from: pid)
            } else if shortcut == self.spokenEditShortcut {
                self.toggleVoiceSelectedText(from: pid)
            }
        }
        let registry = HotKeyRegistry(driver: driver)
        do {
            try registry.register(recordShortcut)
            try registry.register(editShortcut)
            try registry.register(spokenEditShortcut)
            hotKeyDriver = driver
            hotKeys = registry
            interactionStatus = "Ctrl+Option+R 听写；Ctrl+Option+T 文本指令；Ctrl+Option+E 语音修改选区"
        } catch {
            registry.shutdown()
            driver.shutdown()
            interactionStatus = "无法注册全局快捷键，请检查系统快捷键冲突"
        }
    }

    func refreshSideMouse() {
        accessibilityAuthorized = InteractionSession.accessibilityPermissionGranted()
        guard accessibilityAuthorized else {
            sideMouse?.shutdown()
            sideMouse = nil
            sideMouseStatus = "未授权辅助功能；请在系统设置授权后返回应用"
            return
        }
        let isFreshStart = sideMouse == nil
        let driver = sideMouse ?? SideMouseButtonDriver(
            onHold: { [weak self] action, pid in
                self?.sideMouseHeld(action, targetPID: pid)
            },
            onRelease: { [weak self] action in
                self?.sideMouseReleased(action)
            },
            onCancel: { [weak self] in
                self?.cancelCurrent()
            },
            onObservedButton: { [weak self] button in
                if let action = SideMouseAction(rawValue: button) {
                    self?.sideMouseStatus = action == .dictation
                        ? "已收到 Mouse4 按下；继续按住以开始听写"
                        : "已收到 Mouse5 按下；继续按住以开始改写"
                } else {
                    self?.sideMouseStatus = "检测到辅助鼠标按钮 \(button)；当前绑定按钮 3/4"
                }
            }
        )
        do {
            try driver.start()
            sideMouse = driver
            if isFreshStart { sideMouseStatus = "监听已启动；等待 Mouse4/Mouse5 事件" }
        } catch {
            driver.shutdown()
            sideMouse = nil
            sideMouseStatus = "监听失败：\(error.localizedDescription)；键盘快捷键仍可用"
        }
    }

    private func sideMouseHeld(_ action: SideMouseAction, targetPID: Int32?) {
        guard !isRecording && !isStarting && !isProcessing && activeSideAction == nil else { return }
        sideMouseStatus = action == .dictation ? "Mouse4 长按已触发" : "Mouse5 长按已触发"
        switch action {
        case .dictation: toggleRecording(from: targetPID)
        case .rewrite: toggleVoiceSelectedText(from: targetPID)
        }
        // Starting a new task invalidates the previous task's input ownership.
        activeSideAction = action
        pendingSideRelease = nil
    }

    private func sideMouseReleased(_ action: SideMouseAction) {
        guard activeSideAction == action else { return }
        if isStarting {
            pendingSideRelease = action
        } else if isRecording {
            stopRecording()
        } else {
            activeSideAction = nil
        }
    }

    private func stopWhenSideMouseReleased(_ action: SideMouseAction) {
        guard pendingSideRelease == action else { return }
        pendingSideRelease = nil
        if isRecording { stopRecording() }
        else { activeSideAction = nil }
    }

    private func requestConfig(for operation: HelperOperation) throws -> JSONValue {
        let factory = HelperConfigFactory(credentials: credentials)
        return try factory.withEphemeralConfig(settings: settings, operation: operation) { config in
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

    private func cancelProbes() {
        _ = probeGeneration.begin()
        for id in activeProbeIDs.values { bridge?.cancel(id: id) }
        activeProbeIDs.removeAll()
        isCheckingServices = false
    }

    // Windows and macOS use the same Go Core probe semantics via voicebridge.
    // No user microphone input or selected text is sent by these operations.
    func checkServices() {
        runProbes([.transcribe, .selectedTask])
    }

    func checkASRService() {
        runProbes([.transcribe])
    }

    func checkLLMService() {
        runProbes([.selectedTask])
    }

    private func runProbes(_ operations: [HelperOperation]) {
        cancelProbes()
        guard case .connected = connection, let bridge else {
            for operation in operations {
                setProbeStatus("Go Helper 未连接", for: operation)
            }
            return
        }
        let token = probeGeneration.latest
        for operation in operations {
            let name: String
            let bridgeOperation: String
            switch operation {
            case .transcribe:
                name = "ASR"
                bridgeOperation = "probe_asr"
            case .selectedTask:
                name = "LLM"
                bridgeOperation = "probe_llm"
            case .spokenSelectedTask:
                continue
            }
            setProbeStatus("\(name)：正在检测服务…", for: operation)
            do {
                let config = try requestConfig(for: operation)
                let id = bridge.send(operation: bridgeOperation, fields: ["config": config]) {
                    [weak self] response in
                    Task { @MainActor [weak self] in
                        guard let self, self.probeGeneration.accepts(token) else { return }
                        self.activeProbeIDs.removeValue(forKey: bridgeOperation)
                        self.isCheckingServices = !self.activeProbeIDs.isEmpty
                        switch response {
                        case .success(let reply):
                            if reply.ok, reply.result?["kind"]?.string == name,
                               let message = reply.result?["message"]?.string {
                                self.setProbeStatus(message, for: operation)
                            } else {
                                self.setProbeStatus("\(name)：检测失败（\(reply.error?.code ?? "invalid_reply")）",
                                                    for: operation)
                            }
                        case .failure(let error):
                            self.setProbeStatus("\(name)：检测失败（\(error.localizedDescription)）",
                                                for: operation)
                        }
                    }
                }
                activeProbeIDs[bridgeOperation] = id
                isCheckingServices = true
            } catch {
                setProbeStatus("\(name)：无法读取服务配置（\(error.localizedDescription)）",
                               for: operation)
            }
        }
    }

    private func setProbeStatus(_ message: String, for operation: HelperOperation) {
        switch operation {
        case .transcribe: asrProbeStatus = message
        case .selectedTask: llmProbeStatus = message
        case .spokenSelectedTask:
            asrProbeStatus = message
            llmProbeStatus = message
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
        if let old = activeVoiceSelection {
            Task { await interaction.cancelTask(generation: old.generation) }
        }
        activeVoiceSelection = nil
        isVoiceSelectionRecording = false
        activeSideAction = nil
        pendingSideRelease = nil
        isProcessing = false
    }

    func toggleRecording(from targetPID: Int32? = nil) {
        if isRecording { stopRecording() }
        else if !isStarting && !isProcessing { startRecording(targetPID: targetPID) }
    }

    func toggleVoiceSelectedText(from targetPID: Int32?) {
        if isRecording {
            if isVoiceSelectionRecording { stopRecording() }
            return
        }
        guard !isStarting && !isProcessing else { return }
        guard let pid = targetPID, pid > 0 else {
            interactionStatus = "无有效前台目标，无法获取选区"
            return
        }
        invalidatePending()
        let token = resultGeneration.begin()
        isStarting = true
        interactionStatus = "正在安全获取原应用选区"
        Task { @MainActor [weak self] in
            guard let self else { return }
            var generation: UInt64?
            do {
                let next = try await interaction.beginTask(targetProcessID: pid)
                generation = next
                let snapshot = try await interaction.captureSelection(
                    generation: next, intent: .replaceSelection)
                guard resultGeneration.accepts(token) else {
                    await interaction.cancelTask(generation: next)
                    isStarting = false
                    return
                }
                try await microphone.start()
                guard resultGeneration.accepts(token) else {
                    microphone.cancel()
                    await interaction.cancelTask(generation: next)
                    isStarting = false
                    return
                }
                activeVoiceSelection = (snapshot, next)
                isVoiceSelectionRecording = true
                isRecording = true
                recordingStatus = "语音修改选区录音中；松开 Mouse5 或再次按 Ctrl+Option+E 结束"
                interactionStatus = "已锁定选区；录音后校验焦点与选区是否仍有效"
            } catch {
                microphone.cancel()
                if let generation { await interaction.cancelTask(generation: generation) }
                if resultGeneration.accepts(token) {
                    interactionStatus = "无法开始语音选区处理：\(error.localizedDescription)"
                    recordingStatus = "未录音"
                }
            }
            isStarting = false
            stopWhenSideMouseReleased(.rewrite)
        }
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
                        interactionStatus = "已锁定原应用光标；识别后将验证并写入"
                    }
                } catch {
                    interactionStatus = Self.captureErrorMessage(error)
                }
            } else {
                interactionStatus = "未指定外部文本框；本次录音仅显示识别结果。请在目标软件中按 Mouse4 开始听写"
            }
            guard resultGeneration.accepts(token) else { return }
            do {
                try await microphone.start()
                guard resultGeneration.accepts(token) else { microphone.cancel(); return }
                isRecording = true
                recordingStatus = "录音中；松开 Mouse4 或再次按 Ctrl+Option+R 停止"
            } catch {
                recordingStatus = "无法启动麦克风：\(error.localizedDescription)"
            }
            isStarting = false
            stopWhenSideMouseReleased(.dictation)
        }
    }

    func stopRecording() {
        guard isRecording else { return }
        activeSideAction = nil
        pendingSideRelease = nil
        isRecording = false
        isProcessing = true
        recordingStatus = "正在处理语音"
        let token = resultGeneration.latest
        let insertion = activeInsert
        let voiceSelection = activeVoiceSelection
        activeInsert = nil
        activeVoiceSelection = nil
        isVoiceSelectionRecording = false
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let pcm = try await microphone.stop()
                guard resultGeneration.accepts(token) else { return }
                if let voiceSelection {
                    cancelProbes()
                    asrProbeStatus = "ASR：正在识别语音修改指令…"
                    llmProbeStatus = "LLM：等待语音指令识别"
                    let config = try requestConfig(for: .spokenSelectedTask)
                    guard let bridge else { throw BridgeFailure.notRunning }
                    let id = bridge.send(operation: "selected_voice_task", fields: [
                        "config": config,
                        "pcm_base64": .string(pcm.base64EncodedString()),
                        "selected_text": .string(voiceSelection.snapshot.originalSelectedText),
                    ]) { [weak self] reply in
                        Task { @MainActor [weak self] in
                            guard let self else { return }
                            self.handleSelectedTaskReply(reply,
                                snapshot: voiceSelection.snapshot,
                                generation: voiceSelection.generation,
                                token: token, spoken: true)
                        }
                    }
                    activeRequestID = id
                    return
                }
                cancelProbes()
                asrProbeStatus = "ASR：正在转写真实录音…"
                let config = try requestConfig(for: .transcribe)
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
                                self.asrProbeStatus = "ASR：实际转写失败"
                                return
                            }
                            self.transcription = text
                            self.recordingStatus = "语音识别已完成"
                            self.asrProbeStatus = "ASR：真实录音识别成功"
                            if let insertion {
                                Task { @MainActor [weak self] in
                                    guard let self, self.resultGeneration.accepts(token) else { return }
                                    do {
                                        try await self.interaction.insertIfCurrent(
                                            insertion.snapshot, text: text, generation: insertion.generation)
                                        self.interactionStatus = "已安全插入原目标"
                                    } catch {
                                        self.interactionStatus = Self.insertionErrorMessage(error)
                                    }
                                }
                            }
                        case .failure(let failure):
                            self.recordingStatus = "识别失败：\(failure.localizedDescription)"
                            self.asrProbeStatus = "ASR：实际转写失败"
                        }
                    }
                }
                activeRequestID = id
            } catch {
                guard resultGeneration.accepts(token) else { return }
                isProcessing = false
                recordingStatus = "无法识别：\(error.localizedDescription)"
                asrProbeStatus = "ASR：实际转写未完成"
                if let voiceSelection {
                    await interaction.cancelTask(generation: voiceSelection.generation)
                    interactionStatus = "语音选区任务未执行"
                }
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
                cancelProbes()
                llmProbeStatus = "LLM：正在处理选区…"
                let config = try requestConfig(for: .selectedTask)
                guard let bridge else { throw BridgeFailure.notRunning }
                let id = bridge.send(operation: "selected_task", fields: [
                    "config": config,
                    "selected_text": .string(snapshot.originalSelectedText),
                    "instruction": .string(instruction),
                ]) { [weak self] reply in
                    Task { @MainActor [weak self] in
                        guard let self else { return }
                        self.handleSelectedTaskReply(reply, snapshot: snapshot,
                            generation: generation, token: token, spoken: false)
                    }
                }
                activeRequestID = id
            } catch {
                guard resultGeneration.accepts(token) else { return }
                isProcessing = false
                interactionStatus = "选区任务未执行：\(error.localizedDescription)"
                llmProbeStatus = "LLM：实际任务未完成"
            }
        }
    }

    private func handleSelectedTaskReply(
        _ reply: Result<BridgeReply, BridgeFailure>,
        snapshot: SelectionSnapshot, generation: UInt64,
        token: UInt64, spoken: Bool
    ) {
        guard resultGeneration.accepts(token) else {
            Task { await interaction.cancelTask(generation: generation) }
            return
        }
        activeRequestID = nil
        isProcessing = false
        switch reply {
        case .success(let response):
            guard response.ok, let text = response.result?["text"]?.string else {
                interactionStatus = "模型处理失败：\(response.error?.code ?? "invalid_reply")"
                if spoken {
                    recordingStatus = "语音修改未完成"
                    asrProbeStatus = "ASR / LLM：语音选区任务失败"
                }
                llmProbeStatus = "LLM：实际任务失败"
                Task { await interaction.cancelTask(generation: generation) }
                return
            }
            if spoken {
                guard let instruction = response.result?["instruction"]?.string,
                      !instruction.isEmpty else {
                    interactionStatus = "语音指令响应缺失；未写入原选区"
                    Task { await interaction.cancelTask(generation: generation) }
                    return
                }
                transcription = instruction
                asrProbeStatus = "ASR：语音指令识别成功"
                recordingStatus = "语音修改已处理"
            }
            processedText = text
            llmProbeStatus = "LLM：实际文本任务成功"
            let mayWrite = response.result?["may_attempt_replace"]?.boolValue == true
            let previewOnly = response.result?["preview_only"]?.boolValue != false
            guard mayWrite && !previewOnly else {
                interactionStatus = "仅预览结果；未获模型路由修改授权"
                Task { await interaction.cancelTask(generation: generation) }
                return
            }
            Task { @MainActor [weak self] in
                guard let self, self.resultGeneration.accepts(token) else { return }
                do {
                    try await self.interaction.replaceIfCurrent(snapshot, with: text,
                                                               generation: generation)
                    self.interactionStatus = "原选区验证通过并已替换"
                } catch {
                    self.interactionStatus = Self.insertionErrorMessage(error)
                }
            }
        case .failure(let failure):
            interactionStatus = "文本任务失败或已取消：\(failure.localizedDescription)"
            llmProbeStatus = "LLM：实际文本任务失败或已取消"
            if spoken { recordingStatus = "语音选区处理失败" }
            Task { await interaction.cancelTask(generation: generation) }
        }
    }

    private static func captureErrorMessage(_ error: Error) -> String {
        guard let failure = error as? InteractionFailure else {
            return "读取外部文本框失败：\(error.localizedDescription)"
        }
        switch failure {
        case .accessibilityPermissionDenied:
            return "缺少辅助功能权限；请授权当前 VoiceRewrite.app 并重新打开"
        case .noFrontmostApplication, .noFocusedElement, .notEditable, .selectionUnavailable:
            return "未识别到可编辑文本框或光标；请先点击目标软件的输入框"
        case .invalidSelection:
            return "听写需要空光标位置；如有选中文字，请先取消选择"
        case .secureField:
            return "密码或安全输入框禁止自动输入"
        case .monitoringUnavailable:
            return "该输入框不支持必要的焦点/选区监测；已保留安全限制"
        case .unsupportedNativeWrite:
            return "该输入框不支持可验证的写入；结果仅在本应用显示"
        case .applicationChanged, .focusChanged, .selectionChanged:
            return "录音开始时焦点或选区已变化；未锁定写入位置"
        case .systemError(let code):
            return "读取目标文本框失败（Accessibility 错误 \(code)）"
        default:
            return "无法安全锁定外部光标（\(failure)）；本次仅显示识别结果"
        }
    }

    private static func insertionErrorMessage(_ error: Error) -> String {
        if let failure = error as? InteractionFailure {
            switch failure {
            case .writeNotConfirmed:
                return "已尝试写入，但无法确认是否生效；请检查目标或复制结果"
            case .unsupportedNativeWrite:
                return "目标不支持可验证写入；结果已保留，可手动复制"
            default:
                return "目标、焦点或选区验证失败；未发起写入，结果可复制"
            }
        }
        if error is ClipboardFailure {
            return "安全剪贴板操作失败；写入状态可能不确定，请检查目标并复制结果"
        }
        return "原应用写入未完成；结果已保留，可手动复制"
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
        cancelProbes()
        cancelCurrent()
        sideMouse?.shutdown()
        sideMouse = nil
        sideMouseStatus = "鼠标侧键监听已停止"
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
