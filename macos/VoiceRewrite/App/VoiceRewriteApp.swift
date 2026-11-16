import SwiftUI
import AppKit

final class AppLifecycleDelegate: NSObject, NSApplicationDelegate {
    var onTerminate: (() -> Void)?
    var onActivate: (() -> Void)?
    func applicationDidBecomeActive(_ notification: Notification) { onActivate?() }
    func applicationWillTerminate(_ notification: Notification) { onTerminate?() }
}

@main
struct VoiceRewriteApp: App {
    @NSApplicationDelegateAdaptor(AppLifecycleDelegate.self) private var lifecycle
    @StateObject private var workspace = WorkspaceModel()

    var body: some Scene {
        WindowGroup("VoiceRewrite") {
            MainView(model: workspace)
                .frame(minWidth: 670, minHeight: 510)
                .onAppear {
                    lifecycle.onTerminate = { [weak workspace] in workspace?.shutdown() }
                    lifecycle.onActivate = { [weak workspace] in
                        workspace?.refreshSideMouse()
                        workspace?.refreshMicrophonePermission()
                    }
                    workspace.launch()
                }
        }
        .defaultSize(width: 840, height: 600)
        .commands {
            CommandGroup(replacing: .appSettings) {
                SettingsLink { Text("设置…") }
                    .keyboardShortcut(",", modifiers: .command)
            }
        }

        Settings {
            SettingsView(model: workspace)
                .frame(width: 540)
        }
    }
}
