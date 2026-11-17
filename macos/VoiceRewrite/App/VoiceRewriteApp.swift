import SwiftUI
import AppKit
import Carbon.HIToolbox

enum MeetingDemoLaunch {
    static func requested(arguments: [String]) -> Bool {
        arguments.dropFirst().contains("--meeting-demo")
    }
}

/// An independent *quit* action, not a third dictation/rewrite shortcut.
/// Use a distinct Carbon ID so the voice-hotkey handler cannot treat this event
/// as one of its two registered voice actions.
private let exitHotKeySignature: OSType = 0x5652_4558 // 'VREX'
private let exitHotKeyID: UInt32 = 0xFFFF_FF10
private let exitHotKeyHandler: EventHandlerUPP = { _, event, _ in
    guard let event else { return eventNotHandledErr }
    var identifier = EventHotKeyID()
    let status = GetEventParameter(event, EventParamName(kEventParamDirectObject),
                                   EventParamType(typeEventHotKeyID), nil,
                                   MemoryLayout<EventHotKeyID>.size, nil, &identifier)
    guard status == noErr else { return eventNotHandledErr }
    // Leave the two voice shortcuts to their own Carbon handler.
    guard identifier.signature == exitHotKeySignature,
          identifier.id == exitHotKeyID else { return eventNotHandledErr }
    DispatchQueue.main.async { NSApp.terminate(nil) }
    return noErr
}

@MainActor
final class GlobalExitHotKey {
    static let defaultBinding = "Ctrl+Alt+F10"
    private var eventHandler: EventHandlerRef?
    private var hotKey: EventHotKeyRef?

    /// The settings owner can pass the saved canonical binding here once
    /// the independently configurable exit setting is added.
    func register(canonical: String = defaultBinding) -> Bool {
        guard let binding = ShortcutBinding(canonical: canonical),
              !binding.mouse,
              let shortcut = MacShortcut.hotKey(key: binding.key, ctrl: binding.ctrl,
                                                alt: binding.alt, shift: binding.shift) else { return false }
        stop()
        var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard),
                                 eventKind: UInt32(kEventHotKeyPressed))
        var handler: EventHandlerRef?
        let installed = InstallEventHandler(GetApplicationEventTarget(), exitHotKeyHandler,
                                            1, &spec, nil, &handler)
        guard installed == noErr, let handler else { return false }
        eventHandler = handler

        var flags: UInt32 = 0
        if shortcut.modifiers.contains(.control) { flags |= UInt32(controlKey) }
        if shortcut.modifiers.contains(.option) { flags |= UInt32(optionKey) }
        if shortcut.modifiers.contains(.shift) { flags |= UInt32(shiftKey) }
        let identifier = EventHotKeyID(signature: exitHotKeySignature, id: exitHotKeyID)
        var registration: EventHotKeyRef?
        let registered = RegisterEventHotKey(shortcut.keyCode, flags, identifier,
                                             GetApplicationEventTarget(), 0, &registration)
        guard registered == noErr, let registration else {
            stop()
            return false
        }
        hotKey = registration
        return true
    }

    func stop() {
        if let hotKey { UnregisterEventHotKey(hotKey) }
        hotKey = nil
        if let eventHandler { RemoveEventHandler(eventHandler) }
        eventHandler = nil
    }
}


/// The model is owned by the application, not by an optional workspace window.
@MainActor
private final class VoiceRewriteRuntime {
    static let shared = VoiceRewriteRuntime()
    let model = WorkspaceModel()
}

/// The menu bar is the primary application surface; windows are optional.
@MainActor
final class AppLifecycleDelegate: NSObject, NSApplicationDelegate {
    private(set) var didLaunchWorkspace = false
    private let exitHotKey = GlobalExitHotKey()

    func launchWorkspaceOnce(_ launch: () -> Void) {
        guard !didLaunchWorkspace else { return }
        didLaunchWorkspace = true
        launch()
    }

