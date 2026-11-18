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

    private enum MeetingVoiceIntent {
        case express
        case compose

        var operation: String {
            self == .express ? "meeting_express" : "meeting_compose"
        }
    }

    @Published var settings: MacSettings = .defaults
    @Published var asrKeyInput = ""
    @Published var llmKeyInput = ""
    @Published var taskInstruction = ""
    @Published var useTypedRewriteInstruction = false
    @Published private(set) var connection: Connection = .disconnected
    @Published private(set) var recordingStatus = recordingIdle()
    @Published private(set) var microphonePermission: MicrophonePermissionStatus = .notDetermined
    @Published private(set) var microphoneDevices: [MicrophoneInputDevice] = []
    @Published private(set) var keyboardDevices: [MacKeyboardDevice] = []
    @Published private(set) var keyboardSourceStatus = ""
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
    @Published private(set) var menuIndicatorPhase: IndicatorPhase?
    @Published private(set) var menuIndicatorMode = "听写"
    var onUpdateCheckCompleted: ((String, String?) -> Void)?
    @Published private(set) var meetingSources: [MeetingAudioSource] = []
    @Published var meetingSourceID = "system"
    @Published private(set) var meetingStatus = "选择音频来源后启动会议"
    @Published private(set) var meetingCaptions: [MeetingCaptionRow] = []
    @Published private(set) var meetingHistoryTrimmed = 0
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
    @Published private(set) var meetingExpressionSource = ""
    @Published private(set) var meetingExpressionStatus = ""
    @Published private(set) var meetingCopyStatus = ""
    @Published private(set) var meetingBusy = false
    @Published private(set) var meetingCancelPending = false
    @Published private(set) var meetingVoiceRecording = false
    @Published private(set) var meetingVoiceStarting = false
    @Published private(set) var meetingCapturePaused = false
    @Published private(set) var meetingCaptureSwitching = false
    @Published private(set) var meetingProfileUpdating = false
    @Published private(set) var meetingProfileVersion = 0
    @Published private(set) var meetingDemoActive = false
    @Published var meetingSelectedIndex = 0
    @Published var meetingInput = ""
    @Published var meetingRefineInput = ""
    private var historyStore = SessionHistory()
    private var updateCheck = ResultGeneration()

    private let microphone: any VoiceRecordingDevice
    private let systemAudio = SystemAudioCapture()
    private var meetingGeneration = ResultGeneration()
    private var meetingCaptureGeneration = ResultGeneration()
    private var meetingAudioEpoch = 0
    private var meetingSourceGeneration = ResultGeneration()
    private var meetingActionGeneration = ResultGeneration()
    private var meetingVoiceGeneration = ResultGeneration()
    private var meetingPresentationRevision: UInt64 = 0
    private var meetingSessionID: String?
    private var showStartupDemoOnConnect = false
    private var meetingPendingPackets = 0
    private var meetingSnapshotPending = false
    private var meetingSnapshotRequestID: String?
    private var meetingStartWatchdog: Task<Void, Never>?
    private var meetingSnapshotStartedAt: TimeInterval = 0
    private var meetingRequestID: String?
    private var meetingCaptureSourceID = ""
    private var meetingVoiceASRID: String?
    private var meetingVoiceWatchdog: Task<Void, Never>?
    private var meetingVoiceIntent: MeetingVoiceIntent = .compose
    private var meetingSideAction: SideMouseAction?
    private var meetingSideReleasedDuringStart = false
    private let recordingIndicator = RecordingIndicatorController()
    private var indicatorSnapshot = IndicatorSnapshot()

    /// Production user-visible journey timings, independent of CoreAudio/ASR
    /// implementations. Monotonic timestamps also cover real user hardware,
    /// which a hosted CI runner cannot faithfully emulate.
    private struct DictationJourney {
        let triggeredAt: TimeInterval
        var releasedAt: TimeInterval?
        var indicatorLogged = false
    }
    private var dictationJourney: DictationJourney?

    private var sideGate = SideVoiceGate()
    private var keyboardGate = KeyboardHoldGate()
    private var keyboardHoldSequence: UInt64 = 0
    /// Keyboard defaults to Windows' hold-and-release operation. Toggle mode
    /// remains available as a native preference for a future Settings control.
    @Published private(set) var keyboardHoldEnabled = true
    private struct SideDictationPreflight {
        let generation: UInt64
        let targetPID: Int32?
        var held = false
        var ready = false
    }
    private var sideDictationPreflight: SideDictationPreflight?
    private let credentials = KeychainCredentials()
    private let interaction = InteractionSession.macOS()
    private let dictationDelivery: any DictationDelivering
    private let helperExecutableURL: URL?
    private var store: SettingsStore?
    private var bridge: BridgeClient?
    private var helperStartupWatchdog: Task<Void, Never>?
    static let helperStartupTimeoutSeconds: UInt64 = 15
    private var helloGeneration = ResultGeneration()
    private var resultGeneration = ResultGeneration()
    private var probeGeneration = ResultGeneration()
    private var modelQueryCounter: UInt64 = 0
    private var modelQueryTokens: [String: UInt64] = [:]
    private var activeModelQueryIDs: [String: String] = [:]
    private var activeProbeIDs: [String: String] = [:]
    private var activeRequestID: String?
    private var processingWatchdog: Task<Void, Never>?
    private var activeInsert: MacDictationTarget?
    private var activeVoiceSelection: (snapshot: SelectionSnapshot, generation: UInt64)?
    private var activeSelectionGeneration: UInt64?
    private var activeVoiceTargetPID: Int32?
    private var hotKeyDriver: CarbonHotKeyDriver?
    private var rightModifierDriver: MacModifierHoldDriver?
    private var keyboardHID: MacKeyboardHIDMonitor?
    private var activeDictationKeyboardID = ""
    private var activeRewriteKeyboardID = ""
    private var shortcutCaptureActive = false
    private var hotKeys: HotKeyRegistry?
    private var sideMouse: SideMouseButtonDriver?

    private var activeShortcutPlan: MacShortcutPlan?
    private var shortcutGeneration = ResultGeneration()

    init(
        microphone: (any VoiceRecordingDevice)? = nil,
        dictationDelivery: (any DictationDelivering)? = nil,
        helperExecutableURL: URL? = nil
    ) {
        // Default arguments are evaluated outside actor isolation in Swift 5;
        // instantiate AppKit-bound dependencies inside this @MainActor init.
        self.microphone = microphone ?? MicrophoneRecorder()
        self.dictationDelivery = dictationDelivery ?? MacDictationDelivery()
        self.helperExecutableURL = helperExecutableURL
        do {
            let s = try SettingsStore()
            settings = try s.load()
            store = s
            settingsStatus = settingsLoaded()
        } catch {
            settingsStatus = settingsLoadFailed()
        }
    }

    private var microphoneDiscoveryGeneration: UInt64 = 0

    func refreshMicrophoneDevices() {
        // Enumerating CoreAudio HAL devices is synchronous and can stall for
        // seconds when a USB/Bluetooth interface is reconnecting. Keep the
        // settings window and keyboard event tap responsive during discovery.
        microphoneDiscoveryGeneration &+= 1
        let generation = microphoneDiscoveryGeneration
        DispatchQueue.global(qos: .utility).async { [weak self] in
            let devices = MicrophoneRecorder.availableDevices()
            DispatchQueue.main.async { [weak self] in
                guard let self, self.microphoneDiscoveryGeneration == generation else { return }
                self.microphoneDevices = devices
            }
        }
    }

    func refreshKeyboardDevices() {
        do {
            try getKeyboardHID().start()
            keyboardHID?.refresh()
            keyboardSourceStatus = "设备级快捷键需要系统输入监控权限。未连接的已选键盘不会触发录音。"
        } catch {
            keyboardSourceStatus = error.localizedDescription
        }
    }

    func captureKeyFromSelectedKeyboard(_ id: String, action: @escaping @MainActor (MacModifierKey) -> Void) {
        guard !id.isEmpty else { return }
        do {
            let hid = getKeyboardHID()
            try hid.start()
            hid.capture(deviceID: id, onKey: action)
        } catch {
            keyboardSourceStatus = error.localizedDescription
        }
    }

    /// Listen for individual modifier keys across the OS in capture mode.
    /// The settings window's local NSEvent monitor remains a fallback when
    /// Accessibility has not yet been granted.
    func captureGlobalModifierKey(_ onKey: @escaping @MainActor (MacModifierKey) -> Void) {
        do {
            let driver = try makeModifierHoldDriver()
            driver.setCapture(onKey)
            rightModifierDriver = driver
        } catch {
            interactionStatus = "全局按键监听不可用，请检查辅助功能权限：\(error.localizedDescription)"
        }
    }

    func stopKeyboardDeviceCapture() {
        keyboardHID?.capture(deviceID: nil, onKey: nil)
        rightModifierDriver?.setCapture(nil)
        let needsGlobalHold = (activeShortcutPlan?.modifierDictation != nil && activeDictationKeyboardID.isEmpty) ||
            (activeShortcutPlan?.modifierRewrite != nil && activeRewriteKeyboardID.isEmpty)
        if !needsGlobalHold {
            rightModifierDriver?.shutdown()
            rightModifierDriver = nil
        }
    }

    private func getKeyboardHID() -> MacKeyboardHIDMonitor {
        if let keyboardHID { return keyboardHID }
        let hid = MacKeyboardHIDMonitor(
            onDevices: { [weak self] in self?.keyboardDevices = $0 },
            onPress: { [weak self] key, deviceID, pid, pressedAt in
                guard let self, let bound = self.activeShortcutPlan else { return }
                if key == bound.modifierDictation && deviceID == self.activeDictationKeyboardID {
                    self.keyboardPressed(.dictation, targetPID: pid, pressedAt: pressedAt)
                } else if key == bound.modifierRewrite && deviceID == self.activeRewriteKeyboardID {
                    self.keyboardPressed(.rewrite, targetPID: pid)
                }
            },
            onRelease: { [weak self] key, deviceID in
                guard let self, let bound = self.activeShortcutPlan else { return }
                if key == bound.modifierDictation && deviceID == self.activeDictationKeyboardID {
                    self.keyboardReleased(.dictation)
                } else if key == bound.modifierRewrite && deviceID == self.activeRewriteKeyboardID {
                    self.keyboardReleased(.rewrite)
                }
            },
            onCancel: { [weak self] in self?.cancelRightModifierVoice() }
        )
        keyboardHID = hid
        return hid
    }

    func refreshMicrophonePermission() {
        microphonePermission = microphone.permissionStatus
    }

    func openMicrophonePrivacySettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone") else { return }
        NSWorkspace.shared.open(url)
    }

    func launch(demo: Bool = false) {
        showStartupDemoOnConnect = demo
        refreshMicrophonePermission()
        refreshMicrophoneDevices()
        recordingIndicator.prepare()
        DiagnosticLog.note("应用已启动")
        if case .disconnected = connection { connect() }
        if hotKeys == nil { registerHotKeys() }
        refreshSideMouse()
    }

    func connect() {
        helperStartupWatchdog?.cancel()
        helperStartupWatchdog = nil
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
        guard let executable = helperExecutableURL ?? Bundle.main.executableURL?.deletingLastPathComponent()
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
        // Handy's audio-worker postmortem highlights that a successful launch
        // is not proof the pipeline can reply. Bound both cold spawn and hello;
        // stale sessions never regain ownership after the deadline.
        helperStartupWatchdog = Task { @MainActor [weak self] in
            do {
                try await Task.sleep(
                    nanoseconds: Self.helperStartupTimeoutSeconds * 1_000_000_000)
            } catch { return }
            guard let self, self.helloGeneration.accepts(token),
                  self.bridge === client else { return }
            guard case .connecting = self.connection else { return }
            DiagnosticLog.note("stage=helper_hello status=timeout")
            _ = self.helloGeneration.begin()
            self.bridge = nil
            client.stop()
            self.applyHelperConnection(.handshakeFailed)
        }
        // Process.run() can synchronously block AppKit for hundreds of
        // milliseconds on a cold launch. Spawn off-main; fence stale connects.
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let started: Bool
            do {
                try client.start()
                started = true
            } catch {
                started = false
            }
            DispatchQueue.main.async { [weak self] in
                guard let self, self.helloGeneration.accepts(token),
                      self.bridge === client else {
                    client.stop()
                    return
                }
                guard started else {
                    self.applyHelperConnection(.startFailed)
                    return
                }
                client.send(operation: "hello") { [weak self] response in
                    Task { @MainActor [weak self] in
                        guard let self, self.helloGeneration.accepts(token) else { return }
                        switch response {
                        case .success(let reply):
                            guard helperHelloAccepted(
                                ok: reply.ok, name: reply.result?["name"]?.string,
                                version: reply.result?["protocol"]?.int
                            ) else {
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
        guard let (dictation, rewrite) = plan else { return }
        _ = shortcutGeneration.begin()
        defer {
            asrKeyInput = ""
            llmKeyInput = ""
        }
        do {
            guard let store else { throw SettingsError.insecureStorage }
            let previous = activeShortcutPlan
            let previousDictationKeyboardID = activeDictationKeyboardID
            let previousRewriteKeyboardID = activeRewriteKeyboardID
            guard try ShortcutSaveTransaction.run(
                activate: { self.register(dictation: dictation, rewrite: rewrite) },
                persist: { try store.save(self.settings) },
                restore: {
                    self.activeDictationKeyboardID = previousDictationKeyboardID
                    self.activeRewriteKeyboardID = previousRewriteKeyboardID
                    self.restoreShortcutPlan(previous)
                }
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

    private func acceptedShortcutPlan() -> (ShortcutBinding, ShortcutBinding)? {
        // Carbon hotkeys have no trustworthy source device: scope only HID modifier holds.
        guard (settings.dictationKeyboardID.isEmpty ||
               MacModifierKey(canonical: settings.dictationShortcut) != nil),
              (settings.rewriteKeyboardID.isEmpty ||
               MacModifierKey(canonical: settings.rewriteShortcut) != nil),
              MacSettings.validKeyboardID(settings.dictationKeyboardID),
              MacSettings.validKeyboardID(settings.rewriteKeyboardID) else { return nil }
        guard let dictation = ShortcutBinding(canonical: settings.dictationShortcut),
              let rewrite = ShortcutBinding(canonical: settings.rewriteShortcut),
              let exit = ShortcutBinding(canonical: settings.exitShortcut),
              MacShortcutPlan(dictation: dictation, rewrite: rewrite,
                              exit: exit, recordMode: settings.recordMode,
                              dictationDeviceID: settings.dictationKeyboardID,
                              rewriteDeviceID: settings.rewriteKeyboardID) != nil else {
            return nil
        }
        return (dictation, rewrite)
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
        if accessibilityAuthorized && rightModifierDriver == nil { registerHotKeys() }
    }

    private func registerHotKeys() {
        guard let dictation = ShortcutBinding(canonical: settings.dictationShortcut),
              let rewrite = ShortcutBinding(canonical: settings.rewriteShortcut) else {
            interactionStatus = shortcutPendingHelper()
            return
        }
        register(dictation: dictation, rewrite: rewrite)
    }

    private func refreshShortcutsFromCore() {
        guard let bridge, case .connected = connection else { return }
        let token = shortcutGeneration.begin()
        let values = (dictation: settings.dictationShortcut,
                      rewrite: settings.rewriteShortcut)
        parseShortcut(values.dictation) { [weak self] dictation in
            guard let self else { return }
            guard shortcutRefresh(tokenValid: self.shortcutGeneration.accepts(token), parsed: dictation != nil, remaining: 1) == .parseNext, let dictation else { return }
            self.parseShortcut(values.rewrite) { [weak self] rewrite in
                guard let self else { return }
                guard shortcutRefresh(tokenValid: self.shortcutGeneration.accepts(token), parsed: rewrite != nil, remaining: 0) == .register, let rewrite else { return }
                if shortcutRefreshStores(registered: self.register(dictation: dictation, rewrite: rewrite)) {
                    self.settings.dictationShortcut = dictation.normalized
                    self.settings.rewriteShortcut = rewrite.normalized
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

    /// Register a listen-only macOS modifier tap without touching Carbon's
    /// key-combination registry. Installing the tap first keeps old bindings
    /// active when permissions are missing.
    private func makeModifierHoldDriver() throws -> MacModifierHoldDriver {
        let driver = rightModifierDriver ?? MacModifierHoldDriver(
            onHold: { [weak self] key, pid, pressedAt in
                guard let self, let bound = self.activeShortcutPlan else { return }
                if key == bound.modifierDictation && self.activeDictationKeyboardID.isEmpty {
                    self.keyboardPressed(.dictation, targetPID: pid, pressedAt: pressedAt)
                } else if key == bound.modifierRewrite && self.activeRewriteKeyboardID.isEmpty {
                    self.keyboardPressed(.rewrite, targetPID: pid)
                }
            },
            onRelease: { [weak self] key in
                guard let self, let bound = self.activeShortcutPlan else { return }
                if key == bound.modifierDictation && self.activeDictationKeyboardID.isEmpty {
                    self.keyboardReleased(.dictation)
                } else if key == bound.modifierRewrite && self.activeRewriteKeyboardID.isEmpty {
                    self.keyboardReleased(.rewrite)
                }
            },
            onCancel: { [weak self] in self?.cancelRightModifierVoice() },
            onFault: { [weak self] error in
                self?.interactionStatus = "右侧快捷键监听失败：\(error.localizedDescription)"
            }
        )
        try driver.start()
        return driver
    }

    private func cancelRightModifierVoice() {
        guard keyboardGate.action != nil else { return }
        keyboardGate.clear()
        keyboardHoldSequence &+= 1
        if meetingActive {
            if meetingVoiceRecording || meetingVoiceStarting {
                cancelMeetingVoiceStart()
                dismissIndicator()
            }
        } else if isRecording || isStarting {
            cancelCurrent()
        }
    }

    /// Keep working global shortcuts registered until every new key succeeds.
    @discardableResult
    private func register(dictation: ShortcutBinding, rewrite: ShortcutBinding) -> Bool {
        guard let exit = ShortcutBinding(canonical: settings.exitShortcut),
              let next = MacShortcutPlan(dictation: dictation, rewrite: rewrite,
                                         exit: exit, recordMode: settings.recordMode,
                                          dictationDeviceID: settings.dictationKeyboardID,
                                          rewriteDeviceID: settings.rewriteKeyboardID) else {
            interactionStatus = shortcutPlanRejected()
            return false
        }
        let label = voiceShortcutPlanLabel(
            dictation: MacShortcut.display(dictation.normalized),
            rewrite: MacShortcut.display(rewrite.normalized))
        let needsModifier = (next.modifierDictation != nil && settings.dictationKeyboardID.isEmpty) ||
            (next.modifierRewrite != nil && settings.rewriteKeyboardID.isEmpty)
        let needsHID = !settings.dictationKeyboardID.isEmpty || !settings.rewriteKeyboardID.isEmpty
        var candidate: MacModifierHoldDriver?
        do {
            if needsModifier { candidate = try makeModifierHoldDriver() }
            if needsHID {
                try getKeyboardHID().start()
                let chosen = [settings.dictationKeyboardID, settings.rewriteKeyboardID].filter { !$0.isEmpty }
                // Allow saving a disconnected keyboard profile. The HID driver
                // matches exact source IDs, so it stays inert until the device
                // appears; there is never a global Quartz fallback.
                if !chosen.allSatisfy({ id in keyboardDevices.contains(where: { $0.id == id && $0.selectable }) }) {
                    keyboardSourceStatus = "所选键盘暂未连接或无法唯一区分；快捷键不会回退到其他键盘"
                }
            }
            if let registry = hotKeys {
                try registry.replace(with: next.keyboard)
            } else {
                let (driver, registry) = try createHotKeyRegistry(plan: next)
                hotKeyDriver = driver
                hotKeys = registry
            }
            activeShortcutPlan = next
            keyboardHoldEnabled = next.recordMode == "hold"
            activeDictationKeyboardID = settings.dictationKeyboardID
            activeRewriteKeyboardID = settings.rewriteKeyboardID
            if needsHID {
                getKeyboardHID().setBindings(
                    dictation: next.modifierDictation.flatMap {
                        settings.dictationKeyboardID.isEmpty ? nil :
                            MacKeyboardHIDBinding(deviceID: settings.dictationKeyboardID, key: $0)
                    },
                    rewrite: next.modifierRewrite.flatMap {
                        settings.rewriteKeyboardID.isEmpty ? nil :
                            MacKeyboardHIDBinding(deviceID: settings.rewriteKeyboardID, key: $0)
                    })
                keyboardHID?.setSuspended(shortcutCaptureActive)
                keyboardHID?.setPaused(voicePaused)
            } else {
                keyboardHID?.setBindings(dictation: nil, rewrite: nil)
            }
            if let candidate {
                candidate.setBindings(
                    dictation: settings.dictationKeyboardID.isEmpty ? next.modifierDictation : nil,
                    rewrite: settings.rewriteKeyboardID.isEmpty ? next.modifierRewrite : nil)
                candidate.setSuspended(shortcutCaptureActive)
                candidate.setVoicePaused(voicePaused)
                rightModifierDriver = candidate
            } else {
                rightModifierDriver?.shutdown()
                rightModifierDriver = nil
            }
            sideMouse?.setBindings(shortcutCaptureBindings())
            interactionStatus = label
            return true
        } catch {
            if rightModifierDriver == nil { candidate?.shutdown() }
            interactionStatus = "快捷键注册失败：\(error.localizedDescription)；原快捷键保持有效"
            return false
        }
    }

    private func restoreShortcutPlan(_ previous: MacShortcutPlan?) {
        do {
            if let registry = hotKeys {
                try registry.replace(with: previous?.keyboard ?? [])
            } else if let previous {
                let (driver, registry) = try createHotKeyRegistry(plan: previous)
                hotKeyDriver = driver
                hotKeys = registry
            }
            activeShortcutPlan = previous
            keyboardHoldEnabled = previous?.recordMode == "hold"
            // Preferences may contain unsaved changes. Restore only the previous live devices.
            keyboardHID?.setBindings(
                dictation: previous?.modifierDictation.flatMap {
                    activeDictationKeyboardID.isEmpty ? nil :
                        MacKeyboardHIDBinding(deviceID: activeDictationKeyboardID, key: $0)
                },
                rewrite: previous?.modifierRewrite.flatMap {
                    activeRewriteKeyboardID.isEmpty ? nil :
                        MacKeyboardHIDBinding(deviceID: activeRewriteKeyboardID, key: $0)
                })
            if let previous,
               previous.modifierDictation != nil || previous.modifierRewrite != nil {
                let driver = try makeModifierHoldDriver()
                driver.setBindings(
                    dictation: activeDictationKeyboardID.isEmpty ? previous.modifierDictation : nil,
                    rewrite: activeRewriteKeyboardID.isEmpty ? previous.modifierRewrite : nil)
                driver.setSuspended(shortcutCaptureActive)
                driver.setVoicePaused(voicePaused)
                rightModifierDriver = driver
            } else {
                rightModifierDriver?.shutdown()
                rightModifierDriver = nil
            }
            sideMouse?.setBindings(shortcutCaptureBindings())
        } catch {
            interactionStatus = "无法恢复原快捷键：\(error.localizedDescription)"
        }
    }

    private func createHotKeyRegistry(plan: MacShortcutPlan) throws -> (CarbonHotKeyDriver, HotKeyRegistry) {
        let driver = CarbonHotKeyDriver(onPressed: { [weak self] shortcut, pid in
            guard let self, let bound = self.activeShortcutPlan else { return }
            if shortcut == bound.exit {
                NSApp.terminate(nil)
                return
            }
            if shortcut == bound.dictation {
                self.keyboardPressed(.dictation, targetPID: pid)
            } else if shortcut == bound.rewrite {
                self.keyboardPressed(.rewrite, targetPID: pid)
            }
        }, onReleased: { [weak self] shortcut in
            guard let self, let bound = self.activeShortcutPlan else { return }
            if shortcut == bound.dictation {
                self.keyboardReleased(.dictation)
            } else if shortcut == bound.rewrite {
                self.keyboardReleased(.rewrite)
            }
        })
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

    /// Keyboard hold is the default; changing to Toggle persists independently
    /// of native OS hotkey registration. Settings UI owns how this is presented.
    func setKeyboardHoldEnabled(_ enabled: Bool) {
        guard keyboardHoldEnabled != enabled else { return }
        guard !isRecording, !isStarting, !isProcessing,
              !meetingVoiceRecording, !meetingVoiceStarting,
              keyboardGate.action == nil else {
            interactionStatus = "请先结束当前语音操作，再切换键盘录音模式"
            return
        }
        keyboardGate.clear()
        keyboardHoldSequence &+= 1
        keyboardHoldEnabled = enabled
        settings.recordMode = enabled ? "hold" : "toggle"
    }

    // Internal so the CI user-journey test enters through the *same* keyboard
    // state machine as the actual native right-modifier/keyboard callbacks.
    func keyboardPressed(_ action: SideMouseAction, targetPID: Int32?, pressedAt: TimeInterval? = nil) {
        if !keyboardHoldEnabled {
            if meetingActive {
                toggleMeetingVoice(intent: action == .dictation ? .express : .compose)
            } else if action == .dictation {
                toggleRecording(from: targetPID)
            } else {
                triggerSelectionRewrite(from: targetPID)
            }
            return
        }
        guard !voicePaused, !meetingStarting,
              !isRecording, !isStarting, !isProcessing,
              !meetingVoiceStarting, !meetingVoiceRecording,
              !meetingBusy, !meetingCancelPending,
              sideDictationPreflight == nil, sideGate.active == nil,
              keyboardGate.begin(action) else { return }
        if meetingActive {
            toggleMeetingVoice(intent: action == .dictation ? .express : .compose)
        } else if action == .dictation {
            startRecording(targetPID: targetPID, pressedAt: pressedAt)
        } else {
            triggerSelectionRewrite(from: targetPID)
        }
        keyboardHoldSequence &+= 1
        let sequence = keyboardHoldSequence
        let maximumSeconds = settings.maxRecordSeconds
        DispatchQueue.main.asyncAfter(deadline: .now() + Double(maximumSeconds + 2)) { [weak self] in
            guard let self, self.keyboardHoldSequence == sequence,
                  self.keyboardGate.action == action else { return }
            self.keyboardGate.clear()
            self.keyboardHoldSequence &+= 1
            self.hotKeyDriver?.resetHeldState()
            self.interactionStatus = "未收到录音快捷键松开事件，已结束本次录音"
            if self.meetingActive {
                if self.meetingVoiceRecording { self.toggleMeetingVoice() }
                else if self.meetingVoiceStarting { self.cancelMeetingVoiceStart() }
            } else if self.isRecording {
                self.stopRecording()
            } else if self.isStarting {
                self.cancelCurrent()
            }
        }
    }

    func keyboardReleased(_ action: SideMouseAction) {
        guard keyboardHoldEnabled else { return }
        let starting = meetingActive ? meetingVoiceStarting : isStarting
        let recording = meetingActive ? meetingVoiceRecording : isRecording
        let effect = keyboardGate.release(action, starting: starting, recording: recording)
        if effect == .stop {
            keyboardHoldSequence &+= 1
            if meetingActive { toggleMeetingVoice() }
            else { stopRecording() }
        } else if effect == .clear {
            keyboardHoldSequence &+= 1
        }
    }

    private func finishKeyboardStart(_ action: SideMouseAction) {
        let effect = keyboardGate.finishStart(action, recording: meetingActive ? meetingVoiceRecording : isRecording)
        if effect == .stop {
            keyboardHoldSequence &+= 1
            if meetingActive { toggleMeetingVoice() }
            else { stopRecording() }
        } else if effect == .clear {
            keyboardHoldSequence &+= 1
        }
    }

    private func cancelMeetingVoiceStart() {
        _ = meetingVoiceGeneration.begin()
        microphone.cancel()
        meetingVoiceStarting = false
        meetingVoiceRecording = false
    }

    /// Settings key learning passes Mouse4/Mouse5 through to its native window.
    func setShortcutCaptureActive(_ active: Bool) {
        guard shortcutCaptureActive != active else { return }
        shortcutCaptureActive = active
        rightModifierDriver?.setSuspended(active)
        keyboardHID?.setSuspended(active)
        sideMouse?.setBindings(shortcutCaptureBindings())
    }

    private func shortcutCaptureBindings() -> SideMouseBindings {
        shortcutCaptureActive
            ? SideMouseBindings(dictationButton: nil, rewriteButton: nil)
            : (activeShortcutPlan?.sideButtons ?? SideMouseBindings())
    }

    func refreshSideMouse() {
        accessibilityAuthorized = InteractionSession.accessibilityPermissionGranted()
        guard accessibilityAuthorized else {
            rightModifierDriver?.shutdown()
            rightModifierDriver = nil
            sideMouse?.shutdown()
            sideMouse = nil
            sideMouseStatus = sideMouseAccessibilityDenied()
            return
        }
        if rightModifierDriver == nil &&
           (activeShortcutPlan == nil ||
            (activeShortcutPlan?.modifierDictation != nil && activeDictationKeyboardID.isEmpty) ||
            (activeShortcutPlan?.modifierRewrite != nil && activeRewriteKeyboardID.isEmpty)) {
            registerHotKeys()
        }
        let isFreshStart = sideMouse == nil
        let driver = sideMouse ?? SideMouseButtonDriver(
            bindings: shortcutCaptureBindings(),
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
                guard let self else { return }
                if self.meetingActive {
                    self.cancelMeetingSideHold()
                } else {
                    self.cancelCurrent()
                }
            },
            onObservedButton: { [weak self] button in
                let action = self?.activeShortcutPlan?.sideButtons.action(for: button) ?? SideMouseBindings().action(for: button)
                self?.sideMouseStatus = sideMouseObserved(button: button, action: action)
            },
            onFault: { [weak self] failure in
                self?.sideMouseStatus = sideMouseListenFailed(failure.localizedDescription)
            }
        )
        do {
            driver.setBindings(shortcutCaptureBindings())
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
        if voicePaused && (meetingActive || meetingStarting) {
            stopMeeting(reason: "语音已暂停；会议采集已结束")
        }
        sideMouse?.setVoicePaused(voicePaused)
        rightModifierDriver?.setVoicePaused(voicePaused)
        keyboardHID?.setPaused(voicePaused)
        dismissPreviewIndicator()
        if let status = turn.sideStatus {
            sideMouseStatus = status
        }
    }

    private func cancelMeetingSideHold() {
        guard meetingSideAction != nil else { return }
        clearMeetingVoiceWatchdog()
        meetingSideAction = nil
        meetingSideReleasedDuringStart = false
        _ = meetingVoiceGeneration.begin()
        if meetingVoiceRecording || meetingVoiceStarting { microphone.cancel() }
        meetingVoiceRecording = false
        meetingVoiceStarting = false
        meetingExpressionStatus = "鼠标侧键监听已中断；本次会议语音已取消"
    }

    private func sideMouseHeld(_ action: SideMouseAction, targetPID: Int32?) {
        if meetingActive && !meetingDemoActive {
            dismissPreviewIndicator()
            guard meetingSideAction == nil, !meetingBusy, !meetingCancelPending,
                  !meetingVoiceRecording, !meetingVoiceStarting,
                  !isRecording, !isStarting, !isProcessing else { return }
            meetingSideAction = action
            meetingSideReleasedDuringStart = false
            toggleMeetingVoice(intent: action == .dictation ? .express : .compose)
            return
        }
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
        case .rewrite:
            triggerSelectionRewrite(from: targetPID)
        }
        sideGate.began(action)
    }

    private func sideMouseReleased(_ action: SideMouseAction) {
        if meetingSideAction == action {
            meetingSideAction = nil
            if meetingVoiceStarting {
                meetingSideReleasedDuringStart = true
            } else if meetingVoiceRecording {
                toggleMeetingVoice()
            }
            return
        }
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
        armProcessingWatchdog(token: generation, seconds: 30, stage: "microphone_start")
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
                self.finishProcessingWatchdog()
                if current.held { self.promoteSideDictation(current) }
            } catch {
                guard let current = self.sideDictationPreflight,
                      current.generation == generation else { return }
                self.sideDictationPreflight = nil
                self.finishProcessingWatchdog()
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
        finishProcessingWatchdog()
        _ = resultGeneration.begin()
        microphone.cancel()
        isStarting = false
    }

    private func promoteSideDictation(_ preflight: SideDictationPreflight) {
        guard sideDictationPreflight?.generation == preflight.generation,
              preflight.ready, preflight.held,
              resultGeneration.accepts(preflight.generation) else { return }
        sideDictationPreflight = nil
        // The speculative microphone is ready, but another application's
        // AX service may be blocked. Capture its target off-main, retaining the
        // "starting" state so an early side-button release is honored.
        armProcessingWatchdog(token: preflight.generation, seconds: 20, stage: "dictation_capture")
        Task { @MainActor [weak self] in
            guard let self else { return }
            var snapshot: MacDictationTarget?
            var failure: String?
            if let pid = preflight.targetPID, pid > 0 {
                do {
                    snapshot = try await self.dictationDelivery.captureAsync(processID: pid)
                } catch {
                    guard self.resultGeneration.accepts(preflight.generation) else { return }
                    DiagnosticLog.note("stage=dictation_capture status=failed code=\(insertionDiagnosticCode(error))")
                    failure = captureStatus(error, password: ResultCopy.passwordDictation)
                }
            }
            guard self.resultGeneration.accepts(preflight.generation) else { return }
            self.finishProcessingWatchdog()
            let arm = dictationArm(hasTarget: (preflight.targetPID ?? 0) > 0,
                                  tokenValid: true, captured: snapshot != nil,
                                  captureFailure: failure)
            if let status = arm.interactionStatus { self.interactionStatus = status }
            if arm.keepInsert { self.activeInsert = snapshot }
            let ready = dictationMicrophoneReady(started: true, tokenValid: true, failure: "")
            self.isRecording = true
            self.isStarting = false
            self.showIndicator(phase: .recording, mode: "听写", pending: false)
            if let status = ready.recordingStatus { self.recordingStatus = status }
            self.stopWhenSideMouseReleased(.dictation)
        }
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
                self.meetingStatus = (error as? SystemAudioError)?.localizedDescription
                    ?? "无法读取音频来源；请在系统设置中允许屏幕与系统音频录制后重试"
            }
        }
    }

    /// Save the same portable two-field profile as Windows. During a live
    /// meeting, commit the new model/ASR configuration through Go's epoch
    /// boundary. If live delivery fails, the persisted fields are explicitly
    /// next-session only; never claim they affected in-flight inference.
    func updateMeetingProfile(background: String, terms: String, mode: String,
                              completion: @escaping (Bool, String) -> Void) {
        guard !meetingStarting && !meetingCaptureSwitching && !meetingProfileUpdating &&
              !meetingBusy && !meetingVoiceRecording && !meetingVoiceStarting &&
              meetingVoiceASRID == nil else {
            completion(false, "请先完成当前会议切换或语音处理")
            return
        }
        do {
            guard let store else { throw SettingsError.insecureStorage }
            var next = settings
            next.meetingBackground = background
            next.meetingTerms = terms
            next.meetingASRMode = mode
            try next.canonicalize()
            try store.save(next)
            settings = next
        } catch {
            let message = settingsSaveFailed(error.localizedDescription)
            settingsStatus = message
            completion(false, message)
            return
        }
        guard meetingActive && !meetingDemoActive else {
            let message = "会议资料和识别模式已保存；下次真实会议开始时生效"
            settingsStatus = message
            completion(true, message)
            return
        }
        guard let id = meetingSessionID, let bridge else {
            let message = "资料已保存，但当前会议未连接；重新开始会议后生效"
            settingsStatus = message
            completion(false, message)
            return
        }
        let config: JSONValue
        do {
            guard case .object(var values) = try requestConfig(for: .spokenSelectedTask) else {
                throw BridgeFailure.invalidRequest
            }
            values["meeting_asr_mode"] = .string(settings.meetingASRMode)
            config = .object(values)
        } catch {
            let message = "资料已保存，但无法向当前会议提供有效配置；下次开始时生效"
            settingsStatus = message
            completion(false, message)
            return
        }
        meetingProfileUpdating = true
        meetingPresentationRevision &+= 1
        let token = meetingGeneration.latest
        bridge.send(operation: "meeting_configure", fields: [
            "session_id": .string(id), "config": config,
        ]) { [weak self] result in
            Task { @MainActor [weak self] in
                guard let self, self.meetingSessionID == id,
                      self.meetingGeneration.accepts(token) else {
                    completion(false, "会话已更换；资料已保存，新会议开始时生效")
                    return
                }
                self.meetingProfileUpdating = false
                self.meetingPresentationRevision &+= 1
                switch result {
                case .success(let reply) where reply.ok:
                    // Old in-flight feed completions are fenced by audio_epoch below.
                    // Rebase native backpressure to the new Go runtime epoch;
                    // otherwise old pending feeds can falsely stop fresh capture.
                    self.meetingPendingPackets = 0
                    self.applyMeetingSnapshot(reply.result ?? [:])
                    let message = "已应用到当前会议 · 版本 \(self.meetingProfileVersion)；旧识别任务已取消"
                    self.settingsStatus = message
                    completion(true, message)
                default:
                    let message = "资料已保存，但当前会议更新失败；重新开始会议后生效"
                    self.settingsStatus = message
                    completion(false, message)
                }
            }
        }
    }

    private func clearMeetingStartWatchdog() {
        meetingStartWatchdog?.cancel()
        meetingStartWatchdog = nil
    }

    private func armMeetingStartWatchdog(token: UInt64, seconds: UInt64 = 60) {
        clearMeetingStartWatchdog()
        meetingStartWatchdog = Task { @MainActor [weak self] in
            do { try await Task.sleep(nanoseconds: seconds * 1_000_000_000) }
            catch { return }
            guard let self, self.meetingGeneration.accepts(token),
                  self.meetingStarting else { return }
            DiagnosticLog.note("stage=meeting_start status=timeout")
            self.stopMeeting(reason: "会议启动超时，已取消本次采集；请检查 Helper 和系统音频权限")
        }
    }

    /// Preview data comes entirely from the bundled Go Core fixture. This does
    /// not request audio permissions, start ScreenCaptureKit, or call a provider.
    func startMeetingDemo() {
        guard !meetingActive && !meetingStarting, !meetingDemoActive else { return }
        guard let bridge, case .connected = connection else {
            meetingStatus = "离线演示需要已连接的本地 Go Helper，不需要 ASR/LLM 服务"
            return
        }
        let token = meetingGeneration.begin()
        meetingStarting = true
        armMeetingStartWatchdog(token: token, seconds: 15)
        bridge.send(operation: "meeting_demo") { [weak self] result in
            Task { @MainActor [weak self] in
                guard let self, self.meetingGeneration.accepts(token),
                      !self.meetingActive && self.meetingStarting else { return }
                self.meetingStarting = false
                self.clearMeetingStartWatchdog()
                guard case .success(let reply) = result, reply.ok,
                      reply.result?["demo"]?.boolValue == true else {
                    self.meetingStatus = "无法读取本地离线演示数据"
                    self.showStartupDemoOnConnect = false
                    return
                }
                self.meetingDemoActive = true
                self.meetingActive = true
                self.meetingSessionID = nil
                self.meetingCaptionsOnly = false
                self.meetingExpressionExpanded = false
                self.meetingSelectedIndex = 0
                self.applyMeetingSnapshot(reply.result ?? [:])
                if case .array(let items)? = reply.result?["captions"] {
                    self.meetingCaptions = items.compactMap { item in
                        guard case .object(let row) = item, let id = row["id"]?.int,
                              let source = row["source"]?.string,
                              let chinese = row["chinese"]?.string else { return nil }
                        return MeetingCaptionRow(id: id, source: source, chinese: chinese,
                            final: row["final"]?.boolValue == true)
                    }
                }
                self.meetingStatus = "离线演示 · 固定测试字幕，不采集 Zoom 或系统音频，不请求在线服务"
                if self.showStartupDemoOnConnect {
                    self.showStartupDemoOnConnect = false
                    MeetingReaderWindowController.shared.open(model: self)
                }
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
        meetingCapturePaused = false
        meetingCaptureSwitching = false
        meetingDemoActive = false
        meetingProfileVersion = 1
        meetingCaptureSourceID = meetingSourceID
        meetingAudioEpoch = 0
        meetingPendingPackets = 0
        meetingStarting = true
        armMeetingStartWatchdog(token: token)
        meetingStatus = "正在准备会议识别…"
        meetingCaptions = []
        meetingHistoryTrimmed = 0
        meetingCaptionsOnly = false
        meetingExpressionExpanded = false
        meetingReplies = []
        meetingReplyPending = false
        meetingReplyStatus = ""
        meetingReplyTip = ""
        meetingReplySource = ""
        meetingExpression = ""
        meetingExpressionChinese = ""
        meetingExpressionSource = ""
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
                    self.clearMeetingStartWatchdog()
                    self.meetingStatus = "无法启动会议处理，请检查本地 Helper"
                    return
                }
                guard self.meetingGeneration.accepts(token) else {
                    bridge.send(operation: "meeting_stop", fields: ["session_id": .string(id)]) { _ in }
                    return
                }
                guard let epoch = result?.result?["audio_epoch"]?.int, epoch > 0 else {
                    bridge.send(operation: "meeting_stop", fields: ["session_id": .string(id)]) { _ in }
                    self.stopMeeting(reason: "会议处理未返回音频代际，请更新 Helper")
                    return
                }
                self.meetingSessionID = id
                self.meetingAudioEpoch = epoch
                let capture = self.meetingCaptureGeneration.begin()
                do {
                    try await self.systemAudio.start(sourceID: self.meetingCaptureSourceID,
                        onPCM: { [weak self] bytes in
                            self?.feedMeeting(bytes, sessionID: id, capture: capture)
                        },
                        onFailure: { [weak self] detail in
                            guard let self, self.meetingSessionID == id,
                                  self.meetingCaptureGeneration.accepts(capture) else { return }
                            self.stopMeeting(reason: detail)
                        })
                    guard self.meetingGeneration.accepts(token),
                          self.meetingCaptureGeneration.accepts(capture) else { return }
                    self.meetingStarting = false
                    self.clearMeetingStartWatchdog()
                    self.meetingActive = true
                    self.meetingStatus = "正在监听所选应用音频"
                    self.pollMeeting(id: id, token: token)
                } catch {
                    guard self.meetingGeneration.accepts(token),
                          self.meetingCaptureGeneration.accepts(capture) else { return }
                    let detail = (error as? SystemAudioError)?.localizedDescription
                        ?? "无法启动系统音频；请检查屏幕与系统音频录制权限及来源"
                    self.stopMeeting(reason: detail)
                }
            }
        }
    }

    private func feedMeeting(_ bytes: Data, sessionID id: String, capture: UInt64) {
        guard meetingSessionID == id, meetingCaptureGeneration.accepts(capture),
              meetingAudioEpoch > 0, meetingActive, !meetingCapturePaused,
              !meetingCaptureSwitching, !meetingProfileUpdating, let bridge else { return }
        guard meetingPendingPackets < 8 else {
            stopMeeting(reason: "会议音频处理过载，采集已停止；请检查电脑负载")
            return
        }
        meetingPendingPackets += 1
        let sentEpoch = meetingAudioEpoch
        bridge.sendPCM(operation: "meeting_feed", pcm: bytes, fields: [
            "session_id": .string(id),
            "audio_epoch": .number(Double(meetingAudioEpoch)),
        ]) { [weak self] reply in
            Task { @MainActor [weak self] in
                guard let self, self.meetingSessionID == id,
                      self.meetingCaptureGeneration.accepts(capture),
                      self.meetingAudioEpoch == sentEpoch else { return }
                self.meetingPendingPackets = max(0, self.meetingPendingPackets - 1)
                guard case .success(let response) = reply, response.ok else {
                    if self.meetingCapturePaused || self.meetingCaptureSwitching || self.meetingProfileUpdating { return }
                    self.stopMeeting(reason: "会议音频传输中断，已停止采集")
                    return
                }
            }
        }
    }

    static func meetingSnapshotExpired(startedAt: TimeInterval, now: TimeInterval) -> Bool {
        startedAt > 0 && now - startedAt >= 12
    }

    private func pollMeeting(id: String, token: UInt64) {
        Task { @MainActor [weak self] in
            while let self, self.meetingSessionID == id,
                  self.meetingGeneration.accepts(token), self.meetingActive {
                guard !self.meetingSnapshotPending else {
                    if Self.meetingSnapshotExpired(
                        startedAt: self.meetingSnapshotStartedAt,
                        now: ProcessInfo.processInfo.systemUptime) {
                        self.stopMeeting(reason: "会议状态请求超时，已结束采集；请检查 Helper")
                        break
                    }
                    try? await Task.sleep(nanoseconds: 450_000_000)
                    continue
                }
                guard let bridge = self.bridge else {
                    self.stopMeeting(reason: "会议 Helper 已断开")
                    break
                }
                self.meetingSnapshotPending = true
                self.meetingSnapshotStartedAt = ProcessInfo.processInfo.systemUptime
                let revision = self.meetingPresentationRevision
                self.meetingSnapshotRequestID = bridge.send(operation: "meeting_snapshot", fields: [
                    "session_id": .string(id),
                ]) { [weak self] reply in
                    Task { @MainActor [weak self] in
                        guard let self, self.meetingSessionID == id,
                              self.meetingGeneration.accepts(token) else { return }
                        self.meetingSnapshotPending = false
                        self.meetingSnapshotRequestID = nil
                        self.meetingSnapshotStartedAt = 0
                        guard revision == self.meetingPresentationRevision,
                              !self.meetingCancelPending else { return }
                        guard case .success(let response) = reply, response.ok else {
                            self.stopMeeting(reason: "会议处理已断开")
                            return
                        }
                        self.applyMeetingSnapshot(response.result ?? [:])
                        if case .array(let values)? = response.result?["captions"] {
                            let incoming: [MeetingCaptionRow] = values.compactMap { value in
                                guard case .object(let item) = value,
                                      let id = item["id"]?.int,
                                      let source = item["source"]?.string,
                                      let chinese = item["chinese"]?.string else { return nil }
                                return MeetingCaptionRow(id: id, source: source,
                                    chinese: chinese, final: item["final"]?.boolValue == true)
                            }
                            // 450ms snapshots often contain unchanged history.
                            // Avoid invalidating all six SwiftUI reading panes
                            // and their potentially long text layouts on no-op polls.
                            if self.meetingCaptions != incoming { self.meetingCaptions = incoming }
                        }
                    }
                }
                try? await Task.sleep(nanoseconds: 450_000_000)
            }
        }
    }

    private func applyMeetingSnapshot(_ values: [String: JSONValue]) {
        if let epoch = values["epoch"]?.int, epoch > 0 { meetingAudioEpoch = epoch }
        if let version = values["profile_version"]?.int { meetingProfileVersion = version }
        if let paused = values["capture_paused"]?.boolValue, !meetingCaptureSwitching {
            if meetingCapturePaused != paused { meetingCapturePaused = paused }
        }
        if let status = values["status"]?.string {
            // The existing Meeting status line surfaces shared Go Core metrics
            // without introducing another Swift-owned audio state machine.
            var details: [String] = []
            if case .object(let metrics)? = values["metrics"] {
                let queued = (metrics["pending"]?.int ?? 0) + meetingPendingPackets
                let dropped = metrics["dropped"]?.int ?? 0
                if queued > 0 { details.append("音频负载队列 \(queued)") }
                if dropped > 0 { details.append("丢弃 \(dropped) 段") }
                if let ms = metrics["asr_done_ms"]?.int, ms > 0 {
                    details.append("ASR \(ms)ms")
                }
                if let ms = metrics["translation_done_ms"]?.int, ms > 0 {
                    details.append("翻译 \(ms)ms")
                }
            }
            let next = details.isEmpty ? status : status + " · " + details.joined(separator: " · ")
            if meetingStatus != next { meetingStatus = next }
        }
        if let trimmed = values["history_trimmed"]?.int { meetingHistoryTrimmed = trimmed }
        if let status = values["reply_status"]?.string, meetingReplyStatus != status { meetingReplyStatus = status }
        if let status = values["expression_status"]?.string, meetingExpressionStatus != status { meetingExpressionStatus = status }
        if let value = values["expression"]?.string, meetingExpression != value { meetingExpression = value }
        if let value = values["expression_chinese"]?.string, meetingExpressionChinese != value { meetingExpressionChinese = value }
        if let source = values["expression_source"]?.string, meetingExpressionSource != source { meetingExpressionSource = source }
        if let source = values["reply_source"]?.string, meetingReplySource != source { meetingReplySource = source }
        if let tip = values["reply_tip"]?.string, meetingReplyTip != tip { meetingReplyTip = tip }
        let pending = values["reply_pending"]?.boolValue == true
        if meetingReplyPending != pending { meetingReplyPending = pending }
        if let captionsOnly = values["captions_only"]?.boolValue {
            if meetingCaptionsOnly != captionsOnly { meetingCaptionsOnly = captionsOnly }
        }
        if let expanded = values["expression_expanded"]?.boolValue {
            if meetingExpressionExpanded != expanded { meetingExpressionExpanded = expanded }
        }
        if case .array(let items)? = values["replies"] {
            let incoming: [MeetingReplyRow] = items.enumerated().compactMap { index, value in
                guard case .object(let row) = value,
                      let english = row["english"]?.string,
                      let chinese = row["chinese"]?.string else { return nil }
                return MeetingReplyRow(id: index, english: english,
                    chinese: chinese, complete: row["complete"]?.boolValue == true)
            }
            if meetingReplies != incoming { meetingReplies = incoming }
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
        // Offline demo has a Go Core snapshot but deliberately no live session ID.
        // Its reader controls must still work without opening capture/inference.
        if meetingDemoActive {
            meetingCaptionsOnly.toggle()
            if meetingCaptionsOnly { meetingExpressionExpanded = false }
            meetingPresentationRevision &+= 1
            return
        }
        meetingAction("meeting_view", fields: [
            "button_id": .number(Double(ContractLimits.meetingButtonCaptions)),
        ])
    }

    func toggleMeetingExpressionExpanded() {
        if meetingDemoActive {
            if meetingCaptionsOnly { meetingCaptionsOnly = false }
            meetingExpressionExpanded.toggle()
            meetingPresentationRevision &+= 1
            return
        }
        meetingAction("meeting_view", fields: [
            "button_id": .number(Double(ContractLimits.meetingButtonExpression)),
        ])
    }

    // The reader initiates this only for a new expression while following
    // the latest turn. The bridge runs Go Core's ExpressionFocus policy.
    func focusMeetingExpressionIfReadingLatest() {
        guard meetingActive, !meetingBusy, !meetingCancelPending,
              !meetingExpression.isEmpty,
              meetingCaptionsOnly || !meetingExpressionExpanded else { return }
        meetingAction("meeting_view", fields: ["focus_expression": .boolean(true)])
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

    private func clearMeetingVoiceWatchdog() {
        meetingVoiceWatchdog?.cancel()
        meetingVoiceWatchdog = nil
    }

    private func armMeetingVoiceWatchdog(meetingToken: UInt64, voiceToken: UInt64,
                                         seconds: UInt64, stage: String) {
        clearMeetingVoiceWatchdog()
        meetingVoiceWatchdog = Task { @MainActor [weak self] in
            do { try await Task.sleep(nanoseconds: seconds * 1_000_000_000) }
            catch { return }
            guard let self, self.meetingGeneration.accepts(meetingToken),
                  self.meetingVoiceGeneration.accepts(voiceToken),
                  self.meetingBusy || self.meetingVoiceStarting else { return }
            DiagnosticLog.note("stage=meeting_voice_\(stage) status=timeout")
            self.meetingVoiceWatchdog = nil
            _ = self.meetingVoiceGeneration.begin()
            if let id = self.meetingVoiceASRID { self.bridge?.cancel(id: id) }
            self.meetingVoiceASRID = nil
            self.microphone.cancel()
            self.meetingVoiceStarting = false
            self.meetingVoiceRecording = false
            self.meetingBusy = false
            self.meetingSideAction = nil
            self.meetingSideReleasedDuringStart = false
            self.keyboardGate.clear()
            self.meetingExpressionStatus = "会议语音\(stage == "asr" ? "识别" : "采集")超时，已取消，请重试"
        }
    }

    func toggleMeetingVoice() {
        toggleMeetingVoice(intent: .compose)
    }

    private func toggleMeetingVoice(intent: MeetingVoiceIntent) {
        // Viewing prerecorded demo material must never request a microphone.
        guard !meetingDemoActive else { return }
        if meetingVoiceRecording {
            meetingVoiceRecording = false
            meetingBusy = true
            let token = meetingGeneration.latest
            let voiceToken = meetingVoiceGeneration.latest
            let id = meetingSessionID
            let operation = meetingVoiceIntent.operation
            armMeetingVoiceWatchdog(meetingToken: token, voiceToken: voiceToken,
                                    seconds: 20, stage: "microphone_stop")
            meetingExpressionStatus = "正在识别你的语音…"
            Task { @MainActor [weak self] in
                guard let self else { return }
                do {
                    let pcm = try await self.microphone.stop()
                    guard self.meetingGeneration.accepts(token),
                          self.meetingVoiceGeneration.accepts(voiceToken),
                          self.meetingSessionID == id, let bridge = self.bridge else { return }
                    self.clearMeetingVoiceWatchdog()
                    let config = try self.requestConfig(for: .transcribe)
                    self.armMeetingVoiceWatchdog(meetingToken: token, voiceToken: voiceToken,
                                                 seconds: Self.asrDeadline(for: pcm), stage: "asr")
                    self.meetingVoiceASRID = bridge.sendPCM(operation: "transcribe", pcm: pcm, fields: [
                        "config": config,
                    ]) { [weak self] result in
                        Task { @MainActor [weak self] in
                            guard let self, self.meetingGeneration.accepts(token),
                                  self.meetingVoiceGeneration.accepts(voiceToken),
                                  self.meetingSessionID == id else { return }
                            self.clearMeetingVoiceWatchdog()
                            self.meetingVoiceASRID = nil
                            self.meetingBusy = false
                            guard case .success(let response) = result, response.ok,
                                  let words = response.result?["text"]?.string,
                                  !words.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                                self.meetingExpressionStatus = "语音识别失败或未听到有效内容"
                                return
                            }
                            self.meetingInput = words
                            self.meetingAction(operation, fields: ["instruction": .string(words)])
                        }
                    }
                } catch {
                    guard self.meetingGeneration.accepts(token),
                          self.meetingVoiceGeneration.accepts(voiceToken),
                          self.meetingSessionID == id else { return }
                    self.clearMeetingVoiceWatchdog()
                    self.meetingBusy = false
                    self.meetingExpressionStatus = (error as? MicrophoneError)?.localizedDescription
                        ?? "麦克风录音失败，请检查输入设备与权限"
                }
            }
            return
        }
        guard meetingActive, !meetingBusy, !meetingCancelPending, !meetingVoiceStarting,
              !isRecording, !isStarting, !isProcessing else { return }
        meetingVoiceIntent = intent
        meetingVoiceStarting = true
        meetingExpressionStatus = "正在启动麦克风…"
        let token = meetingGeneration.latest
        let voiceToken = meetingVoiceGeneration.begin()
        armMeetingVoiceWatchdog(meetingToken: token, voiceToken: voiceToken,
                                seconds: 30, stage: "microphone_start")
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
                self.clearMeetingVoiceWatchdog()
                self.meetingVoiceRecording = true
                self.meetingExpressionStatus = intent == .express
                    ? "正在录制需要忠实翻译的中文；松开侧键结束"
                    : "正在录制想表达的内容；松开侧键或再次点击结束"
                if self.meetingSideReleasedDuringStart {
                    self.meetingSideReleasedDuringStart = false
                    self.toggleMeetingVoice()
                } else {
                    self.finishKeyboardStart(intent == .express ? .dictation : .rewrite)
                }
            } catch {
                if self.meetingGeneration.accepts(token),
                   self.meetingVoiceGeneration.accepts(voiceToken) {
                    self.meetingExpressionStatus = (error as? MicrophoneError)?.localizedDescription
                        ?? "无法启动麦克风，请检查权限与设备"
                }
            }
            if self.meetingVoiceGeneration.accepts(voiceToken) {
                self.clearMeetingVoiceWatchdog()
                if !self.meetingVoiceRecording {
                    self.meetingSideAction = nil
                    self.meetingSideReleasedDuringStart = false
                }
                self.meetingVoiceStarting = false
                self.finishKeyboardStart(intent == .express ? .dictation : .rewrite)
                self.refreshMicrophonePermission()
            }
        }
    }

    func cancelMeetingWork() {
        guard meetingActive, !meetingCancelPending,
              let id = meetingSessionID, let bridge else { return }
        let token = meetingGeneration.latest
        let actionToken = meetingActionGeneration.begin()
        clearMeetingVoiceWatchdog()
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

    /// Pause is a native capture boundary as well as a Go Core epoch boundary.
    /// The Go Core rejects late PCM while paused; the host stops ScreenCaptureKit.
    func toggleMeetingCapturePause() {
        guard meetingActive, !meetingStarting, !meetingCaptureSwitching,
              !meetingVoiceRecording, !meetingVoiceStarting,
              let session = meetingSessionID, let bridge else { return }
        let token = meetingGeneration.latest
        meetingCaptureSwitching = true
        meetingPresentationRevision &+= 1
        if !meetingCapturePaused {
            meetingCapturePaused = true
            meetingAudioEpoch = 0
            meetingPendingPackets = 0
            _ = meetingCaptureGeneration.begin()
            systemAudio.cancel()
            meetingStatus = "正在暂停会议音频…"
            Task { @MainActor [weak self] in
                guard let self else { return }
                await self.systemAudio.waitForStop()
                guard self.meetingSessionID == session,
                      self.meetingGeneration.accepts(token), self.meetingActive else { return }
                bridge.send(operation: "meeting_pause", fields: [
                    "session_id": .string(session),
                ]) { [weak self] result in
                    Task { @MainActor [weak self] in
                        guard let self, self.meetingSessionID == session,
                              self.meetingGeneration.accepts(token) else { return }
                        guard case .success(let reply) = result, reply.ok else {
                            self.stopMeeting(reason: "暂停会议音频失败，已停止采集")
                            return
                        }
                        self.meetingCaptureSwitching = false
                        self.applyMeetingSnapshot(reply.result ?? [:])
                        self.meetingStatus = "会议音频已暂停"
                    }
                }
            }
        } else {
            meetingStatus = "正在恢复会议音频…"
            bridge.send(operation: "meeting_resume", fields: [
                "session_id": .string(session),
            ]) { [weak self] result in
                Task { @MainActor [weak self] in
                    guard let self, self.meetingSessionID == session,
                          self.meetingGeneration.accepts(token), self.meetingActive else { return }
                    guard case .success(let reply) = result, reply.ok else {
                        self.stopMeeting(reason: "恢复会议处理失败，已停止采集")
                        return
                    }
                    guard let epoch = reply.result?["epoch"]?.int, epoch > 0 else {
                        self.stopMeeting(reason: "恢复会议处理失败：音频代际无效")
                        return
                    }
                    self.meetingAudioEpoch = epoch
                    self.meetingPendingPackets = 0
                    let capture = self.meetingCaptureGeneration.begin()
                    do {
                        try await self.systemAudio.start(sourceID: self.meetingCaptureSourceID,
                            onPCM: { [weak self] bytes in
                                self?.feedMeeting(bytes, sessionID: session, capture: capture)
                            },
                            onFailure: { [weak self] detail in
                                guard let self, self.meetingSessionID == session,
                                      self.meetingCaptureGeneration.accepts(capture) else { return }
                                self.stopMeeting(reason: detail)
                            })
                        guard self.meetingSessionID == session,
                              self.meetingCaptureGeneration.accepts(capture),
                              self.meetingGeneration.accepts(token), self.meetingActive else { return }
                        self.meetingCaptureSwitching = false
                        self.meetingCapturePaused = false
                        self.applyMeetingSnapshot(reply.result ?? [:])
                        self.meetingStatus = "正在监听所选应用音频"
                    } catch {
                        guard self.meetingSessionID == session,
                              self.meetingCaptureGeneration.accepts(capture),
                              self.meetingGeneration.accepts(token) else { return }
                        let detail = (error as? SystemAudioError)?.localizedDescription
                            ?? "恢复系统音频失败；请检查录制权限并重新选择音频来源"
                        self.stopMeeting(reason: detail)
                    }
                }
            }
        }
    }

    func stopMeeting(reason: String = "会议采集已停止") {
        clearMeetingStartWatchdog()
        clearMeetingVoiceWatchdog()
        let wasDemo = meetingDemoActive
        meetingDemoActive = false
        meetingProfileUpdating = false
        if wasDemo {
            meetingCaptions = []
            meetingReplies = []
            meetingExpression = ""
            meetingExpressionChinese = ""
        }
        // Fence native callbacks synchronously, before a new meeting can start.
        systemAudio.cancel()
        _ = meetingCaptureGeneration.begin()
        meetingAudioEpoch = 0
        meetingCapturePaused = false
        meetingCaptureSwitching = false
        meetingCaptureSourceID = ""
        meetingSideAction = nil
        meetingSideReleasedDuringStart = false
        keyboardGate.clear()
        keyboardHoldSequence &+= 1
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
        if let snapshotID = meetingSnapshotRequestID { bridge?.cancel(id: snapshotID) }
        meetingSnapshotRequestID = nil
        meetingSnapshotStartedAt = 0
        meetingSnapshotPending = false
        meetingStarting = false
        meetingActive = false
        meetingPendingPackets = 0
        meetingStatus = reason
        let id = meetingSessionID
        meetingSessionID = nil
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
                self.onUpdateCheckCompleted?(follow.status, follow.page)
            }
        }
    }

    func openUpdatePage() {
        guard let updatePage, let url = UpdatePagePolicy.officialReleaseURL(updatePage) else { return }
        if !NSWorkspace.shared.open(url) { DiagnosticLog.note("无法打开官方发布页面") }
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

    /// Shared upper bound for the native stop, AX and Helper stages. The Go
    /// service timeout cannot protect a stalled local microphone/AX await.
    static func elapsedMilliseconds(since start: TimeInterval) -> Int {
        max(0, Int(((ProcessInfo.processInfo.systemUptime - start) * 1000).rounded()))
    }

    static func asrDeadline(for pcm: Data) -> UInt64 {
        let duration = (pcm.count + 31_999) / 32_000
        return UInt64(min(220, max(65, 45 + 2 * duration)) + 15)
    }

    private func armProcessingWatchdog(token: UInt64, seconds: UInt64, stage: String) {
        processingWatchdog?.cancel()
        processingWatchdog = Task { @MainActor [weak self] in
            do { try await Task.sleep(nanoseconds: seconds * 1_000_000_000) }
            catch { return }
            guard let self, self.resultGeneration.accepts(token),
                  (self.isProcessing || self.isStarting) else { return }
            DiagnosticLog.note("stage=voice_\(stage) status=timeout")
            self.processingWatchdog = nil
            self.keyboardGate.clear()
            self.keyboardHoldSequence &+= 1
            self.invalidatePending()
            self.microphone.cancel()
            self.isRecording = false
            self.isStarting = false
            self.recordingStatus = "语音处理超时，已结束本次任务；请检查 ASR 服务后重试"
            self.interactionStatus = self.recordingStatus
            self.asrProbeStatus = "ASR：本次请求超时"
            self.showIndicator(phase: .error, mode: self.indicatorSnapshot.mode, pending: false)
            if stage == "dictation_delivery", let text = self.transcription, !text.isEmpty {
                // Never retry a paste with an uncertain outcome. Preserve a
                // copyable result even when native delivery doesn't return.
                ResultCardPanel.shared.present(
                    title: ResultCopy.dictationWriteUnconfirmed, text: text)
            }
        }
    }

    private func finishProcessingWatchdog() {
        processingWatchdog?.cancel()
        processingWatchdog = nil
    }

    private func invalidatePending() {
        dictationJourney = nil
        finishProcessingWatchdog()
        _ = resultGeneration.begin()
        if let id = activeRequestID { bridge?.cancel(id: id) }
        activeRequestID = nil
        activeInsert = nil
        if let old = activeVoiceSelection {
            Task { await interaction.cancelTask(generation: old.generation) }
        }
        if let generation = activeSelectionGeneration {
            Task { await interaction.cancelTask(generation: generation) }
        }
        activeSelectionGeneration = nil
        activeVoiceSelection = nil
        activeVoiceTargetPID = nil
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
        armProcessingWatchdog(token: token, seconds: 30, stage: "microphone_start")
        // Like Windows Hold, capture the selection on release rather than
        // delaying the microphone startup with an AX request on key-down.
        showIndicator(phase: .selection, mode: "改写", pending: false)
        Task { @MainActor [weak self] in
            guard let self else { return }
            defer { self.refreshMicrophonePermission() }
            do {
                try await microphone.start(deviceUID: settings.microphoneUID,
                                           maximumSeconds: settings.maxRecordSeconds)
                guard resultGeneration.accepts(token) else {
                    microphone.cancel()
                    isStarting = false
                    return
                }
                finishProcessingWatchdog()
                activeVoiceTargetPID = pid
                isVoiceSelectionRecording = true
                isRecording = true
                showIndicator(phase: .recording, mode: "改写", pending: false)
                recordingStatus = rewriteRecording()
            } catch {
                guard resultGeneration.accepts(token) else { return }
                microphone.cancel()
                finishProcessingWatchdog()
                interactionStatus = rewriteCaptureStatus(error, password: ResultCopy.passwordSelection)
                showIndicator(phase: .error, mode: "改写", pending: false)
            }
            isStarting = false
            stopWhenSideMouseReleased(.rewrite)
            finishKeyboardStart(.rewrite)
        }
    }

    private func startRecording(targetPID: Int32?, pressedAt: TimeInterval? = nil) {
        guard !meetingVoiceRecording && !meetingVoiceStarting else { return }
        invalidatePending()
        let now = ProcessInfo.processInfo.systemUptime
        let onset = pressedAt.flatMap { $0 > 0 && $0 <= now ? $0 : nil } ?? now
        dictationJourney = DictationJourney(triggeredAt: onset)
        DiagnosticLog.note("journey=dictation event=hold_callback elapsed_ms=\(Self.elapsedMilliseconds(since: onset))")
        let token = resultGeneration.begin()
        isStarting = true
        armProcessingWatchdog(token: token, seconds: 30, stage: "microphone_start")
        recordingStatus = dictationRequestingMicrophone()
        // A pending pill must not wait for AX focus probes or AVAudioEngine start.
        // It becomes an active recording indicator only after the engine starts.
        showIndicator(phase: .recording, mode: "听写", pending: true)
        Task { @MainActor [weak self] in
            guard let self else { return }
            defer { self.refreshMicrophonePermission() }
            let startupStarted = ProcessInfo.processInfo.systemUptime
            var captured = false
            var failure: String?
            var snapshot: MacDictationTarget?
            if let pid = targetPID, pid > 0 {
                do {
                    snapshot = try await dictationDelivery.captureAsync(processID: pid)
                    captured = true
                } catch {
                    guard resultGeneration.accepts(token) else { return }
                    DiagnosticLog.note("stage=dictation_capture status=failed code=\(insertionDiagnosticCode(error))")
                    failure = captureStatus(error, password: ResultCopy.passwordDictation)
                }
            }
            guard resultGeneration.accepts(token) else { return }
            DiagnosticLog.note("stage=dictation_target status=done elapsed_ms=\(Self.elapsedMilliseconds(since: startupStarted))")
            let arm = dictationArm(hasTarget: (targetPID ?? 0) > 0, tokenValid: resultGeneration.accepts(token), captured: captured, captureFailure: failure)
            if let status = arm.interactionStatus { interactionStatus = status }
            if arm.keepInsert, let snapshot {
                activeInsert = snapshot
            }
            guard arm.startMicrophone else {
                guard resultGeneration.accepts(token) else { return }
                finishProcessingWatchdog()
                isStarting = false
                finishKeyboardStart(.dictation)
                return
            }
            do {
                try await microphone.start(deviceUID: settings.microphoneUID,
                                               maximumSeconds: settings.maxRecordSeconds)
                let ready = dictationMicrophoneReady(started: true, tokenValid: resultGeneration.accepts(token), failure: "")
                if ready.cancelMicrophone {
                    microphone.cancel()
                    return
                }
                finishProcessingWatchdog()
                DiagnosticLog.note("stage=microphone_start status=done elapsed_ms=\(Self.elapsedMilliseconds(since: startupStarted))")
                if let journey = dictationJourney {
                    DiagnosticLog.note("journey=dictation event=recording_ready elapsed_ms=\(Self.elapsedMilliseconds(since: journey.triggeredAt))")
                }
                isRecording = true
                showIndicator(phase: .recording, mode: "听写", pending: false)
                if let status = ready.recordingStatus { recordingStatus = status }
            } catch {
                guard resultGeneration.accepts(token) else { return }
                finishProcessingWatchdog()
                let ready = dictationMicrophoneReady(started: false, tokenValid: true, failure: error.localizedDescription)
                if let status = ready.recordingStatus { recordingStatus = status }
                if ready.showError {
                    showIndicator(phase: .error, mode: "听写", pending: false)
                }
            }
            guard resultGeneration.accepts(token) else { return }
            isStarting = false
            stopWhenSideMouseReleased(.dictation)
            finishKeyboardStart(.dictation)
        }
    }

    func stopRecording() {
        guard isRecording else { return }
        if dictationJourney != nil {
            dictationJourney?.releasedAt = ProcessInfo.processInfo.systemUptime
        }
        sideGate.clear()
        keyboardGate.clear()
        keyboardHoldSequence &+= 1
        isRecording = false
        isProcessing = true
        showIndicator(phase: .processing, mode: indicatorSnapshot.mode, pending: false)
        recordingStatus = dictationProcessing()
        armProcessingWatchdog(token: resultGeneration.latest, seconds: 20, stage: "capture_stop")
        let token = resultGeneration.latest
        let insertion = activeInsert
        let voiceTargetPID = activeVoiceTargetPID
        activeInsert = nil
        activeVoiceTargetPID = nil
        isVoiceSelectionRecording = false
        Task { @MainActor [weak self] in
            guard let self else { return }
            var selectionGeneration: UInt64?
            let stopStarted = ProcessInfo.processInfo.systemUptime
            do {
                let pcm = try await microphone.stop()
                guard resultGeneration.accepts(token) else { return }
                DiagnosticLog.note("stage=microphone_stop status=done elapsed_ms=\(Self.elapsedMilliseconds(since: stopStarted))")
                armProcessingWatchdog(token: token,
                    seconds: voiceTargetPID == nil ? Self.asrDeadline(for: pcm) : 375,
                    stage: voiceTargetPID == nil ? "asr" : "spoken_edit")
                let route = recordingDispatch(tokenValid: resultGeneration.accepts(token), voiceSelection: voiceTargetPID != nil)
                guard route.send else { return }
                if route.cancelProbes { cancelProbes() }
                if let status = route.asrProbe { asrProbeStatus = status }
                if let status = route.llmProbe { llmProbeStatus = status }
                if route.spoken, let voiceTargetPID {
                    // The release PID must still be the original application.
                    // InteractionSession then captures and monitors the *actual*
                    // selected element before the model is ever called.
                    guard voiceSelectionReleaseMayCapture(
                        tokenValid: resultGeneration.accepts(token),
                        targetPID: voiceTargetPID,
                        foregroundPID: NSWorkspace.shared.frontmostApplication?.processIdentifier) else {
                        throw InteractionFailure.applicationChanged
                    }
                    interactionStatus = rewriteCapturingSelection()
                    armProcessingWatchdog(token: token, seconds: 20, stage: "selection_capture")
                    let generation = try await interaction.beginTask(targetProcessID: voiceTargetPID)
                    selectionGeneration = generation
                    activeSelectionGeneration = generation
                    let snapshot = try await interaction.captureSelection(
                        generation: generation, intent: .replaceSelection)
                    guard resultGeneration.accepts(token) else {
                        await interaction.cancelTask(generation: generation)
                        return
                    }
                    if let tooLong = SelectionLimit.tooLongMessage(limit: settings.maxSelectedRunes, selected: snapshot.originalSelectedText) {
                        await interaction.cancelTask(generation: generation)
                        isProcessing = false
                        interactionStatus = tooLong
                        showIndicator(phase: .error, mode: "改写", pending: false)
                        return
                    }
                    activeVoiceSelection = (snapshot, generation)
                    armProcessingWatchdog(token: token, seconds: 375, stage: "spoken_edit")
                    let config = try requestConfig(for: .spokenSelectedTask)
                    guard let bridge else { throw BridgeFailure.notRunning }
                    let id = bridge.sendPCM(operation: "selected_voice_task", pcm: pcm, fields: [
                        "config": config,
                        "selected_text": .string(snapshot.originalSelectedText),
                    ]) { [weak self] reply in
                        Task { @MainActor [weak self] in
                            guard let self else { return }
                            self.handleSelectedTaskReply(reply, snapshot: snapshot,
                                generation: generation, token: token, spoken: true)
                        }
                    }
                    activeRequestID = id
                    return
                }
                let config = try requestConfig(for: .transcribe)
                guard let bridge else { throw BridgeFailure.notRunning }
                let requestStarted = ProcessInfo.processInfo.systemUptime
                let id = bridge.sendPCM(operation: "transcribe", pcm: pcm, fields: [
                    "config": config,
                ]) { [weak self] reply in
                    Task { @MainActor [weak self] in
                        guard let self, self.resultGeneration.accepts(token) else { return }
                        self.activeRequestID = nil
                        self.finishProcessingWatchdog()
                        self.isProcessing = false
                        DiagnosticLog.note("stage=asr_reply status=finished elapsed_ms=\(Self.elapsedMilliseconds(since: requestStarted))")
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
                if failed.stopProcessing {
                    finishProcessingWatchdog()
                    isProcessing = false
                }
                applyTask(recordingStopFollowUp(detail: error.localizedDescription,
                    voiceSelection: voiceTargetPID != nil, mode: indicatorSnapshot.mode),
                    generation: selectionGeneration)
                if voiceTargetPID != nil {
                    interactionStatus = rewriteCaptureStatus(error, password: ResultCopy.passwordSelection)
                }
            }
        }
    }

    private func triggerSelectionRewrite(from targetPID: Int32?) {
        if useTypedRewriteInstruction {
            processSelectedText(from: targetPID)
        } else {
            toggleVoiceSelectedText(from: targetPID)
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
        armProcessingWatchdog(token: token, seconds: 20, stage: "selection_capture")
        interactionStatus = rewriteCheckingSelection()
        showIndicator(phase: .selection, mode: "改写", pending: false)
        let instruction = taskInstruction
        Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let generation = try await interaction.beginTask(targetProcessID: pid)
                guard resultGeneration.accepts(token) else {
                    await interaction.cancelTask(generation: generation)
                    return
                }
                activeSelectionGeneration = generation
                let snapshot = try await interaction.captureSelection(
                    generation: generation, intent: .replaceSelection)
                let step = typedSelectionAfterCapture(
                    tokenValid: resultGeneration.accepts(token),
                    tooLong: SelectionLimit.tooLongMessage(limit: settings.maxSelectedRunes, selected: snapshot.originalSelectedText))
                if step.cancelTask {
                    await cancelSelectionCue(step, generation: generation)
                }
                presentSelectionCue(step)
                guard step.sendTask else { return }
                cancelProbes()
                llmProbeStatus = probeRewritingSelection()
                showIndicator(phase: .rewriting, mode: "改写", pending: false)
                let config = try requestConfig(for: .selectedTask)
                armProcessingWatchdog(token: token, seconds: 165, stage: "selected_edit")
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

    private func cancelSelectionCue(_ cue: SelectionCue, generation: UInt64?) async {
        if cue.cancelTask, let generation {
            if activeSelectionGeneration == generation { activeSelectionGeneration = nil }
            await interaction.cancelTask(generation: generation)
        }
    }

    private func presentSelectionCue(_ cue: SelectionCue) {
        if cue.stopProcessing {
            finishProcessingWatchdog()
            isProcessing = false
        }
        if let status = cue.interactionStatus {
            interactionStatus = status
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
        finishProcessingWatchdog()
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
        if !spoken, let body = follow.replaceText {
            // An explicit typed edit is reviewed before its first write. Only a
            // Go-approved replacement (never a preview/answer) receives Apply.
            // Spoken edits preserve the Windows immediate-replacement workflow.
            var review = follow
            review.replaceText = nil
            review.cardText = body
            review.phase = .answer
            applyTask(review, generation: generation, token: token,
                      manualReplacement: (snapshot, generation, token))
        } else {
            applyTask(follow, generation: generation, token: token,
                      replacement: (snapshot, generation))
        }
    }

    private func applyHelperConnection(_ event: HelperConnection) {
        helperStartupWatchdog?.cancel()
        helperStartupWatchdog = nil
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
            let abandonedVoice = isProcessing || isStarting || isRecording
            let abandonedMode = indicatorSnapshot.mode
            invalidatePending()
            if abandonedVoice {
                // A disconnected Helper cannot complete the old operation.
                // Never leave its nonterminal "processing" pill on screen after
                // invalidating the generation and cancelling its watchdog.
                keyboardGate.clear()
                keyboardHoldSequence &+= 1
                microphone.cancel()
                isRecording = false
                isStarting = false
                recordingStatus = "语音处理已中断：后台服务断开"
                showIndicator(phase: .error, mode: abandonedMode, pending: false)
            }
            invalidateDiscoveredModels(.transcribe)
            invalidateDiscoveredModels(.selectedTask)
        }
        if effect.logServiceStatus {
            DiagnosticLog.note(serviceStatus)
        }
        // The --meeting-demo path must not run even background ASR/LLM
        // endpoint probes. The bundled fixture is genuinely offline.
        if effect.checkServices && !showStartupDemoOnConnect {
            checkServices()
        }
        if effect.refreshShortcuts {
            refreshShortcutsFromCore()
        }
        if effect.connectedLabel != nil && showStartupDemoOnConnect {
            startMeetingDemo()
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
        replacement: (snapshot: SelectionSnapshot, generation: UInt64)? = nil,
        manualReplacement: (snapshot: SelectionSnapshot, generation: UInt64, token: UInt64)? = nil
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
            var action: ResultCardApplyAction?
            if let manualReplacement, !follow.cancelTask,
               follow.replaceText == nil, resultGeneration.accepts(manualReplacement.token) {
                let snapshot = manualReplacement.snapshot
                let generation = manualReplacement.generation
                let token = manualReplacement.token
                action = ResultCardApplyAction(
                    isBusy: { [weak self] in
                        guard let self else { return true }
                        return self.isProcessing || self.isRecording || self.isStarting ||
                               self.meetingBusy || self.meetingVoiceRecording || self.voicePaused
                    },
                    perform: { [weak self] in
                        guard let self, self.resultGeneration.accepts(token) else {
                            throw ResultCardApplyFailure.unsafe
                        }
                        guard NSRunningApplication(processIdentifier: snapshot.processID) != nil else {
                            throw ResultCardApplyFailure.editorClosed
                        }
                        self.isProcessing = true
                        defer {
                            if self.resultGeneration.accepts(token) { self.isProcessing = false }
                        }
                        do {
                            // InteractionSession consumes the captured authorization
                            // *before* the synchronous native verification/write.
                            try await self.interaction.replaceIfCurrent(
                                snapshot, with: text, generation: generation)
                            // A cancellation racing the OS write must never
                            // re-certify a stale card or start another send.
                            guard self.resultGeneration.accepts(token) else {
                                throw ResultCardApplyFailure.unconfirmed
                            }
                            self.interactionStatus = rewriteReplaced()
                        } catch {
                            if self.resultGeneration.accepts(token) {
                                self.interactionStatus = insertionStatus(
                                    error, rewrite: true,
                                    passwordRewrite: ResultCopy.passwordRewrite,
                                    passwordDictation: ResultCopy.passwordDictation)
                            }
                            throw error
                        }
                    },
                    cancel: { [weak self] in
                        guard let self else { return }
                        Task { await self.interaction.cancelTask(generation: generation) }
                    })
            }
            ResultCardPanel.shared.present(title: title, text: text, applyAction: action)
        }
        if follow.cancelTask, let generation {
            Task { await interaction.cancelTask(generation: generation) }
        }
        if let text = follow.insertText, let insertion, let token {
            Task { @MainActor [weak self] in
                guard let self, self.resultGeneration.accepts(token) else { return }
                self.isProcessing = true
                self.armProcessingWatchdog(token: token, seconds: 20, stage: "dictation_delivery")
                do {
                    let outcome = try await self.dictationDelivery.deliver(text, to: insertion, stillValid: {
                        self.resultGeneration.accepts(token)
                    })
                    guard self.resultGeneration.accepts(token) else { return }
                    self.finishProcessingWatchdog()
                    self.isProcessing = false
                    DiagnosticLog.note("stage=dictation_delivery status=\(outcome)")
                    if let journey = self.dictationJourney, let released = journey.releasedAt {
                        DiagnosticLog.note("journey=dictation event=delivery_returned elapsed_ms=\(Self.elapsedMilliseconds(since: released)) total_since_key_down_ms=\(Self.elapsedMilliseconds(since: journey.triggeredAt)) outcome=\(outcome)")
                    }
                    self.dictationJourney = nil
                    self.applyTask(dictationInsertFollowUp(
                        outcome: outcome,
                        status: insertionStatus(InteractionFailure.writeNotConfirmed, rewrite: false,
                                                passwordRewrite: ResultCopy.passwordRewrite,
                                                passwordDictation: ResultCopy.passwordDictation),
                        manualTitle: ResultCopy.dictationWriteUnconfirmed, text: text))
                } catch {
                    guard self.resultGeneration.accepts(token) else { return }
                    self.finishProcessingWatchdog()
                    self.isProcessing = false
                    DiagnosticLog.note("stage=dictation_delivery status=failed code=\(insertionDiagnosticCode(error))")
                    if let journey = self.dictationJourney, let released = journey.releasedAt {
                        DiagnosticLog.note("journey=dictation event=delivery_failed elapsed_ms=\(Self.elapsedMilliseconds(since: released)) total_since_key_down_ms=\(Self.elapsedMilliseconds(since: journey.triggeredAt))")
                    }
                    self.dictationJourney = nil
                    self.applyTask(dictationInsertFollowUp(
                        outcome: (error as? InteractionFailure) == .writeNotConfirmed ? .uncertain : .blocked,
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
                self.isProcessing = true
                self.armProcessingWatchdog(token: token, seconds: 20, stage: "selected_write")
                do {
                    try await self.interaction.replaceIfCurrent(replacement.snapshot, with: text, generation: replacement.generation)
                    guard self.resultGeneration.accepts(token) else { return }
                    self.finishProcessingWatchdog()
                    self.isProcessing = false
                    self.applyTask(selectedWriteFollowUp(succeeded: true, status: "", cardTitle: title, text: text))
                } catch {
                    guard self.resultGeneration.accepts(token) else { return }
                    self.finishProcessingWatchdog()
                    self.isProcessing = false
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
        dictationJourney = nil
        dismissIndicator()
        keyboardGate.clear()
        keyboardHoldSequence &+= 1
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

    #if DEBUG
    func pendingRecordingIndicatorForCI() -> (count: UInt64, panel: NSPanel?) {
        (recordingIndicator.pendingRecordingPresentationCount,
         recordingIndicator.panelForCI)
    }
    #endif

    private func showIndicator(phase: IndicatorPhase, mode: String, pending: Bool) {
        let now = Date()
        let timing = indicatorTiming(wasVisible: indicatorSnapshot.visible, wasPending: indicatorSnapshot.pending, wasPhase: indicatorSnapshot.phase.rawValue, pending: pending, phase: phase.rawValue, now: now, started: indicatorSnapshot.started, changed: indicatorSnapshot.changed)
        indicatorSnapshot.started = timing.started
        indicatorSnapshot.changed = timing.changed
        indicatorSnapshot.visible = true
        indicatorSnapshot.pending = pending
        indicatorSnapshot.phase = phase
        indicatorSnapshot.mode = mode
        if !pending {
            menuIndicatorPhase = phase
            menuIndicatorMode = mode
        }
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
            self?.menuIndicatorPhase = nil
        })
        // This is AppKit orderFront completion (not a physical photodiode
        // measurement). The CI additionally calls displayIfNeeded on this view.
        if phase == .recording, pending, var journey = dictationJourney,
           !journey.indicatorLogged {
            journey.indicatorLogged = true
            dictationJourney = journey
            DiagnosticLog.note("journey=dictation event=indicator_ordered elapsed_ms=\(Self.elapsedMilliseconds(since: journey.triggeredAt))")
        }
    }

    private func dismissIndicator() {
        menuIndicatorPhase = nil
        indicatorSnapshot.visible = false
        indicatorSnapshot.pending = false
        recordingIndicator.dismiss()
    }

    func shutdown() {
        _ = helloGeneration.begin()
        _ = shortcutGeneration.begin()
        // Tear down Meeting before closing the Helper; native ScreenCaptureKit
        // callbacks must not outlive the app's session and IPC ownership.
        if meetingActive || meetingStarting || meetingSessionID != nil {
            stopMeeting()
        } else {
            systemAudio.cancel()
        }
        cancelProbes()
        cancelCurrent()
        sideMouse?.shutdown()
        sideMouse = nil
        sideMouseStatus = sideMouseStopped()
        hotKeys?.shutdown()
        hotKeyDriver?.shutdown()
        rightModifierDriver?.shutdown()
        rightModifierDriver = nil
        keyboardHID?.stop()
        keyboardHID = nil
        hotKeys = nil
        hotKeyDriver = nil
        activeShortcutPlan = nil
        bridge?.stop()
        bridge = nil
        connection = .disconnected
        Task { await interaction.shutdown() }
    }
}
