import AppKit
import Combine
import Foundation
import Microphone
import VoiceRewriteInteraction
import VoiceRewriteStorage

struct MeetingCaptionRow: Identifiable, Equatable {
    let id: Int
    let source: String
    let chinese: String
    let final: Bool
}

struct MeetingReplyRow: Identifiable, Equatable {
    let id: Int
    let english: String
    let chinese: String
    let complete: Bool
}

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
    @Published private(set) var microphonePermission: MicrophonePermissionStatus = .notDetermined
    @Published private(set) var microphoneDevices: [MicrophoneInputDevice] = []
    @Published private(set) var serviceStatus = helperNotYetConnected()
    @Published private(set) var asrProbeStatus = ServiceProbeCopy.idle
    @Published private(set) var llmProbeStatus = ServiceProbeCopy.idle
    @Published private(set) var isCheckingServices = false
    @Published private(set) var asrAvailableModels: [String] = []
    @Published private(set) var llmAvailableModels: [String] = []
    @Published private(set) var asrModelDiscoveryStatus = ""
    @Published private(set) var llmModelDiscoveryStatus = ""
    @Published private(set) var isDiscoveringASRModels = false
    @Published private(set) var isDiscoveringLLMModels = false
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
    @Published private(set) var meetingSources: [MeetingAudioSource] = []
    @Published var meetingSourceID = "system"
    @Published private(set) var meetingStatus = "选择音频来源后启动会议"
    @Published private(set) var meetingCaptions: [MeetingCaptionRow] = []
    @Published private(set) var meetingStarting = false
    @Published private(set) var meetingActive = false
    @Published private(set) var meetingReplies: [MeetingReplyRow] = []
    @Published private(set) var meetingReplyPending = false
    @Published private(set) var meetingCaptionsOnly = false
    @Published private(set) var meetingExpressionExpanded = false
    @Published private(set) var meetingReplyStatus = ""
    @Published private(set) var meetingReplySource = ""
    @Published private(set) var meetingReplyTip = ""
    @Published private(set) var meetingExpression = ""
    @Published private(set) var meetingExpressionChinese = ""
    @Published private(set) var meetingExpressionStatus = ""
    @Published private(set) var meetingCopyStatus = ""
    @Published private(set) var meetingBusy = false
    @Published private(set) var meetingCancelPending = false
    @Published private(set) var meetingVoiceRecording = false
    @Published private(set) var meetingVoiceStarting = false
    @Published var meetingSelectedIndex = 0
    @Published var meetingInput = ""
    @Published var meetingRefineInput = ""
    private var historyStore = SessionHistory()
    private var updateCheck = ResultGeneration()

    private let microphone = MicrophoneRecorder()
    private let systemAudio = SystemAudioCapture()
    private var meetingGeneration = ResultGeneration()
    private var meetingSourceGeneration = ResultGeneration()
    private var meetingActionGeneration = ResultGeneration()
    private var meetingVoiceGeneration = ResultGeneration()
    private var meetingPresentationRevision: UInt64 = 0
    private var meetingSessionID: String?
    private var meetingPendingPackets = 0
    private var meetingSnapshotPending = false
    private var meetingRequestID: String?
    private var meetingVoiceASRID: String?
    private let recordingIndicator = RecordingIndicatorController()
    private var indicatorSnapshot = IndicatorSnapshot()
    private var sideGate = SideVoiceGate()
    private struct SideDictationPreflight {
        let generation: UInt64
        let targetPID: Int32?
        var held = false
        var ready = false
    }
    private var sideDictationPreflight: SideDictationPreflight?
    private let credentials = KeychainCredentials()
    private let interaction = InteractionSession.macOS()
    private let dictationDelivery = MacDictationDelivery()
    private var store: SettingsStore?
    private var bridge: BridgeClient?
    private var helloGeneration = ResultGeneration()
    private var resultGeneration = ResultGeneration()
    private var probeGeneration = ResultGeneration()
    private var modelQueryCounter: UInt64 = 0
    private var modelQueryTokens: [String: UInt64] = [:]
    private var activeModelQueryIDs: [String: String] = [:]
    private var activeProbeIDs: [String: String] = [:]
    private var activeRequestID: String?
    private var activeInsert: MacDictationTarget?
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

    func refreshMicrophoneDevices() {
        microphoneDevices = MicrophoneRecorder.availableDevices()
    }

    func refreshMicrophonePermission() {
        microphonePermission = microphone.permissionStatus
    }

    func openMicrophonePrivacySettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone") else { return }
        NSWorkspace.shared.open(url)
    }

    func launch() {
        refreshMicrophonePermission()
        refreshMicrophoneDevices()
        DiagnosticLog.note("应用已启动")
        if case .disconnected = connection { connect() }
        if hotKeys == nil { registerHotKeys() }
        refreshSideMouse()
    }

    func connect() {
        if meetingActive || meetingStarting || meetingSessionID != nil { stopMeeting() }
        let token = helloGeneration.begin()
        _ = updateCheck.begin()
        isCheckingUpdate = false
        updatePage = nil
        updateStatus = updateNotChecked()
        cancelProbes()
        invalidateDiscoveredModels(.transcribe)
        invalidateDiscoveredModels(.selectedTask)
        asrProbeStatus = ServiceProbeCopy.idle
        llmProbeStatus = ServiceProbeCopy.idle
        bridge?.stop()
        bridge = nil
        connection = .connecting
        serviceStatus = helperStarting()
        guard let executable = Bundle.main.executableURL?.deletingLastPathComponent()
            .appendingPathComponent("voicebridge") else {
            applyHelperConnection(.bundleMissing)
            return
        }
        let client = BridgeClient(executableURL: executable) { [weak self] failure in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                self.applyHelperConnection(.disconnected(failure.localizedDescription))
            }
        }
        bridge = client
        do { try client.start() }
        catch {
            applyHelperConnection(.startFailed)
            return
        }
        client.send(operation: "hello") { [weak self] response in
            Task { @MainActor [weak self] in
                guard let self, self.helloGeneration.accepts(token) else { return }
                switch response {
                case .success(let reply):
                    guard helperHelloAccepted(ok: reply.ok, name: reply.result?["name"]?.string, version: reply.result?["protocol"]?.int) else {
                        self.applyHelperConnection(.protocolRejected)
                        return
                    }
                    self.applyHelperConnection(.connected)
                case .failure:
                    self.applyHelperConnection(.handshakeFailed)
                }
            }
        }
    }

    func saveSettings() {
        let plan = acceptedShortcutPlan()
        switch settingsSaveRefusal(
            busy: isRecording || isStarting || isProcessing,
            credentialTooLong: MacSettings.credentialTooLong(asrKeyInput) || MacSettings.credentialTooLong(llmKeyInput),
            planValid: plan != nil) {
        case .busy:
            settingsStatus = SettingsCopy.saveWhileBusy
            return
        case .credential:
            settingsStatus = ContractLimits.credentialTooLong
            return
        case .shortcut:
            settingsStatus = shortcutSaveRejected()
            return
        case .ready:
            break
        }
        guard let (dictation, rewrite, textEdit) = plan else { return }
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
            invalidateDiscoveredModels(.transcribe)
            invalidateDiscoveredModels(.selectedTask)
            cancelProbes()
            checkServices()
            refreshShortcutsFromCore()
        } catch {
            settingsStatus = settingsSaveFailed(error.localizedDescription)
        }
    }

    private func acceptedShortcutPlan() -> (ShortcutBinding, ShortcutBinding, ShortcutBinding)? {
        guard let dictation = ShortcutBinding(canonical: settings.dictationShortcut),
              let rewrite = ShortcutBinding(canonical: settings.rewriteShortcut),
              let textEdit = ShortcutBinding(canonical: settings.textEditShortcut),
              MacShortcutPlan(dictation: dictation, rewrite: rewrite, textEdit: textEdit) != nil else {
            return nil
        }
        return (dictation, rewrite, textEdit)
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
            guard let self else { return }
            guard shortcutRefresh(tokenValid: self.shortcutGeneration.accepts(token), parsed: dictation != nil, remaining: 2) == .parseNext, let dictation else { return }
            self.parseShortcut(values.rewrite) { [weak self] rewrite in
                guard let self else { return }
                guard shortcutRefresh(tokenValid: self.shortcutGeneration.accepts(token), parsed: rewrite != nil, remaining: 1) == .parseNext, let rewrite else { return }
                self.parseShortcut(values.textEdit) { [weak self] textEdit in
                    guard let self else { return }
                    guard shortcutRefresh(tokenValid: self.shortcutGeneration.accepts(token), parsed: textEdit != nil, remaining: 0) == .register, let textEdit else { return }
                    if shortcutRefreshStores(registered: self.register(dictation: dictation, rewrite: rewrite, textEdit: textEdit)) {
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
                    self.interactionStatus = FailureCopy.status(prefix: "快捷键无效：", message: shortcutParseFailure(transport: nil, userText: response.error?.userText))
                    done(nil)
                case .failure(let failure):
                    self.interactionStatus = FailureCopy.status(prefix: "快捷键无效：", message: shortcutParseFailure(transport: failure.localizedDescription, userText: nil))
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
            onPress: { [weak self] action, pid in
                self?.previewIndicator(action)
                self?.beginSideDictationPreflight(action: action, targetPID: pid)
            },
            onPreviewDismissed: { [weak self] in
                self?.cancelUnconfirmedSideDictation()
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
            switch sideMouseStart(fresh: isFreshStart, paused: voicePaused) {
            case .paused:
                sideMouseStatus = VoicePauseCopy.paused
            case .listening:
                sideMouseStatus = sideMouseListening()
            case .unchanged:
                break
            }
        } catch {
            driver.shutdown()
            sideMouse = nil
            sideMouseStatus = sideMouseListenFailed(error.localizedDescription)
        }
    }

    func toggleVoicePause() {
        let turn = voicePauseTurn(
            busy: isRecording || isStarting || isProcessing,
            currentlyPaused: voicePaused,
            busyStatus: VoicePauseCopy.busy,
            pausedStatus: VoicePauseCopy.paused,
            idleStatus: VoicePauseCopy.idle)
        if let status = turn.interactionStatus {
            interactionStatus = status
        }
        guard turn.toggle else { return }
        voicePaused.toggle()
        sideMouse?.setVoicePaused(voicePaused)
        dismissPreviewIndicator()
        if let status = turn.sideStatus {
            sideMouseStatus = status
        }
    }

    private func sideMouseHeld(_ action: SideMouseAction, targetPID: Int32?) {
        let promotingPreflight = action == .dictation && sideDictationPreflight != nil
        switch sideGate.hold(paused: voicePaused,
                             busy: isRecording || isProcessing || (isStarting && !promotingPreflight)) {
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
        // A speculative microphone is already running or starting. Promotion
        // keeps the first samples and does not issue a second mic start.
        if action == .dictation, var preflight = sideDictationPreflight {
            preflight.held = true
            sideDictationPreflight = preflight
            sideGate.began(action)
            if preflight.ready { promoteSideDictation(preflight) }
            return
        }
        switch action {
        case .dictation: toggleRecording(from: targetPID)
        case .rewrite: toggleVoiceSelectedText(from: targetPID)
        }
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

    /// Begin native audio on mouse down; a short browser navigation never
    /// reaches ASR. Do not prompt for mic permission on a short click.
    private func beginSideDictationPreflight(action: SideMouseAction, targetPID: Int32?) {
        guard shouldPreCaptureDictation(
            action: action, authorized: microphone.permissionStatus == .authorized,
            paused: voicePaused, meetingRunning: meetingActive || meetingStarting,
            busy: isRecording || isStarting || isProcessing ||
                meetingVoiceRecording || meetingVoiceStarting ||
                sideGate.active != nil || sideDictationPreflight != nil) else { return }
        invalidatePending()
        let generation = resultGeneration.begin()
        sideDictationPreflight = SideDictationPreflight(generation: generation, targetPID: targetPID)
        isStarting = true
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                try await self.microphone.start(deviceUID: self.settings.microphoneUID,
                                                maximumSeconds: self.settings.maxRecordSeconds)
                guard var current = self.sideDictationPreflight,
                      current.generation == generation,
                      self.resultGeneration.accepts(generation) else { return }
                current.ready = true
                self.sideDictationPreflight = current
                if current.held { self.promoteSideDictation(current) }
            } catch {
                guard let current = self.sideDictationPreflight,
                      current.generation == generation else { return }
                self.sideDictationPreflight = nil
                self.isStarting = false
                if current.held {
                    let failed = dictationMicrophoneReady(started: false, tokenValid: true,
                                                         failure: error.localizedDescription)
                    if let status = failed.recordingStatus { self.recordingStatus = status }
                    self.showIndicator(phase: .error, mode: "听写", pending: false)
                    self.stopWhenSideMouseReleased(.dictation)
                }
            }
            self.refreshMicrophonePermission()
        }
    }

    private func cancelUnconfirmedSideDictation() {
        guard let current = sideDictationPreflight, !current.held else { return }
        sideDictationPreflight = nil
        _ = resultGeneration.begin()
        microphone.cancel()
        isStarting = false
    }

    private func promoteSideDictation(_ preflight: SideDictationPreflight) {
        guard sideDictationPreflight?.generation == preflight.generation,
              preflight.ready, preflight.held,
              resultGeneration.accepts(preflight.generation) else { return }
        sideDictationPreflight = nil
        var snapshot: MacDictationTarget?
        var failure: String?
        if let pid = preflight.targetPID, pid > 0 {
            do {
                snapshot = try dictationDelivery.capture(processID: pid)
            } catch {
                DiagnosticLog.note("stage=dictation_capture status=failed code=\(insertionDiagnosticCode(error))")
                failure = captureStatus(error, password: ResultCopy.passwordDictation)
            }
        }
        let arm = dictationArm(hasTarget: (preflight.targetPID ?? 0) > 0,
                              tokenValid: true, captured: snapshot != nil,
                              captureFailure: failure)
        if let status = arm.interactionStatus { interactionStatus = status }
        if arm.keepInsert { activeInsert = snapshot }
        let ready = dictationMicrophoneReady(started: true, tokenValid: true, failure: "")
        isRecording = true
        isStarting = false
        showIndicator(phase: .recording, mode: "听写", pending: false)
        if let status = ready.recordingStatus { recordingStatus = status }
        stopWhenSideMouseReleased(.dictation)
    }

    func refreshMeetingSources() {
        guard !meetingStarting && !meetingActive else { return }
        let token = meetingSourceGeneration.begin()
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let available = try await self.systemAudio.sources()
                guard self.meetingSourceGeneration.accepts(token),
                      !self.meetingStarting && !self.meetingActive else { return }
                self.meetingSources = available
                if !available.contains(where: { $0.id == self.meetingSourceID }) {
                    self.meetingStatus = "所选音频来源已经退出；请重新选择或改用系统音频"
                }
            } catch {
                guard self.meetingSourceGeneration.accepts(token),
                      !self.meetingStarting && !self.meetingActive else { return }
                self.meetingStatus = "无法读取音频来源；请在系统设置中允许屏幕与系统音频录制后重试"
            }
        }
    }

    func startMeeting() {
        guard !meetingActive && !meetingStarting else { return }
        guard meetingSources.contains(where: { $0.id == meetingSourceID }) else {
            meetingStatus = "所选音频来源已不可用；请刷新后重新选择"
            return
        }
        _ = meetingSourceGeneration.begin()
        guard let bridge, case .connected = connection else {
            meetingStatus = "请先连接 Go Helper"
            return
        }
        let config: JSONValue
        do {
            guard case .object(var fields) = try requestConfig(for: .spokenSelectedTask) else {
                throw BridgeFailure.invalidRequest
            }
            // Meeting mode follows the same saved Go Core configuration
            // as Windows; no macOS-only recognizer behavior is added.
            fields["meeting_asr_mode"] = .string(settings.meetingASRMode)
            config = .object(fields)
        } catch {
            meetingStatus = "请保存有效的 ASR/LLM 设置后重试"
            return
        }
        let token = meetingGeneration.begin()
        _ = meetingActionGeneration.begin()
        _ = meetingVoiceGeneration.begin()
        meetingPresentationRevision &+= 1
        meetingCancelPending = false
        meetingStarting = true
        meetingStatus = "正在准备会议识别…"
        meetingCaptions = []
        meetingCaptionsOnly = false
        meetingExpressionExpanded = false
        meetingReplies = []
        meetingReplyPending = false
        meetingReplyStatus = ""
        meetingReplyTip = ""
        meetingReplySource = ""
        meetingExpression = ""
        meetingExpressionChinese = ""
        meetingExpressionStatus = ""
        meetingCopyStatus = ""
        meetingSelectedIndex = 0
        bridge.send(operation: "meeting_start", fields: ["config": config]) { [weak self] reply in
            Task { @MainActor [weak self] in
                guard let self else { return }
                let result = try? reply.get()
                guard result?.ok == true, let id = result?.result?["session_id"]?.string else {
                    guard self.meetingGeneration.accepts(token) else { return }
                    self.meetingStarting = false
                    self.meetingStatus = "无法启动会议处理，请检查本地 Helper"
                    return
                }
                guard self.meetingGeneration.accepts(token) else {
                    bridge.send(operation: "meeting_stop", fields: ["session_id": .string(id)]) { _ in }
                    return
                }
                self.meetingSessionID = id
                do {
                    try await self.systemAudio.start(sourceID: self.meetingSourceID,
                        onPCM: { [weak self] bytes in self?.feedMeeting(bytes, sessionID: id) },
                        onFailure: { [weak self] detail in self?.stopMeeting(reason: detail) })
                    guard self.meetingGeneration.accepts(token) else {
                        await self.systemAudio.stop()
                        return
                    }
                    self.meetingStarting = false
                    self.meetingActive = true
                    self.meetingStatus = "正在监听所选应用音频"
                    self.pollMeeting(id: id, token: token)
                } catch {
                    let detail = (error as? SystemAudioError)?.localizedDescription
                        ?? "无法启动系统音频；请检查屏幕与系统音频录制权限及来源"
                    self.stopMeeting(reason: detail)
                }
            }
        }
    }

    private func feedMeeting(_ bytes: Data, sessionID id: String) {
        guard meetingSessionID == id, meetingActive, let bridge else { return }
        guard meetingPendingPackets < 8 else {
            stopMeeting(reason: "会议音频处理过载，采集已停止；请检查电脑负载")
            return
        }
        meetingPendingPackets += 1
        bridge.send(operation: "meeting_feed", fields: [
            "session_id": .string(id),
            "pcm_base64": .string(bytes.base64EncodedString()),
        ]) { [weak self] reply in
            Task { @MainActor [weak self] in
                guard let self, self.meetingSessionID == id else { return }
                self.meetingPendingPackets = max(0, self.meetingPendingPackets - 1)
                guard case .success(let response) = reply, response.ok else {
                    self.stopMeeting(reason: "会议音频传输中断，已停止采集")
                    return
                }
            }
        }
    }

    private func pollMeeting(id: String, token: UInt64) {
        Task { @MainActor [weak self] in
            while let self, self.meetingSessionID == id,
                  self.meetingGeneration.accepts(token), self.meetingActive {
                guard !self.meetingSnapshotPending else {
                    try? await Task.sleep(nanoseconds: 450_000_000)
                    continue
                }
                self.meetingSnapshotPending = true
                let revision = self.meetingPresentationRevision
                self.bridge?.send(operation: "meeting_snapshot", fields: [
                    "session_id": .string(id),
                ]) { [weak self] reply in
                    Task { @MainActor [weak self] in
                        guard let self, self.meetingSessionID == id,
                              self.meetingGeneration.accepts(token) else { return }
                        self.meetingSnapshotPending = false
                        guard revision == self.meetingPresentationRevision,
                              !self.meetingCancelPending else { return }
                        guard case .success(let response) = reply, response.ok else {
                            self.stopMeeting(reason: "会议处理已断开")
                            return
                        }
                        self.applyMeetingSnapshot(response.result ?? [:])
                        if case .array(let values)? = response.result?["captions"] {
                            self.meetingCaptions = values.compactMap { value in
                                guard case .object(let item) = value,
                                      let id = item["id"]?.int,
                                      let source = item["source"]?.string,
                                      let chinese = item["chinese"]?.string else { return nil }
                                return MeetingCaptionRow(id: id, source: source,
                                    chinese: chinese, final: item["final"]?.boolValue == true)
                            }
                        }
                    }
                }
                try? await Task.sleep(nanoseconds: 450_000_000)
            }
        }
    }

    private func applyMeetingSnapshot(_ values: [String: JSONValue]) {
        if let status = values["status"]?.string { meetingStatus = status }
        if let status = values["reply_status"]?.string { meetingReplyStatus = status }
        if let status = values["expression_status"]?.string { meetingExpressionStatus = status }
        if let value = values["expression"]?.string { meetingExpression = value }
        if let value = values["expression_chinese"]?.string { meetingExpressionChinese = value }
        if let source = values["reply_source"]?.string { meetingReplySource = source }
        if let tip = values["reply_tip"]?.string { meetingReplyTip = tip }
        meetingReplyPending = values["reply_pending"]?.boolValue == true
        if let captionsOnly = values["captions_only"]?.boolValue {
            meetingCaptionsOnly = captionsOnly
        }
        if let expanded = values["expression_expanded"]?.boolValue {
            meetingExpressionExpanded = expanded
        }
        if case .array(let items)? = values["replies"] {
            meetingReplies = items.enumerated().compactMap { index, value in
                guard case .object(let row) = value,
                      let english = row["english"]?.string,
                      let chinese = row["chinese"]?.string else { return nil }
                return MeetingReplyRow(id: index, english: english,
                    chinese: chinese, complete: row["complete"]?.boolValue == true)
            }
            // The core selects the first finished option, not necessarily ONE.
            // Preserve an explicit selection while it remains copyable.
            let selectedReady = meetingReplies.contains {
                $0.id == meetingSelectedIndex && $0.complete && !$0.english.isEmpty
            }
            if !selectedReady, let suggested = values["reply_first_ready"]?.int, suggested >= 0 {
                meetingSelectedIndex = suggested
            }
        }
    }

    private func meetingAction(_ operation: String, fields: [String: JSONValue] = [:]) {
        guard meetingActive, !meetingBusy, !meetingCancelPending,
              let id = meetingSessionID, let bridge else { return }
        meetingBusy = true
        let actionToken = meetingActionGeneration.begin()
        meetingPresentationRevision &+= 1
        let token = meetingGeneration.latest
        var payload = fields
        payload["session_id"] = .string(id)
        meetingRequestID = bridge.send(operation: operation, fields: payload) { [weak self] result in
            Task { @MainActor [weak self] in
                guard let self, self.meetingSessionID == id,
                      self.meetingGeneration.accepts(token),
                      self.meetingActionGeneration.accepts(actionToken) else { return }
                self.meetingRequestID = nil
                self.meetingBusy = false
                self.meetingPresentationRevision &+= 1
                switch result {
                case .success(let response) where response.ok:
                    self.applyMeetingSnapshot(response.result ?? [:])
                case .success(let response):
                    let message = response.error?.userText ?? "会议处理失败，请重试"
                    if operation == "meeting_replies" {
                        self.meetingReplyStatus = message
                    } else if operation == "meeting_view" {
                        self.meetingStatus = message
                    } else {
                        self.meetingExpressionStatus = message
                    }
                case .failure:
                    if operation == "meeting_replies" {
                        self.meetingReplyStatus = "会议建议请求已取消或连接已断开"
                    } else if operation == "meeting_view" {
                        self.meetingStatus = "会议布局切换失败；请检查 Helper 连接"
                    } else {
                        self.meetingExpressionStatus = "会议处理已取消或连接已断开"
                    }
                }
            }
        }
    }

    func requestMeetingReplies() {
        meetingAction("meeting_replies")
    }

    func toggleMeetingCaptionsOnly() {
        meetingAction("meeting_view", fields: [
            "button_id": .number(Double(ContractLimits.meetingButtonCaptions)),
        ])
    }

    func toggleMeetingExpressionExpanded() {
        meetingAction("meeting_view", fields: [
            "button_id": .number(Double(ContractLimits.meetingButtonExpression)),
        ])
    }

    func acceptMeetingReplies() {
        guard meetingActive, !meetingBusy, !meetingCancelPending, meetingReplyPending,
              let id = meetingSessionID, let bridge else { return }
        let token = meetingGeneration.latest
        let actionToken = meetingActionGeneration.begin()
        meetingPresentationRevision &+= 1
        meetingBusy = true
        meetingRequestID = bridge.send(operation: "meeting_accept",
                                       fields: ["session_id": .string(id)]) { [weak self] result in
            Task { @MainActor [weak self] in
                guard let self, self.meetingSessionID == id,
                      self.meetingGeneration.accepts(token),
                      self.meetingActionGeneration.accepts(actionToken) else { return }
                self.meetingRequestID = nil
                self.meetingBusy = false
                self.meetingPresentationRevision &+= 1
                if case .success(let response) = result, response.ok {
                    self.applyMeetingSnapshot(response.result ?? [:])
                } else {
                    self.meetingReplyStatus = "切换回复失败，请重试"
                }
            }
        }
    }

    func refineMeetingReply() {
        guard meetingReplies.contains(where: {
            $0.id == meetingSelectedIndex && $0.complete && !$0.english.isEmpty
        }) else { return }
        let instruction = meetingRefineInput.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !instruction.isEmpty else { return }
        meetingAction("meeting_refine", fields: [
            "reply_index": .number(Double(meetingSelectedIndex)),
            "instruction": .string(instruction),
        ])
    }

    func composeMeetingText() {
        let input = meetingInput.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !input.isEmpty else { return }
        meetingAction("meeting_compose", fields: ["instruction": .string(input)])
    }

    func toggleMeetingVoice() {
        if meetingVoiceRecording {
            meetingVoiceRecording = false
            meetingBusy = true
            let token = meetingGeneration.latest
            let voiceToken = meetingVoiceGeneration.latest
            let id = meetingSessionID
            meetingExpressionStatus = "正在识别你的语音…"
            Task { @MainActor [weak self] in
                guard let self else { return }
                do {
                    let pcm = try await self.microphone.stop()
                    guard self.meetingGeneration.accepts(token),
                          self.meetingVoiceGeneration.accepts(voiceToken),
                          self.meetingSessionID == id, let bridge = self.bridge else { return }
                    let config = try self.requestConfig(for: .transcribe)
                    self.meetingVoiceASRID = bridge.send(operation: "transcribe", fields: [
                        "config": config,
                        "pcm_base64": .string(pcm.base64EncodedString()),
                    ]) { [weak self] result in
                        Task { @MainActor [weak self] in
                            guard let self, self.meetingGeneration.accepts(token),
                                  self.meetingVoiceGeneration.accepts(voiceToken),
                                  self.meetingSessionID == id else { return }
                            self.meetingVoiceASRID = nil
                            self.meetingBusy = false
                            guard case .success(let response) = result, response.ok,
                                  let words = response.result?["text"]?.string,
                                  !words.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                                self.meetingExpressionStatus = "语音识别失败或未听到有效内容"
                                return
                            }
                            self.meetingInput = words
                            self.composeMeetingText()
                        }
                    }
                } catch {
                    guard self.meetingGeneration.accepts(token),
                          self.meetingVoiceGeneration.accepts(voiceToken),
                          self.meetingSessionID == id else { return }
                    self.meetingBusy = false
                    self.meetingExpressionStatus = (error as? MicrophoneError)?.localizedDescription
                        ?? "麦克风录音失败，请检查输入设备与权限"
                }
            }
            return
        }
        guard meetingActive, !meetingBusy, !meetingCancelPending, !meetingVoiceStarting,
              !isRecording, !isStarting, !isProcessing else { return }
        meetingVoiceStarting = true
        meetingExpressionStatus = "正在启动麦克风…"
        let token = meetingGeneration.latest
        let voiceToken = meetingVoiceGeneration.begin()
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                try await self.microphone.start(deviceUID: self.settings.microphoneUID,
                                               maximumSeconds: self.settings.maxRecordSeconds)
                guard self.meetingGeneration.accepts(token),
                      self.meetingVoiceGeneration.accepts(voiceToken), self.meetingActive else {
                    // Stop/cancel already revoked this capture. A late start
                    // completion must not cancel a newer recording.
                    return
                }
                self.meetingVoiceRecording = true
                self.meetingExpressionStatus = "正在录制想表达的内容；再次点击结束"
            } catch {
                if self.meetingGeneration.accepts(token),
                   self.meetingVoiceGeneration.accepts(voiceToken) {
                    self.meetingExpressionStatus = (error as? MicrophoneError)?.localizedDescription
                        ?? "无法启动麦克风，请检查权限与设备"
                }
            }
            if self.meetingVoiceGeneration.accepts(voiceToken) {
                self.meetingVoiceStarting = false
                self.refreshMicrophonePermission()
            }
        }
    }

    func cancelMeetingWork() {
        guard meetingActive, !meetingCancelPending,
              let id = meetingSessionID, let bridge else { return }
        let token = meetingGeneration.latest
        let actionToken = meetingActionGeneration.begin()
        _ = meetingVoiceGeneration.begin()
        meetingPresentationRevision &+= 1
        if meetingVoiceRecording || meetingVoiceStarting {
            microphone.cancel()
            meetingVoiceRecording = false
            meetingVoiceStarting = false
        }
        if let requestID = meetingVoiceASRID { bridge.cancel(id: requestID) }
        meetingVoiceASRID = nil
        if let requestID = meetingRequestID { bridge.cancel(id: requestID) }
        meetingRequestID = nil
        // Do not admit new work or apply a polled snapshot until the helper
        // acknowledges cancellation. Otherwise a rapid regenerate can race
        // with the previous cancel and revoke the fresh request.
        meetingCancelPending = true
        meetingBusy = true
        meetingExpressionStatus = "正在取消处理…"
        meetingRequestID = bridge.send(operation: "meeting_cancel",
                                       fields: ["session_id": .string(id)]) { [weak self] result in
            Task { @MainActor [weak self] in
                guard let self, self.meetingSessionID == id,
                      self.meetingGeneration.accepts(token),
                      self.meetingActionGeneration.accepts(actionToken) else { return }
                self.meetingRequestID = nil
                self.meetingCancelPending = false
                self.meetingBusy = false
                self.meetingPresentationRevision &+= 1
                if case .success(let response) = result, response.ok {
                    self.applyMeetingSnapshot(response.result ?? [:])
                } else {
                    self.meetingExpressionStatus = "取消请求未确认；请检查 Helper 连接后重试"
                }
            }
        }
    }

    func copyMeetingReply() {
        guard let row = meetingReplies.first(where: { $0.id == meetingSelectedIndex }),
              row.complete, !row.english.isEmpty else { return }
        NSPasteboard.general.clearContents()
        let copied = NSPasteboard.general.setString(row.english, forType: .string)
        meetingCopyStatus = copied ? ResultCopy.copySucceeded : ResultCopy.copyFailed
    }

    func copyMeetingExpression() {
        guard !meetingExpression.isEmpty else { return }
        NSPasteboard.general.clearContents()
        let copied = NSPasteboard.general.setString(meetingExpression, forType: .string)
        meetingCopyStatus = copied ? ResultCopy.copySucceeded : ResultCopy.copyFailed
    }

    func stopMeeting(reason: String = "会议采集已停止") {
        _ = meetingGeneration.begin()
        _ = meetingActionGeneration.begin()
        _ = meetingVoiceGeneration.begin()
        meetingPresentationRevision &+= 1
        if meetingVoiceRecording || meetingVoiceStarting { microphone.cancel() }
        meetingVoiceRecording = false
        meetingVoiceStarting = false
        if let id = meetingVoiceASRID { bridge?.cancel(id: id) }
        meetingVoiceASRID = nil
        if let id = meetingRequestID { bridge?.cancel(id: id) }
        meetingRequestID = nil
        meetingBusy = false
        meetingCancelPending = false
        meetingSnapshotPending = false
        meetingStarting = false
        meetingActive = false
        meetingPendingPackets = 0
        meetingStatus = reason
        let id = meetingSessionID
        meetingSessionID = nil
        Task { await systemAudio.stop() }
        if let id {
            bridge?.send(operation: "meeting_stop", fields: [
                "session_id": .string(id),
            ]) { _ in }
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

    private func modelKind(_ operation: HelperOperation) -> String? {
        switch operation {
        case .transcribe: return "asr"
        case .selectedTask: return "llm"
        case .spokenSelectedTask: return nil
        }
    }

    // Provider URL or credential edits invalidate outstanding query results.
    // Selecting a model leaves the choices visible.
    func invalidateDiscoveredModels(_ operation: HelperOperation) {
        guard let kind = modelKind(operation) else { return }
        modelQueryCounter &+= 1
        modelQueryTokens[kind] = modelQueryCounter
        if let id = activeModelQueryIDs.removeValue(forKey: kind) {
            bridge?.cancel(id: id)
        }
        if kind == "asr" {
            asrAvailableModels = []
            asrModelDiscoveryStatus = ""
            isDiscoveringASRModels = false
        } else {
            llmAvailableModels = []
            llmModelDiscoveryStatus = ""
            isDiscoveringLLMModels = false
        }
    }

    func discoverModels(for operation: HelperOperation) {
        guard let kind = modelKind(operation) else { return }
        invalidateDiscoveredModels(operation)
        let token = modelQueryTokens[kind]
        guard let bridge, case .connected = connection else {
            setModelList(kind, models: nil, status: "请先连接 Go Helper")
            return
        }
        let config: JSONValue
        do {
            config = try requestConfig(for: operation)
        } catch {
            setModelList(kind, models: nil, status: "请先保存有效的服务地址与 API Key")
            return
        }
        setModelList(kind, models: nil, status: "正在读取模型列表…", loading: true)
        let id = bridge.send(operation: "list_\(kind)_models", fields: ["config": config]) { [weak self] reply in
            Task { @MainActor [weak self] in
                guard let self, self.modelQueryTokens[kind] == token else { return }
                self.activeModelQueryIDs.removeValue(forKey: kind)
                switch reply {
                case .failure(let failure):
                    self.setModelList(kind, models: nil, status: failure.localizedDescription)
                case .success(let response) where response.ok:
                    guard case .array(let values)? = response.result?["models"],
                          values.allSatisfy({ $0.string != nil }) else {
                        self.setModelList(kind, models: nil, status: "模型列表响应无效")
                        return
                    }
                    let models = values.compactMap(\.string)
                    let truncated = response.result?["truncated"]?.boolValue == true
                    let status: String
                    if models.isEmpty {
                        status = "服务未列出模型，可继续手动输入模型 ID"
                    } else if truncated {
                        status = "显示前 \(models.count) 个模型；其他 ID 可手填，选择后需保存设置"
                    } else {
                        status = "已发现 \(models.count) 个模型；选择后仍需保存设置"
                    }
                    self.setModelList(kind, models: models, status: status)
                case .success(let response):
                    self.setModelList(kind, models: nil,
                        status: response.error?.userText ?? "读取模型列表失败")
                }
            }
        }
        activeModelQueryIDs[kind] = id
    }

    private func setModelList(_ kind: String, models: [String]?, status: String,
                              loading: Bool = false) {
        if kind == "asr" {
            if let models { asrAvailableModels = models }
            asrModelDiscoveryStatus = status
            isDiscoveringASRModels = loading
        } else {
            if let models { llmAvailableModels = models }
            llmModelDiscoveryStatus = status
            isDiscoveringLLMModels = loading
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
    func checkForUpdate() {
        guard !isCheckingUpdate else { return }
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
                let follow: UpdateFollowUp
                switch reply {
                case .success(let response):
                    follow = updateFollowUp(
                        ok: response.ok,
                        summary: response.result?["summary"]?.string,
                        newer: response.result?["newer"]?.boolValue == true,
                        page: response.result?["page"]?.string,
                        errorText: response.error?.userText,
                        transport: nil)
                case .failure(let failure):
                    follow = updateFollowUp(ok: false, summary: nil, newer: false, page: nil, errorText: nil, transport: failure.localizedDescription)
                }
                self.updateStatus = follow.status
                self.updatePage = follow.page
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
            guard let request = probeRequest(operation) else { continue }
            let name = request.name
            let bridgeOperation = request.operation
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
        switch probeField(operation) {
        case .asr:
            asrProbeStatus = message
        case .llm:
            llmProbeStatus = message
        case .both:
            asrProbeStatus = message
            llmProbeStatus = message
        }
    }

    private func invalidatePending() {
        _ = resultGeneration.begin()
        if let id = activeRequestID { bridge?.cancel(id: id) }
        activeRequestID = nil
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
        guard !meetingVoiceRecording && !meetingVoiceStarting else { return }
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
            defer { self.refreshMicrophonePermission() }
            var generation: UInt64?
            do {
                let next = try await interaction.beginTask(targetProcessID: pid)
                generation = next
                let snapshot = try await interaction.captureSelection(
                    generation: next, intent: .replaceSelection)
                let captured = rewriteVoiceAfterCapture(
                    tokenValid: resultGeneration.accepts(token),
                    tooLong: SelectionLimit.tooLongMessage(selected: snapshot.originalSelectedText))
                if captured.cancelTask || captured.cancelMicrophone {
                    await cancelSelectionCue(captured, generation: next)
                }
                presentSelectionCue(captured)
                if captured.leave {
                    isStarting = false
                    return
                }
                if captured.startMicrophone {
                    try await microphone.start(deviceUID: settings.microphoneUID,
                                               maximumSeconds: settings.maxRecordSeconds)
                    let ready = rewriteVoiceMicrophoneReady(tokenValid: resultGeneration.accepts(token))
                    if ready.beginRecording {
                        activeVoiceSelection = (snapshot, next)
                        isVoiceSelectionRecording = true
                        isRecording = true
                        showIndicator(phase: .recording, mode: "改写", pending: false)
                    }
                    if ready.cancelTask || ready.cancelMicrophone {
                        await cancelSelectionCue(ready, generation: next)
                    }
                    presentSelectionCue(ready)
                    if ready.leave {
                        isStarting = false
                        return
                    }
                }
            } catch {
                let failed = rewriteVoiceCaptureFailed(
                    tokenValid: resultGeneration.accepts(token),
                    status: rewriteCaptureStatus(error, password: ResultCopy.passwordSelection))
                await cancelSelectionCue(failed, generation: generation)
                presentSelectionCue(failed)
            }
            isStarting = false
            stopWhenSideMouseReleased(.rewrite)
        }
    }

    private func startRecording(targetPID: Int32?) {
        guard !meetingVoiceRecording && !meetingVoiceStarting else { return }
        invalidatePending()
        let token = resultGeneration.begin()
        isStarting = true
        recordingStatus = dictationRequestingMicrophone()
        Task { @MainActor [weak self] in
            guard let self else { return }
            defer { self.refreshMicrophonePermission() }
            var captured = false
            var failure: String?
            var snapshot: MacDictationTarget?
            if let pid = targetPID, pid > 0 {
                do {
                    snapshot = try dictationDelivery.capture(processID: pid)
                    captured = true
                } catch {
                    DiagnosticLog.note("stage=dictation_capture status=failed code=\(insertionDiagnosticCode(error))")
                    failure = captureStatus(error, password: ResultCopy.passwordDictation)
                }
            }
            let arm = dictationArm(hasTarget: (targetPID ?? 0) > 0, tokenValid: resultGeneration.accepts(token), captured: captured, captureFailure: failure)
            if let status = arm.interactionStatus { interactionStatus = status }
            if arm.keepInsert, let snapshot {
                activeInsert = snapshot
            }
            guard arm.startMicrophone else { return }
            do {
                try await microphone.start(deviceUID: settings.microphoneUID,
                                               maximumSeconds: settings.maxRecordSeconds)
                let ready = dictationMicrophoneReady(started: true, tokenValid: resultGeneration.accepts(token), failure: "")
                if ready.cancelMicrophone {
                    microphone.cancel()
                    return
                }
                isRecording = true
                showIndicator(phase: .recording, mode: "听写", pending: false)
                if let status = ready.recordingStatus { recordingStatus = status }
            } catch {
                let ready = dictationMicrophoneReady(started: false, tokenValid: true, failure: error.localizedDescription)
                if let status = ready.recordingStatus { recordingStatus = status }
                if ready.showError {
                    showIndicator(phase: .error, mode: "听写", pending: false)
                }
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
                let route = recordingDispatch(tokenValid: resultGeneration.accepts(token), voiceSelection: voiceSelection != nil)
                guard route.send else { return }
                if route.cancelProbes { cancelProbes() }
                if let status = route.asrProbe { asrProbeStatus = status }
                if let status = route.llmProbe { llmProbeStatus = status }
                if route.spoken, let voiceSelection {
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
                let failed = recordingStopFailure(tokenValid: resultGeneration.accepts(token))
                guard failed.apply else { return }
                if failed.stopProcessing { isProcessing = false }
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
                let step = typedSelectionAfterCapture(
                    tokenValid: resultGeneration.accepts(token),
                    tooLong: SelectionLimit.tooLongMessage(selected: snapshot.originalSelectedText))
                if step.cancelTask || step.cancelMicrophone {
                    await cancelSelectionCue(step, generation: generation)
                }
                presentSelectionCue(step)
                guard step.sendTask else { return }
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
                let failed = typedSelectionStartFailed(
                    tokenValid: resultGeneration.accepts(token),
                    detail: error.localizedDescription)
                presentSelectionCue(failed)
            }
        }
    }

    /// The microphone is canceled before the Accessibility task.
    private func cancelSelectionCue(_ cue: SelectionCue, generation: UInt64?) async {
        if cue.cancelMicrophone {
            microphone.cancel()
        }
        if cue.cancelTask, let generation {
            await interaction.cancelTask(generation: generation)
        }
    }

    private func presentSelectionCue(_ cue: SelectionCue) {
        if cue.stopProcessing {
            isProcessing = false
        }
        if let status = cue.interactionStatus {
            interactionStatus = status
        }
        if let status = cue.recordingStatus {
            recordingStatus = status
        }
        if let probe = cue.llmProbe {
            llmProbeStatus = probe
        }
        if cue.showError {
            showIndicator(phase: .error, mode: "改写", pending: false)
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

    private func applyHelperConnection(_ event: HelperConnection) {
        let effect = helperConnectionEffect(event, helperName: ContractLimits.helperName, helperVersion: ContractLimits.protocolVersion)
        if let label = effect.connectedLabel {
            connection = .connected(label)
        } else if let detail = effect.failedDetail {
            connection = .failed(detail)
        }
        if let status = effect.serviceStatus {
            serviceStatus = status
        }
        if effect.cancelProbes {
            cancelProbes()
        }
        if let probe = effect.probeStatus {
            asrProbeStatus = probe
            llmProbeStatus = probe
        }
        if effect.invalidatePending {
            invalidatePending()
            invalidateDiscoveredModels(.transcribe)
            invalidateDiscoveredModels(.selectedTask)
        }
        if effect.logServiceStatus {
            DiagnosticLog.note(serviceStatus)
        }
        if effect.checkServices {
            checkServices()
        }
        if effect.refreshShortcuts {
            refreshShortcutsFromCore()
        }
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
        insertion: MacDictationTarget? = nil,
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
                    let outcome = try await self.dictationDelivery.deliver(text, to: insertion, stillValid: {
                        self.resultGeneration.accepts(token)
                    })
                    guard self.resultGeneration.accepts(token) else { return }
                    DiagnosticLog.note("stage=dictation_delivery status=\(outcome)")
                    self.applyTask(dictationInsertFollowUp(
                        succeeded: !outcome.requiresManualResult,
                        status: "", manualTitle: ResultCopy.dictationWriteUnconfirmed, text: text))
                } catch {
                    guard self.resultGeneration.accepts(token) else { return }
                    DiagnosticLog.note("stage=dictation_delivery status=failed code=\(insertionDiagnosticCode(error))")
                    self.applyTask(dictationInsertFollowUp(
                        succeeded: false,
                        status: insertionStatus(error, rewrite: false, passwordRewrite: ResultCopy.passwordRewrite, passwordDictation: ResultCopy.passwordDictation),
                        manualTitle: ((error as? InteractionFailure) == .writeNotConfirmed || error is ClipboardFailure)
                            ? ResultCopy.dictationWriteUnconfirmed : ResultCopy.dictationCopyManually,
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
                    guard self.resultGeneration.accepts(token) else { return }
                    self.applyTask(selectedWriteFollowUp(succeeded: true, status: "", cardTitle: title, text: text))
                } catch {
                    guard self.resultGeneration.accepts(token) else { return }
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
        sideDictationPreflight = nil
        invalidatePending()
        microphone.cancel()
        isRecording = false
        isStarting = false
        isProcessing = false
        recordingStatus = rewriteCanceled()
        interactionStatus = rewriteCanceledDetail()
    }

    private func previewIndicator(_ action: SideMouseAction) {
        switch sidePreview(recording: isRecording, starting: isStarting, processing: isProcessing, dictation: action == .dictation) {
        case .hidden:
            return
        case .dictation:
            showIndicator(phase: .recording, mode: "听写", pending: true)
        case .rewrite:
            showIndicator(phase: .recording, mode: "改写", pending: true)
        }
    }

    private func dismissPreviewIndicator() {
        let step = previewDismiss(pending: indicatorSnapshot.pending)
        if step.allowNextHold {
            sideGate.allowNextHold()
        }
        if step.dismissIndicator {
            dismissIndicator()
        }
    }

    private func showIndicator(phase: IndicatorPhase, mode: String, pending: Bool) {
        let now = Date()
        let timing = indicatorTiming(wasVisible: indicatorSnapshot.visible, wasPending: indicatorSnapshot.pending, wasPhase: indicatorSnapshot.phase.rawValue, pending: pending, phase: phase.rawValue, now: now, started: indicatorSnapshot.started, changed: indicatorSnapshot.changed)
        indicatorSnapshot.started = timing.started
        indicatorSnapshot.changed = timing.changed
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
                self.cancelUnconfirmedSideDictation()
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
