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
    @Published private(set) var recordingStatus = recordingIdle()
    @Published private(set) var serviceStatus = helperNotYetConnected()
    @Published private(set) var asrProbeStatus = ServiceProbeCopy.idle
    @Published private(set) var llmProbeStatus = ServiceProbeCopy.idle
    @Published private(set) var isCheckingServices = false
    @Published private(set) var interactionStatus = shortcutsUnregistered()
    @Published private(set) var sideMouseStatus = sideMouseUnchecked()
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
    @Published private(set) var updateStatus = updateNotChecked()
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
            settingsStatus = settingsLoaded()
        } catch {
            settingsStatus = settingsLoadFailed()
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
        serviceStatus = helperStarting()
        guard let executable = Bundle.main.executableURL?.deletingLastPathComponent()
            .appendingPathComponent("voicebridge") else {
            connection = .failed(helperBundleMissing())
            return
        }
        let client = BridgeClient(executableURL: executable) { [weak self] failure in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                self.connection = .failed(failure.localizedDescription)
                self.serviceStatus = helperDisconnected()
                self.cancelProbes()
                self.asrProbeStatus = helperProbeDisconnected()
                self.llmProbeStatus = helperProbeDisconnected()
                self.invalidatePending()
            }
        }
        bridge = client
        do { try client.start() }
        catch {
            connection = .failed(helperStartFailed())
            serviceStatus = helperUnavailable()
            return
        }
        client.send(operation: "hello") { [weak self] response in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                switch response {
                case .success(let reply):
                    guard helperHelloAccepted(ok: reply.ok, name: reply.result?["name"]?.string, version: reply.result?["protocol"]?.int) else {
                        self.connection = .failed(helperProtocolRejected())
                        self.serviceStatus = helperHandshakeFailedStatus()
                        return
                    }
                    self.connection = .connected(helperConnectedLabel())
                    self.serviceStatus = helperConnected()
                    DiagnosticLog.note(helperConnected())
                    self.checkServices()
                    self.refreshShortcutsFromCore()
                case .failure:
                    self.connection = .failed(helperHandshakeFailed())
                    self.serviceStatus = helperUnavailable()
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
            settingsStatus = shortcutSaveRejected()
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
                settingsStatus = shortcutNotRegistered()
                return
            }
            try saveCredential(asrKeyInput, account: .asr)
            try saveCredential(llmKeyInput, account: .llm)
            settingsStatus = settingsSaved()
            DiagnosticLog.note(settingsSavedLog())
            cancelProbes()
            checkServices()
            refreshShortcutsFromCore()
        } catch {
            settingsStatus = settingsSaveFailed(error.localizedDescription)
        }
    }

    private func saveCredential(_ value: String, account: CredentialAccount) throws {
        guard MacSettings.replacesStoredCredential(value) else { return }
        do {
            try credentials.add(value, for: account)
        } catch CredentialError.duplicate {
            try credentials.update(value, for: account)
        }
    }

    func requestAccessibility() {
        accessibilityAuthorized = InteractionSession.accessibilityPermissionGranted(prompt: true)
        interactionStatus = accessibilityAuthorized
            ? accessibilityGranted()
            : accessibilityNeeded()
        refreshSideMouse()
    }

    private func registerHotKeys() {
        guard let dictation = ShortcutBinding(canonical: settings.dictationShortcut),
              let rewrite = ShortcutBinding(canonical: settings.rewriteShortcut),
              let textEdit = ShortcutBinding(canonical: settings.textEditShortcut) else {
            interactionStatus = shortcutPendingHelper()
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
            interactionStatus = shortcutPlanRejected()
            return false
        }
        let label = shortcutPlanLabel(
            dictation: MacShortcut.display(dictation.normalized),
            textEdit: MacShortcut.display(textEdit.normalized),
            rewrite: MacShortcut.display(rewrite.normalized))
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
            sideMouseStatus = sideMouseAccessibilityDenied()
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
                let action = self?.activeShortcutPlan?.sideButtons.action(for: button) ?? SideMouseBindings().action(for: button)
                self?.sideMouseStatus = sideMouseObserved(button: button, action: action)
            }
        )
        do {
            driver.setBindings(activeShortcutPlan?.sideButtons ?? SideMouseBindings())
            try driver.start()
            sideMouse = driver
            driver.setVoicePaused(voicePaused)
            if isFreshStart {
                sideMouseStatus = voicePaused ? VoicePauseCopy.paused : sideMouseListening()
            }
        } catch {
            driver.shutdown()
            sideMouse = nil
            sideMouseStatus = sideMouseListenFailed(error.localizedDescription)
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
        sideMouseStatus = sideMouseHoldStarted(button: mappedButton)
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
            updateStatus = helperRequired()
            updatePage = nil
            return
        }
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
        let token = updateCheck.begin()
        isCheckingUpdate = true
        updateStatus = updateChecking()
        updatePage = nil
        bridge.send(operation: "check_update", fields: ["current_version": .string(version)]) { [weak self] reply in
            Task { @MainActor [weak self] in
                guard let self, self.updateCheck.accepts(token) else { return }
                self.isCheckingUpdate = false
                switch reply {
                case .success(let response):
                    if response.ok {
                        self.updateStatus = response.result?["summary"]?.string ?? updateMissingSummary()
                        if response.result?["newer"]?.boolValue == true {
                            self.updatePage = response.result?["page"]?.string
                        }
                    } else {
                        self.updateStatus = response.error?.userText ?? updateFailed()
                    }
                case .failure(let failure):
                    self.updateStatus = updateFailedDetail(failure.localizedDescription)
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
                setProbeStatus(helperNotConnectedProbe(), for: operation)
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
            setProbeStatus(serviceProbeRunning(name: name, running: ServiceProbeCopy.running), for: operation)
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
                            self.setProbeStatus(serviceProbeStatus(
                                name: name, ok: reply.ok, kind: reply.result?["kind"]?.string,
                                message: reply.result?["message"]?.string,
                                failure: reply.error?.code ?? "invalid_reply"), for: operation)
                        case .failure(let error):
                            self.setProbeStatus(serviceProbeStatus(
                                name: name, ok: false, kind: nil, message: nil,
                                failure: error.localizedDescription), for: operation)
                        }
                    }
                }
                activeProbeIDs[bridgeOperation] = id
                isCheckingServices = true
            } catch {
                setProbeStatus(serviceProbeConfigStatus(name: name, detail: error.localizedDescription),
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
        switch recordingToggle(paused: voicePaused, recording: isRecording, starting: isStarting, processing: isProcessing) {
        case .ignore:
            return
        case .stop:
            stopRecording()
        case .start:
            startRecording(targetPID: targetPID)
        }
    }

    func toggleVoiceSelectedText(from targetPID: Int32?) {
        let decision = rewriteToggle(
            paused: voicePaused, recording: isRecording, voiceSelection: isVoiceSelectionRecording,
            starting: isStarting, processing: isProcessing, targetPID: targetPID)
        let pid: Int32
        switch decision {
        case .ignore:
            return
        case .stop:
            stopRecording()
            return
        case .missingTarget:
            interactionStatus = rewriteNoTargetForSelection()
            return
        case .start(let target):
            pid = target
        }
        invalidatePending()
        let token = resultGeneration.begin()
        isStarting = true
        interactionStatus = rewriteCapturingSelection()
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
                    recordingStatus = recordingIdle()
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
                    recordingStatus = rewriteRecording()
                    interactionStatus = rewriteSelectionLocked()
                }
            } catch {
                microphone.cancel()
                if let generation { await interaction.cancelTask(generation: generation) }
                if resultGeneration.accepts(token) {
                    interactionStatus = rewriteCaptureStatus(error, password: ResultCopy.passwordSelection)
                    recordingStatus = recordingIdle()
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
        recordingStatus = dictationRequestingMicrophone()
        Task { @MainActor [weak self] in
            guard let self else { return }
            if let pid = targetPID, pid > 0 {
                do {
                    let generation = try await interaction.beginTask(targetProcessID: pid)
                    let snapshot = try await interaction.captureSelection(
                        generation: generation, intent: .insertAtCaret)
                    if resultGeneration.accepts(token) {
                        activeInsert = (snapshot, generation)
                        interactionStatus = dictationCaretLocked()
                    }
                } catch {
                    interactionStatus = captureStatus(error, password: ResultCopy.passwordDictation)
                }
            } else {
                interactionStatus = dictationNoTarget()
            }
            guard resultGeneration.accepts(token) else { return }
            do {
                try await microphone.start()
                guard resultGeneration.accepts(token) else { microphone.cancel(); return }
                isRecording = true
                showIndicator(phase: .recording, mode: "听写", pending: false)
                recordingStatus = dictationRecording()
            } catch {
                recordingStatus = dictationMicrophoneFailed(error.localizedDescription)
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
        recordingStatus = dictationProcessing()
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
                    asrProbeStatus = probeRewriteListeningASR()
                    llmProbeStatus = probeRewriteWaitingLLM()
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
                asrProbeStatus = probeTranscribing()
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
                        let response = try? reply.get()
                        let follow = dictationTranscriptFollowUp(
                            ok: response?.ok ?? false,
                            text: response?.result?["text"]?.string,
                            transport: self.replyFailure(reply),
                            hasInsertion: insertion != nil,
                            failureStatus: FailureCopy.status(prefix: "ASR 请求失败：", message: response?.error?.userText ?? "invalid_reply"),
                            emptyStatus: ResultCopy.dictationEmpty,
                            historyTitle: ResultCopy.dictationHistory,
                            notInsertedTitle: ResultCopy.dictationNotInserted)
                        self.applyTask(follow, token: token, insertion: insertion)
                    }
                }
                activeRequestID = id
            } catch {
                guard resultGeneration.accepts(token) else { return }
                isProcessing = false
                applyTask(recordingStopFollowUp(detail: error.localizedDescription, voiceSelection: voiceSelection != nil, mode: indicatorSnapshot.mode), generation: voiceSelection?.generation)
            }
        }
    }

    func processSelectedText(from targetPID: Int32?) {
        let pid: Int32
        switch selectionTaskStart(paused: voicePaused, targetPID: targetPID, instruction: taskInstruction) {
        case .paused:
            interactionStatus = VoicePauseCopy.paused
            return
        case .missingTarget:
            interactionStatus = rewriteNoTarget()
            return
        case .missingInstruction:
            interactionStatus = rewriteNeedInstruction()
            return
        case .start(let target):
            pid = target
        }
        invalidatePending()
        let token = resultGeneration.begin()
        isProcessing = true
        interactionStatus = rewriteCheckingSelection()
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
                    llmProbeStatus = probeTaskIncomplete()
                    showIndicator(phase: .error, mode: "改写", pending: false)
                    return
                }
                cancelProbes()
                llmProbeStatus = probeRewritingSelection()
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
                interactionStatus = rewriteTaskNotStarted(error.localizedDescription)
                llmProbeStatus = probeTaskIncomplete()
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
        let response = try? reply.get()
        let result = response?.result
        let intent = result?["intent"]?.string ?? ""
        let follow = selectedTaskFollowUp(
            ok: response?.ok ?? false,
            text: result?["text"]?.string,
            spoken: spoken,
            instruction: result?["instruction"]?.string,
            mayWrite: result?["may_attempt_replace"]?.boolValue == true,
            previewOnly: result?["preview_only"]?.boolValue != false,
            transport: replyFailure(reply),
            failureStatus: FailureCopy.status(prefix: "模型处理失败：", message: response?.error?.userText ?? "invalid_reply"),
            historyTitle: ResultCopy.historyTitle(intent: intent),
            cardTitle: ResultCopy.cardTitle(intent: intent),
            spokenHistoryTitle: ResultCopy.dictationHistory)
        applyTask(follow, generation: generation, token: token, replacement: (snapshot, generation))
    }

    private func replyFailure(_ reply: Result<BridgeReply, BridgeFailure>) -> String? {
        if case .failure(let failure) = reply {
            return failure.localizedDescription
        }
        return nil
    }

    private func applyTask(
        _ follow: TaskFollowUp,
        generation: UInt64? = nil,
        token: UInt64? = nil,
        insertion: (snapshot: SelectionSnapshot, generation: UInt64)? = nil,
        replacement: (snapshot: SelectionSnapshot, generation: UInt64)? = nil
    ) {
        if let text = follow.transcription { transcription = text }
        if let text = follow.processedText { processedText = text }
        for item in follow.history { rememberHistory(title: item.title, value: item.value) }
        if let status = follow.recordingStatus { recordingStatus = status }
        if let status = follow.interactionStatus { interactionStatus = status }
        if let probe = follow.asrProbe { asrProbeStatus = probe }
        if let probe = follow.llmProbe { llmProbeStatus = probe }
        if let phase = follow.phase, let indicator = IndicatorPhase(rawValue: phase.rawValue) {
            showIndicator(phase: indicator, mode: follow.mode, pending: false)
        }
        if let title = follow.cardTitle, let text = follow.cardText {
            ResultCardPanel.shared.present(title: title, text: text)
        }
        if follow.cancelTask, let generation {
            Task { await interaction.cancelTask(generation: generation) }
        }
        if let text = follow.insertText, let insertion, let token {
            Task { @MainActor [weak self] in
                guard let self, self.resultGeneration.accepts(token) else { return }
                do {
                    try await self.interaction.insertIfCurrent(insertion.snapshot, text: text, generation: insertion.generation)
                    self.applyTask(dictationInsertFollowUp(succeeded: true, status: "", manualTitle: "", text: text))
                } catch {
                    self.applyTask(dictationInsertFollowUp(
                        succeeded: false,
                        status: insertionStatus(error, rewrite: false, passwordRewrite: ResultCopy.passwordRewrite, passwordDictation: ResultCopy.passwordDictation),
                        manualTitle: ResultCopy.dictationCopyManually,
                        text: text))
                }
            }
        }
        if let text = follow.replaceText, let replacement, let token {
            let title = follow.cardTitle ?? ""
            Task { @MainActor [weak self] in
                guard let self, self.resultGeneration.accepts(token) else { return }
                do {
                    try await self.interaction.replaceIfCurrent(replacement.snapshot, with: text, generation: replacement.generation)
                    self.applyTask(selectedWriteFollowUp(succeeded: true, status: "", cardTitle: title, text: text))
                } catch {
                    self.applyTask(selectedWriteFollowUp(
                        succeeded: false,
                        status: insertionStatus(error, rewrite: true, passwordRewrite: ResultCopy.passwordRewrite, passwordDictation: ResultCopy.passwordDictation),
                        cardTitle: title,
                        text: text))
                }
            }
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
        recordingStatus = rewriteCanceled()
        interactionStatus = rewriteCanceledDetail()
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
            switch IndicatorMath.cancelChoice(pending: self.indicatorSnapshot.pending, current: true, terminal: IndicatorMath.terminal(self.indicatorSnapshot.phase)) {
            case IndicatorMath.cancelPending:
                self.sideGate.suppressNextHold()
                self.dismissIndicator()
            case IndicatorMath.cancelDismiss:
                self.dismissIndicator()
            case IndicatorMath.cancelWork:
                self.cancelCurrent()
            default:
                break
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
        sideMouseStatus = sideMouseStopped()
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
