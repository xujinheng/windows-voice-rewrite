import SwiftUI
import AppKit

final class AppLifecycleDelegate: NSObject, NSApplicationDelegate {
    var onTerminate: (() -> Void)?
    var onActivate: (() -> Void)?
    private(set) var didLaunchWorkspace = false

    func launchWorkspaceOnce(_ launch: () -> Void) {
        guard !didLaunchWorkspace else { return }
        didLaunchWorkspace = true
        launch()
    }

    func applicationDidBecomeActive(_ notification: Notification) { onActivate?() }
    func applicationWillTerminate(_ notification: Notification) { onTerminate?() }

    // Closing the workspace must not stop global shortcuts or the menu bar controller.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows: Bool) -> Bool { true }
}

enum MenuBarStatus {
    static func title(paused: Bool, recording: Bool, starting: Bool,
                      processing: Bool, meetingActive: Bool) -> String {
        if processing { return "正在处理语音" }
        if recording { return "正在录音" }
        if starting { return "正在启动录音" }
        if meetingActive && paused { return "会议进行中 · 普通语音已暂停" }
        if meetingActive { return "会议进行中" }
        return paused ? "普通语音已暂停" : "待命"
    }

    static func symbol(paused: Bool, recording: Bool, starting: Bool,
                       processing: Bool, meetingActive: Bool, connectionFailed: Bool) -> String {
        if connectionFailed { return "exclamationmark.triangle.fill" }
        if processing { return "ellipsis.circle.fill" }
        if recording { return "mic.fill" }
        if starting { return "mic.badge.plus" }
        if meetingActive { return "person.2.wave.2.fill" }
        return paused ? "pause.circle.fill" : "waveform"
    }
}

private struct MenuBarControlCenter: View {
    @ObservedObject var model: WorkspaceModel
    @Environment(\.openWindow) private var openWindow

    private var busy: Bool { model.isRecording || model.isStarting || model.isProcessing }

    var body: some View {
        Text("状态：\(MenuBarStatus.title(paused: model.voicePaused, recording: model.isRecording, starting: model.isStarting, processing: model.isProcessing, meetingActive: model.meetingActive))")
        Button("打开 VoiceRewrite") { showWorkspace() }
        Button("会议助手（打开主窗口）") { showWorkspace() }
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
    @StateObject private var workspace = WorkspaceModel()

    private var menuSymbol: String {
        let connectionFailed: Bool
        if case .failed = workspace.connection { connectionFailed = true }
        else { connectionFailed = false }
        return MenuBarStatus.symbol(
            paused: workspace.voicePaused, recording: workspace.isRecording,
            starting: workspace.isStarting, processing: workspace.isProcessing,
            meetingActive: workspace.meetingActive, connectionFailed: connectionFailed)
    }

    var body: some Scene {
        Window("VoiceRewrite", id: "workspace") {
            MainView(model: workspace)
                .frame(minWidth: 670, minHeight: 510)
                .onAppear {
                    lifecycle.onTerminate = { [weak workspace] in workspace?.shutdown() }
                    lifecycle.onActivate = { [weak workspace] in
                        workspace?.refreshSideMouse()
                        workspace?.refreshMicrophonePermission()
                    }
                    lifecycle.launchWorkspaceOnce {
                        workspace.launch(demo: CommandLine.arguments.contains("--meeting-demo"))
                    }
                }
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