    func applicationWillFinishLaunching(_ notification: Notification) {
        // A normal launch must not activate an empty dashboard over the editor.
        NSApp.setActivationPolicy(.accessory)
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        launchWorkspaceOnce {
            let model = VoiceRewriteRuntime.shared.model
            model.onUpdateCheckCompleted = { [weak self] status, page in
                self?.presentUpdateCheck(status: status, page: page)
            }
            model.launch(demo: MeetingDemoLaunch.requested(arguments: CommandLine.arguments))
            if !exitHotKey.register(canonical: model.settings.exitShortcut) {
                DiagnosticLog.note("stage=global_exit_hotkey status=failed")
            }
        }
        // macOS 14 automatically constructs a SwiftUI Window scene on launch.
        // Order it out; an explicit menu click or Finder reopen shows it again.
        DispatchQueue.main.async {
            for window in NSApp.windows where window.title == "VoiceRewrite" && !(window is NSPanel) {
                window.orderOut(nil)
            }
        }
    }

    func applicationDidBecomeActive(_ notification: Notification) {
        let model = VoiceRewriteRuntime.shared.model
        model.refreshSideMouse()
        model.refreshMicrophonePermission()
    }

    func applicationWillTerminate(_ notification: Notification) {
        exitHotKey.stop()
        VoiceRewriteRuntime.shared.model.shutdown()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        // The last workspace/settings window can close while the status item lives on.
        DispatchQueue.main.async {
            let hasDocumentWindows = NSApp.windows.contains { $0.isVisible && !($0 is NSPanel) }
            if !hasDocumentWindows { NSApp.setActivationPolicy(.accessory) }
        }
        return false
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows: Bool) -> Bool {
        guard !hasVisibleWindows,
              let window = sender.windows.first(where: { $0.title == "VoiceRewrite" && !($0 is NSPanel) }) else {
            return true
        }
        sender.setActivationPolicy(.regular)
        window.makeKeyAndOrderFront(nil)
        sender.activate(ignoringOtherApps: true)
        return false
    }

    private func presentUpdateCheck(status: String, page: String?) {
        // Completion may arrive after the user closes the menu or workspace.
        // Only user-initiated checks reach this callback.
        DispatchQueue.main.async {
            let alert = NSAlert()
            alert.messageText = page == nil ? "检查更新结果" : "发现新版本"
            alert.informativeText = status
            if page != nil {
                alert.addButton(withTitle: "打开官方发布页面")
                alert.addButton(withTitle: "取消")
            } else {
                alert.addButton(withTitle: "确定")
            }
            NSApp.activate(ignoringOtherApps: true)
            if alert.runModal() == .alertFirstButtonReturn && page != nil {
                VoiceRewriteRuntime.shared.model.openUpdatePage()
            }
        }
    }
}

/// Never open arbitrary URLs returned by a helper process.
enum UpdatePagePolicy {
    static func officialReleaseURL(_ value: String) -> URL? {
        guard let url = URLComponents(string: value),
              url.scheme == "https",
              url.host == "github.com",
              url.user == nil, url.password == nil, url.port == nil,
              url.query == nil, url.fragment == nil,
              (url.path == "/xujinheng/windows-voice-rewrite/releases" ||
               url.path.hasPrefix("/xujinheng/windows-voice-rewrite/releases/")),
              let result = url.url else { return nil }
        return result
    }
}

/// A missing settings.json means the user has not completed initial setup.
/// Checking it does not create a second settings source of truth.
enum InitialSetup {
    static func required(settingsFile: URL) -> Bool {
        !FileManager.default.fileExists(atPath: settingsFile.path)
    }

    static var required: Bool {
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return required(settingsFile: support.appendingPathComponent("VoiceRewrite/settings.json"))
    }
}

