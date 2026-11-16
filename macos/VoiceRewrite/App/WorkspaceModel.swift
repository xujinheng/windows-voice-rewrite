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
    @Published private(set) var asrProbeStatus = ServiceProbeCopy.idle
    @Published private(set) var llmProbeStatus = ServiceProbeCopy.idle
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
    @Published private(set) var sessionHistory: [SessionHistoryEntry] = []
    @Published private(set) var restoredHistoryText: String?
    @Published private(set) var voicePaused = false
    @Published private(set) var updateStatus = "尚未检查更新"
    @Published private(set) var updatePage: String?
    @Published private(set) var isCheckingUpdate = false
    private var historyStore = SessionHistory()
    private var updateCheck = ResultGeneration()

    private let microphone = MicrophoneRecorder()
    private let recordingIndicator = RecordingIndicatorController()
    private var indicatorSnapshot = IndicatorSnapshot()
    private var sideGate = SideVoiceGate()
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

    private var activeShortcutPlan: MacShortcutPlan?
    private var shortcutGeneration = ResultGeneration()

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
        DiagnosticLog.note("应用已启动")
        if case .disconnected = connection { connect() }
        if hotKeys == nil { registerHotKeys() }
        refreshSideMouse()
    }

    func connect() {
        let token = helloGeneration.begin()
        cancelProbes()
        asrProbeStatus = ServiceProbeCopy.idle
        llmProbeStatus = ServiceProbeCopy.idle
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
                    DiagnosticLog.note("Go Helper 已连接")
                    self.checkServices()
                    self.refreshShortcutsFromCore()
                case .failure:
                    self.connection = .failed("Go Helper 握手失败")
                    self.serviceStatus = "不可用"
                }
            }
        }
    }

    func saveSettings() {
        if isRecording || isStarting || isProcessing {
            settingsStatus = SettingsCopy.saveWhileBusy
            return
        }
        if MacSettings.credentialTooLong(asrKeyInput) || MacSettings.credentialTooLong(llmKeyInput) {
            settingsStatus = ContractLimits.credentialTooLong
            return
        }
        guard let dictation = ShortcutBinding(canonical: settings.dictationShortcut),
              let rewrite = ShortcutBinding(canonical: settings.rewriteShortcut),
              let textEdit = ShortcutBinding(canonical: settings.textEditShortcut),
              MacShortcutPlan(dictation: dictation, rewrite: rewrite, textEdit: textEdit) != nil else {
            settingsStatus = "快捷键冲突或不受 macOS 支持；设置未保存"
            return
        }
        _ = shortcutGeneration.begin()
        defer {
            asrKeyInput = ""
            llmKeyInput = ""
        }
        do {
            guard let store else { throw SettingsError.insecureStorage }
            let previous = activeShortcutPlan
            guard try ShortcutSaveTransaction.run(
                activate: { self.register(dictation: dictation, rewrite: rewrite, textEdit: textEdit) },
                persist: { try store.save(self.settings) },
                restore: { self.restoreShortcutPlan(previous) }
            ) else {
                settingsStatus = "快捷键未能注册；设置未保存，原快捷键保持有效"
                return
            }
            try saveCredential(asrKeyInput, account: .asr)
            try saveCredential(llmKeyInput, account: .llm)
            settingsStatus = "设置已保存，密钥不写入配置文件"
            DiagnosticLog.note("设置已保存")
            cancelProbes()
            checkServices()
            refreshShortcutsFromCore()
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
        guard let dictation = ShortcutBinding(canonical: settings.dictationShortcut),
              let rewrite = ShortcutBinding(canonical: settings.rewriteShortcut),
              let textEdit = ShortcutBinding(canonical: settings.textEditShortcut) else {
            interactionStatus = "快捷键将在 Go Helper 连接后确认"
            return
        }
        register(dictation: dictation, rewrite: rewrite, textEdit: textEdit)
    }

    private func refreshShortcutsFromCore() {
        guard let bridge, case .connected = connection else { return }
        let token = shortcutGeneration.begin()
        let values = (dictation: settings.dictationShortcut,
                      rewrite: settings.rewriteShortcut,
                      textEdit: settings.textEditShortcut)
        parseShortcut(values.dictation) { [weak self] dictation in
            guard let self, self.shortcutGeneration.accepts(token), let dictation else { return }
            self.parseShortcut(values.rewrite) { [weak self] rewrite in
                guard let self, self.shortcutGeneration.accepts(token), let rewrite else { return }
                self.parseShortcut(values.textEdit) { [weak self] textEdit in
                    guard let self, self.shortcutGeneration.accepts(token), let textEdit else { return }
                    if self.register(dictation: dictation, rewrite: rewrite, textEdit: textEdit) {
                        self.settings.dictationShortcut = dictation.normalized
                        self.settings.rewriteShortcut = rewrite.normalized
                        self.settings.textEditShortcut = textEdit.normalized
                    }
                }
            }
        }
    }

    private func parseShortcut(_ value: String, done: @escaping (ShortcutBinding?) -> Void) {
        guard let bridge else {
            done(nil)
            return
        }
        bridge.send(operation: "parse_shortcut", fields: ["shortcut": .string(value)]) { [weak self] reply in
            Task { @MainActor [weak self] in
                guard let self else { return }
                switch reply {
                case .success(let response) where response.ok:
                    done(ShortcutBinding(response.result))
                case .success(let response):
                    self.interactionStatus = FailureCopy.status(prefix: "快捷键无效：", message: response.error?.userText ?? "无法解析")
                    done(nil)
                case .failure(let failure):
                    self.interactionStatus = FailureCopy.status(prefix: "快捷键无效：", message: failure.localizedDescription)
                    done(nil)
                }
            }
        }
    }

    /// Keep working global shortcuts registered until every new key succeeds.
    @discardableResult
    private func register(dictation: ShortcutBinding, rewrite: ShortcutBinding,
                          textEdit: ShortcutBinding) -> Bool {
        guard let next = MacShortcutPlan(dictation: dictation, rewrite: rewrite, textEdit: textEdit) else {
            interactionStatus = "快捷键冲突或不受 macOS 支持；原快捷键保持有效"
            return false
        }
        let label = "\(MacShortcut.display(dictation.normalized)) 听写；\(MacShortcut.display(textEdit.normalized)) 文本指令；\(MacShortcut.display(rewrite.normalized)) 语音修改选区"
        do {
            if let registry = hotKeys {
                try registry.replace(with: next.keyboard)
            } else {
                let (driver, registry) = try createHotKeyRegistry(plan: next)
                hotKeyDriver = driver
                hotKeys = registry
            }
            activeShortcutPlan = next
            sideMouse?.setBindings(next.sideButtons)
            interactionStatus = label
            return true
        } catch {
            interactionStatus = "快捷键注册失败：\(error.localizedDescription)；原快捷键保持有效"
            return false
        }
    }

    private func restoreShortcutPlan(_ previous: MacShortcutPlan?) {
        guard let registry = hotKeys else {
            if let previous, let restored = try? createHotKeyRegistry(plan: previous) {
                hotKeyDriver = restored.0
                hotKeys = restored.1
                activeShortcutPlan = previous
                sideMouse?.setBindings(previous.sideButtons)
            }
            return
        }
        do {
            try registry.replace(with: previous?.keyboard ?? [])
            activeShortcutPlan = previous
            sideMouse?.setBindings(previous?.sideButtons ?? SideMouseBindings())
        } catch {
            interactionStatus = "无法恢复原快捷键：\(error.localizedDescription)"
        }
    }

    private func createHotKeyRegistry(plan: MacShortcutPlan) throws -> (CarbonHotKeyDriver, HotKeyRegistry) {
        let driver = CarbonHotKeyDriver { [weak self] shortcut, pid in
            guard let self, let bound = self.activeShortcutPlan else { return }
            if shortcut == bound.dictation {
                self.toggleRecording(from: pid)
            } else if shortcut == bound.textEdit {
                self.processSelectedText(from: pid)
            } else if shortcut == bound.rewrite {
                self.toggleVoiceSelectedText(from: pid)
            }
        }
        let registry = HotKeyRegistry(driver: driver)
        do {
            for shortcut in plan.keyboard { try registry.register(shortcut) }
        } catch {
            registry.shutdown()
            driver.shutdown()
            throw error
        }
        return (driver, registry)
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
            bindings: activeShortcutPlan?.sideButtons ?? SideMouseBindings(),
            onPress: { [weak self] action in
                self?.previewIndicator(action)
            },
            onPreviewDismissed: { [weak self] in
                self?.dismissPreviewIndicator()
            },
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
                if let action = self?.activeShortcutPlan?.sideButtons.action(for: button) ?? SideMouseBindings().action(for: button) {
                    self?.sideMouseStatus = action == .dictation
                        ? "已收到 Mouse\(button + 1) 按下；继续按住以开始听写"
                        : "已收到 Mouse\(button + 1) 按下；继续按住以开始改写"
                } else {
                    self?.sideMouseStatus = "检测到辅助鼠标按钮 \(button)；当前绑定按钮 3/4"
                }
            }
        )
        do {
            driver.setBindings(activeShortcutPlan?.sideButtons ?? SideMouseBindings())
            try driver.start()
            sideMouse = driver
            driver.setVoicePaused(voicePaused)
            if isFreshStart {
                sideMouseStatus = voicePaused ? VoicePauseCopy.paused : "监听已启动；等待已绑定的鼠标侧键事件"
            }
        } catch {
            driver.shutdown()
            sideMouse = nil
            sideMouseStatus = "监听失败：\(error.localizedDescription)；键盘快捷键仍可用"
        }
    }

    func toggleVoicePause() {
        if isRecording || isStarting || isProcessing {
            interactionStatus = VoicePauseCopy.busy
            return
        }
        voicePaused.toggle()
        sideMouse?.setVoicePaused(voicePaused)
        dismissPreviewIndicator()
        sideMouseStatus = voicePaused ? VoicePauseCopy.paused : VoicePauseCopy.idle
    }

    private func sideMouseHeld(_ action: SideMouseAction, targetPID: Int32?) {
        switch sideGate.hold(paused: voicePaused, busy: isRecording || isStarting || isProcessing) {
        case .dismissPreview:
            dismissPreviewIndicator()
            return
        case .ignored:
            return
        case .start:
            break
        }
        let mappedButton = (activeShortcutPlan?.sideButtons ?? SideMouseBindings()).button(for: action)
        sideMouseStatus = mappedButton.map { "Mouse\($0 + 1) 长按已触发" } ?? "已通过配置的快捷键触发"
        switch action {
        case .dictation: toggleRecording(from: targetPID)
        case .rewrite: toggleVoiceSelectedText(from: targetPID)
        }
        // Starting a new task invalidates the previous task's input ownership.
        sideGate.began(action)
    }

    private func sideMouseReleased(_ action: SideMouseAction) {
        switch sideGate.release(action, starting: isStarting, recording: isRecording) {
        case .none, .wait:
            return
        case .stop:
            stopRecording()
        case .clear:
            break
        }
    }

    private func stopWhenSideMouseReleased(_ action: SideMouseAction) {
        switch sideGate.finishStart(action, recording: isRecording) {
        case .stop:
            stopRecording()
        case .none, .wait, .clear:
            break
        }
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

    func markConnectionProbeStale(_ operation: HelperOperation) {
        cancelProbes()
        setProbeStatus(ServiceProbeCopy.stale, for: operation)
    }

    private func cancelProbes() {
        _ = probeGeneration.begin()
        for id in activeProbeIDs.values { bridge?.cancel(id: id) }
        activeProbeIDs.removeAll()
        isCheckingServices = false
    }

    // Windows and macOS use the same Go Core probe semantics via voicebridge.
    // No user microphone input or selected text is sent by these operations.
    func checkForUpdate() {
        guard let bridge, case .connected = connection else {
            updateStatus = "请先连接 Go Helper"
            updatePage = nil
            return
        }
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
        let token = updateCheck.begin()
        isCheckingUpdate = true
        updateStatus = "正在检查更新"
        updatePage = nil
        bridge.send(operation: "check_update", fields: ["current_version": .string(version)]) { [weak self] reply in
            Task { @MainActor [weak self] in
                guard let self, self.updateCheck.accepts(token) else { return }
                self.isCheckingUpdate = false
                switch reply {
                case .success(let response):
                    if response.ok {
                        self.updateStatus = response.result?["summary"]?.string ?? "检查更新没有返回说明"
                        if response.result?["newer"]?.boolValue == true {
                            self.updatePage = response.result?["page"]?.string
                        }
                    } else {
                        self.updateStatus = response.error?.userText ?? "检查更新失败"
                    }
                case .failure(let failure):
                    self.updateStatus = "检查更新失败：\(failure.localizedDescription)"
                }
                DiagnosticLog.note(self.updateStatus)
            }
        }
    }

    func openUpdatePage() {
        guard let updatePage, let url = URL(string: updatePage) else { return }
        NSWorkspace.shared.open(url)
    }

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
            setProbeStatus("\(name)：" + ServiceProbeCopy.running, for: operation)
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
        sideGate.clear()
        isProcessing = false
    }

    func toggleRecording(from targetPID: Int32? = nil) {
        if voicePaused && !isRecording { return }
        if isRecording { stopRecording() }
        else if !isStarting && !isProcessing { startRecording(targetPID: targetPID) }
    }

    func toggleVoiceSelectedText(from targetPID: Int32?) {
        if voicePaused && !isRecording { return }
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
        showIndicator(phase: .selection, mode: "改写", pending: false)
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
                if let message = SelectionLimit.tooLongMessage(selected: snapshot.originalSelectedText) {
                    await interaction.cancelTask(generation: next)
                    interactionStatus = message
                    recordingStatus = "未录音"
                    showIndicator(phase: .error, mode: "改写", pending: false)
                } else {
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
                    showIndicator(phase: .recording, mode: "改写", pending: false)
                    recordingStatus = "语音修改选区录音中；松开当前绑定侧键或再次按配置的快捷键结束"
                    interactionStatus = "已锁定选区；录音后校验焦点与选区是否仍有效"
                }
            } catch {
                microphone.cancel()
                if let generation { await interaction.cancelTask(generation: generation) }
                if resultGeneration.accepts(token) {
                    interactionStatus = rewriteCaptureStatus(error, password: ResultCopy.passwordSelection)
                    recordingStatus = "未录音"
                    showIndicator(phase: .error, mode: "改写", pending: false)
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
                    interactionStatus = captureStatus(error, password: ResultCopy.passwordDictation)
                }
            } else {
                interactionStatus = "未指定外部文本框；本次录音仅显示识别结果。请在目标软件中按配置的听写快捷键开始听写"
            }
            guard resultGeneration.accepts(token) else { return }
            do {
                try await microphone.start()
                guard resultGeneration.accepts(token) else { microphone.cancel(); return }
                isRecording = true
                showIndicator(phase: .recording, mode: "听写", pending: false)
                recordingStatus = "录音中；松开当前绑定侧键或再次按配置的快捷键停止"
            } catch {
                recordingStatus = "无法启动麦克风：\(error.localizedDescription)"
                showIndicator(phase: .error, mode: "听写", pending: false)
            }
            isStarting = false
            stopWhenSideMouseReleased(.dictation)
        }
    }

    func stopRecording() {
        guard isRecording else { return }
        sideGate.clear()
        isRecording = false
        isProcessing = true
        showIndicator(phase: .processing, mode: indicatorSnapshot.mode, pending: false)
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
                                self.recordingStatus = FailureCopy.status(prefix: "ASR 请求失败：", message: response.error?.userText ?? "invalid_reply")
                                self.asrProbeStatus = "ASR：实际转写失败"
                                self.showIndicator(phase: .error, mode: "听写", pending: false)
                                return
                            }
                            if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                                self.recordingStatus = ResultCopy.dictationEmpty
                                self.asrProbeStatus = "ASR：实际转写失败"
                                self.showIndicator(phase: .error, mode: "听写", pending: false)
                                return
                            }
                            self.transcription = text
                            self.rememberHistory(title: ResultCopy.dictationHistory, value: text)
                            self.recordingStatus = "语音识别已完成"
                            self.asrProbeStatus = "ASR：真实录音识别成功"
                            if let insertion {
                                self.showIndicator(phase: .applying, mode: "听写", pending: false)
                                Task { @MainActor [weak self] in
                                    guard let self, self.resultGeneration.accepts(token) else { return }
                                    do {
                                        try await self.interaction.insertIfCurrent(
                                            insertion.snapshot, text: text, generation: insertion.generation)
                                        self.interactionStatus = "已安全插入原目标"
                                        self.showIndicator(phase: .success, mode: "听写", pending: false)
                                    } catch {
                                        self.interactionStatus = insertionStatus(error, rewrite: false, passwordRewrite: ResultCopy.passwordRewrite, passwordDictation: ResultCopy.passwordDictation)
                                        ResultCardPanel.shared.present(title: ResultCopy.dictationCopyManually, text: text)
                                        self.showIndicator(phase: .fallback, mode: "听写", pending: false)
                                    }
                                }
                            } else {
                                ResultCardPanel.shared.present(title: ResultCopy.dictationNotInserted, text: text)
                                self.showIndicator(phase: .fallback, mode: "听写", pending: false)
                            }
                        case .failure(let failure):
                            self.recordingStatus = "识别失败：\(failure.localizedDescription)"
                            self.asrProbeStatus = "ASR：实际转写失败"
                            self.showIndicator(phase: .error, mode: "听写", pending: false)
                        }
                    }
                }
                activeRequestID = id
            } catch {
                guard resultGeneration.accepts(token) else { return }
                isProcessing = false
                recordingStatus = "无法识别：\(error.localizedDescription)"
                asrProbeStatus = "ASR：实际转写未完成"
                showIndicator(phase: .error, mode: indicatorSnapshot.mode, pending: false)
                if let voiceSelection {
                    await interaction.cancelTask(generation: voiceSelection.generation)
                    interactionStatus = "语音选区任务未执行"
                }
            }
        }
    }

    func processSelectedText(from targetPID: Int32?) {
        guard !voicePaused else {
            interactionStatus = VoicePauseCopy.paused
            return
        }
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
        showIndicator(phase: .selection, mode: "改写", pending: false)
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
                if let message = SelectionLimit.tooLongMessage(selected: snapshot.originalSelectedText) {
                    await interaction.cancelTask(generation: generation)
                    isProcessing = false
                    interactionStatus = message
                    llmProbeStatus = "LLM：实际任务未完成"
                    showIndicator(phase: .error, mode: "改写", pending: false)
                    return
                }
                cancelProbes()
                llmProbeStatus = "LLM：正在处理选区…"
                showIndicator(phase: .rewriting, mode: "改写", pending: false)
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
                showIndicator(phase: .error, mode: "改写", pending: false)
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
                interactionStatus = FailureCopy.status(prefix: "模型处理失败：", message: response.error?.userText ?? "invalid_reply")
                if spoken {
                    recordingStatus = "语音修改未完成"
                    asrProbeStatus = "ASR / LLM：语音选区任务失败"
                }
                llmProbeStatus = "LLM：实际任务失败"
                showIndicator(phase: .error, mode: "改写", pending: false)
                Task { await interaction.cancelTask(generation: generation) }
                return
            }
            if spoken {
                guard let instruction = response.result?["instruction"]?.string,
                      !instruction.isEmpty else {
                    interactionStatus = "语音指令响应缺失；未写入原选区"
                    showIndicator(phase: .error, mode: "改写", pending: false)
                    Task { await interaction.cancelTask(generation: generation) }
                    return
                }
                transcription = instruction
                rememberHistory(title: ResultCopy.dictationHistory, value: instruction)
                asrProbeStatus = "ASR：语音指令识别成功"
                recordingStatus = "语音修改已处理"
            }
            processedText = text
            let intent = response.result?["intent"]?.string ?? ""
            rememberHistory(title: ResultCopy.historyTitle(intent: intent), value: text)
            llmProbeStatus = "LLM：实际文本任务成功"
            let cardTitle = ResultCopy.cardTitle(intent: intent)
            let mayWrite = response.result?["may_attempt_replace"]?.boolValue == true
            let previewOnly = response.result?["preview_only"]?.boolValue != false
            guard mayWrite && !previewOnly else {
                ResultCardPanel.shared.present(title: cardTitle, text: text)
                interactionStatus = "仅预览结果；未获模型路由修改授权"
                showIndicator(phase: .answer, mode: "改写", pending: false)
                Task { await interaction.cancelTask(generation: generation) }
                return
            }
            showIndicator(phase: .applying, mode: "改写", pending: false)
            Task { @MainActor [weak self] in
                guard let self, self.resultGeneration.accepts(token) else { return }
                do {
                    try await self.interaction.replaceIfCurrent(snapshot, with: text,
                                                               generation: generation)
                    self.interactionStatus = "原选区验证通过并已替换"
                    self.showIndicator(phase: .success, mode: "改写", pending: false)
                } catch {
                    self.interactionStatus = insertionStatus(error, rewrite: true, passwordRewrite: ResultCopy.passwordRewrite, passwordDictation: ResultCopy.passwordDictation)
                    ResultCardPanel.shared.present(title: cardTitle, text: text)
                    self.showIndicator(phase: .fallback, mode: "改写", pending: false)
                }
            }
        case .failure(let failure):
            interactionStatus = "文本任务失败或已取消：\(failure.localizedDescription)"
            llmProbeStatus = "LLM：实际文本任务失败或已取消"
            if spoken { recordingStatus = "语音选区处理失败" }
            showIndicator(phase: .error, mode: "改写", pending: false)
            Task { await interaction.cancelTask(generation: generation) }
        }
    }

    func clearSessionHistory() {
        historyStore.clear()
        sessionHistory = []
        restoredHistoryText = nil
        ResultCardPanel.shared.dismiss()
    }

    func showSessionHistory(id: UInt64) {
        guard let entry = historyStore.entry(id: id) else { return }
        restoredHistoryText = entry.text
        ResultCardPanel.shared.present(title: ResultCopy.historyRestoreTitle(entry.title), text: entry.text)
    }

    private func rememberHistory(title: String, value: String) {
        historyStore.remember(title: title, value: value)
        sessionHistory = historyStore.entries
    }

    func cancelCurrent() {
        dismissIndicator()
        invalidatePending()
        microphone.cancel()
        isRecording = false
        isStarting = false
        isProcessing = false
        recordingStatus = "已取消"
        interactionStatus = "已取消当前操作"
    }

    private func previewIndicator(_ action: SideMouseAction) {
        guard !isRecording && !isStarting && !isProcessing else { return }
        showIndicator(phase: .recording, mode: action == .dictation ? "听写" : "改写", pending: true)
    }

    private func dismissPreviewIndicator() {
        sideGate.allowNextHold()
        guard indicatorSnapshot.pending else { return }
        dismissIndicator()
    }

    private func showIndicator(phase: IndicatorPhase, mode: String, pending: Bool) {
        let now = Date()
        if !indicatorSnapshot.visible {
            indicatorSnapshot.started = now
        }
        if !indicatorSnapshot.visible || indicatorSnapshot.phase != phase || indicatorSnapshot.pending != pending {
            indicatorSnapshot.changed = now
        }
        indicatorSnapshot.visible = true
        indicatorSnapshot.pending = pending
        indicatorSnapshot.phase = phase
        indicatorSnapshot.mode = mode
        recordingIndicator.present(indicatorSnapshot, levelSource: { [weak self] in
            IndicatorMath.normalizeMicrophoneLevel(Double(self?.microphone.rmsLevel ?? 0))
        }, onCancel: { [weak self] in
            guard let self else { return }
            switch indicatorCancel(pending: self.indicatorSnapshot.pending, terminal: IndicatorMath.terminal(self.indicatorSnapshot.phase)) {
            case .ignoreHold:
                self.sideGate.suppressNextHold()
                self.dismissIndicator()
            case .dismiss:
                self.dismissIndicator()
            case .cancelWork:
                self.cancelCurrent()
            }
        }, onAutoDismiss: { [weak self] in
            self?.indicatorSnapshot.visible = false
            self?.indicatorSnapshot.pending = false
        })
    }

    private func dismissIndicator() {
        indicatorSnapshot.visible = false
        indicatorSnapshot.pending = false
        recordingIndicator.dismiss()
    }

    func shutdown() {
        _ = helloGeneration.begin()
        _ = shortcutGeneration.begin()
        cancelProbes()
        cancelCurrent()
        sideMouse?.shutdown()
        sideMouse = nil
        sideMouseStatus = "鼠标侧键监听已停止"
        hotKeys?.shutdown()
        hotKeyDriver?.shutdown()
        hotKeys = nil
        hotKeyDriver = nil
        activeShortcutPlan = nil
        bridge?.stop()
        bridge = nil
        connection = .disconnected
        Task { await interaction.shutdown() }
    }
}
