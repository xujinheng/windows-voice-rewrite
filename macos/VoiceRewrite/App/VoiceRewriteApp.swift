import SwiftUI
import AppKit

final class AppLifecycleDelegate: NSObject, NSApplicationDelegate {
    var onTerminate: (() -> Void)?
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
                    if case .disconnected = workspace.connection { workspace.connect() }
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