enum MenuBarStatus {
    static func title(paused: Bool, recording: Bool, starting: Bool,
                      processing: Bool, meetingActive: Bool,
                      phase: IndicatorPhase? = nil, mode: String = "听写",
                      connectionFailed: Bool = false) -> String {
        switch phase {
        case .success: return "语音任务完成"
        case .answer: return "已显示 AI 结果"
        case .fallback: return "结果已保留 · 可复制"
        case .error: return "语音任务失败"
        case .rewriting, .applying: return "正在改写"
        case .selection: return "正在读取选区"
        case .processing: return mode == "改写" ? "正在识别指令" : "正在识别"
        default: break
        }
        if processing { return "正在处理语音" }
        if recording { return "正在录音" }
        if starting { return "正在启动录音" }
        if meetingActive && paused { return "会议进行中 · 普通语音已暂停" }
        if meetingActive { return "会议进行中" }
        if paused { return "普通语音已暂停" }
        return connectionFailed ? "服务连接失败 · 查看诊断" : "待命"
    }

    static func symbol(paused: Bool, recording: Bool, starting: Bool,
                       processing: Bool, meetingActive: Bool, connectionFailed: Bool,
                       phase: IndicatorPhase? = nil) -> String {
        switch phase {
        case .success, .answer: return "checkmark.circle.fill"
        case .fallback: return "doc.on.doc.fill"
        case .error: return "xmark.octagon.fill"
        case .rewriting, .applying: return "arrow.triangle.2.circlepath.circle.fill"
        case .processing, .selection: return "ellipsis.circle.fill"
        default: break
        }
        if recording { return "mic.fill" }
        if starting { return "mic.badge.plus" }
        if processing { return "ellipsis.circle.fill" }
        if paused { return "pause.circle.fill" }
        if connectionFailed { return "exclamationmark.triangle.fill" }
        if meetingActive { return "person.2.wave.2.fill" }
        return "waveform"
    }
}

private struct MenuBarControlCenter: View {
    @ObservedObject var model: WorkspaceModel
    @Environment(\.openWindow) private var openWindow

    private var busy: Bool { model.isRecording || model.isStarting || model.isProcessing }
    private var connectionFailed: Bool {
        if case .failed = model.connection { return true }
        return false
    }

    var body: some View {
        Text("状态：\(MenuBarStatus.title(paused: model.voicePaused, recording: model.isRecording, starting: model.isStarting, processing: model.isProcessing, meetingActive: model.meetingActive, phase: model.menuIndicatorPhase, mode: model.menuIndicatorMode, connectionFailed: connectionFailed))")
        if InitialSetup.required {
            Text("首次使用：请先在设置中配置 ASR / LLM 服务")
        }
        Button("打开 VoiceRewrite") { showWorkspace() }
        Button("打开会议阅读窗口") { MeetingReaderWindowController.shared.open(model: model) }
        if !model.meetingActive && !model.meetingStarting {
            Button("刷新会议音频来源") { model.refreshMeetingSources() }
            Menu("从音频来源开始会议") {
                ForEach(model.meetingSources) { source in
                    Button(source.name) {
                        model.meetingSourceID = source.id
                        model.startMeeting()
                    }
                }
            }
            .disabled(model.meetingSources.isEmpty)
        }
        if !model.meetingActive && !model.meetingStarting {
            Button("查看会议模式演示（离线数据）") {
                model.startMeetingDemo()
                MeetingReaderWindowController.shared.open(model: model)
            }
        }
        if model.meetingActive || model.meetingStarting {
            Button(model.meetingDemoActive ? "退出离线演示" : "停止会议采集") { model.stopMeeting() }
        }
        Divider()
        Button(model.voicePaused ? "恢复普通语音" : "暂停普通语音") {
            model.toggleVoicePause()
        }
        .disabled(busy)
        Button("取消当前语音任务") { model.cancelCurrent() }
            .disabled(!busy)
        Menu("键盘录音模式") {
            Button(model.keyboardHoldEnabled ? "✓ 按住录音，松开结束" : "按住录音，松开结束") {
                model.setKeyboardHoldEnabled(true)
            }
            Button(model.keyboardHoldEnabled ? "点击开始／再次点击结束" : "✓ 点击开始／再次点击结束") {
                model.setKeyboardHoldEnabled(false)
            }
        }
        .disabled(busy || model.meetingVoiceRecording || model.meetingVoiceStarting)
        Divider()
        Button("恢复最近结果") {
            if let entry = model.sessionHistory.first {
                model.showSessionHistory(id: entry.id)
            }
        }
        .disabled(model.sessionHistory.isEmpty)
        Menu(SessionHistory.sectionTitle) {
            if model.sessionHistory.isEmpty {
                Text(SessionHistory.emptySession)
            } else {
                ForEach(Array(model.sessionHistory.enumerated()), id: \.element.id) { index, entry in
                    Button(SessionHistory.menuLine(index: index, title: entry.title, createdAt: entry.createdAt)) {
                        model.showSessionHistory(id: entry.id)
                    }
                }
            }
        }
        Button("关闭当前 AI 结果窗") { ResultCardPanel.shared.dismiss() }
        Button(SessionHistory.clearSession) { model.clearSessionHistory() }
            .disabled(model.sessionHistory.isEmpty)
        Divider()
        Button("最近错误与解决方法…") { showRecentError() }
        Button("查看诊断日志") { DiagnosticLog.reveal() }
        Menu("权限与故障恢复") {
            Button("重新检测辅助功能权限") { model.refreshSideMouse() }
            Button("请求辅助功能权限…") { model.requestAccessibility() }
            Button("打开麦克风权限设置…") { model.openMicrophonePrivacySettings() }
            Button("重新检测麦克风权限") { model.refreshMicrophonePermission() }
        }
        Button("检查更新…") { model.checkForUpdate() }
            .disabled(model.isCheckingUpdate)
        if model.isCheckingUpdate || model.updatePage != nil {
            Text(model.updateStatus)
            if model.updatePage != nil {
                Button("打开新版本发布页面") { model.openUpdatePage() }
            }
        }
        SettingsLink { Text("设置…") }
        Divider()
        Button("退出 VoiceRewrite") { NSApp.terminate(nil) }
    }

    private func showWorkspace() {
        NSApp.setActivationPolicy(.regular)
        openWindow(id: "workspace")
        NSApp.activate(ignoringOtherApps: true)
    }

    private func showRecentError() {
        // Present after the status menu closes so AppKit can own the modal alert.
        DispatchQueue.main.async {
            let alert = NSAlert()
            alert.messageText = "最近错误与解决方法"
            if let latest = DiagnosticLog.latestError() {
                alert.informativeText = "\(latest)\n\n请检查服务连接、麦克风及辅助功能权限。详细信息请查看诊断日志。"
            } else {
                alert.informativeText = "尚未记录错误。如遇到问题，请打开诊断日志检查详情。"
            }
            alert.addButton(withTitle: "查看诊断日志")
            alert.addButton(withTitle: "关闭")
            NSApp.activate(ignoringOtherApps: true)
            if alert.runModal() == .alertFirstButtonReturn { DiagnosticLog.reveal() }
        }
    }
}

@main
struct VoiceRewriteApp: App {
    @NSApplicationDelegateAdaptor(AppLifecycleDelegate.self) private var lifecycle
    @StateObject private var workspace = VoiceRewriteRuntime.shared.model

    private var menuSymbol: String {
        let connectionFailed: Bool
        if case .failed = workspace.connection { connectionFailed = true }
        else { connectionFailed = false }
        return MenuBarStatus.symbol(
            paused: workspace.voicePaused, recording: workspace.isRecording,
            starting: workspace.isStarting, processing: workspace.isProcessing,
            meetingActive: workspace.meetingActive, connectionFailed: connectionFailed,
            phase: workspace.menuIndicatorPhase)
    }

    var body: some Scene {
        Window("VoiceRewrite", id: "workspace") {
            MainView(model: workspace)
                .frame(minWidth: 670, minHeight: 510)
        }
        .defaultSize(width: 840, height: 600)
        .commands {
            CommandGroup(replacing: .appSettings) {
                SettingsLink { Text("设置…") }
                    .keyboardShortcut(",", modifiers: .command)
            }
        }

        MenuBarExtra("VoiceRewrite", systemImage: menuSymbol) {
            MenuBarControlCenter(model: workspace)
        }

        Settings {
            SettingsView(model: workspace)
                .frame(width: 540)
        }
    }
}
