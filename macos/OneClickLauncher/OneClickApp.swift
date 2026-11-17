import AppKit
import Darwin
import Foundation
import SwiftUI

// The GUI is a macOS-only process adapter. The bundled Go OneClick executable
// owns the single shared HTTP proxy, llama arguments and child lifetime.
struct OneClickConfiguration {
    let llamaServer: String
    let model: String
    let audioEncoder: String
    let attachToExisting: Bool
    let publicPort: Int
    let backendPort: Int
    let gpuLayers: Int
    let contextSize: Int
    let allowCPU: Bool

    var baseURL: String { "http://127.0.0.1:\(publicPort)/v1" }

    func validate() throws {
        let fs = FileManager.default
        if !attachToExisting {
        guard fs.isExecutableFile(atPath: llamaServer) else {
            throw LauncherError.message("请选择可执行的 llama-server 文件（macOS 对应架构）。")
        }
        guard fs.isReadableFile(atPath: model), fs.isReadableFile(atPath: audioEncoder) else {
            throw LauncherError.message("模型 GGUF 或音频编码器 mmproj 不存在或不可读。")
        }
        }
        guard (1...65535).contains(publicPort), (1...65535).contains(backendPort),
              publicPort != backendPort else {
            throw LauncherError.message("公开端口与内部端口必须不同，且均位于 1–65535。")
        }
        guard (0...999).contains(gpuLayers), (1...131072).contains(contextSize) else {
            throw LauncherError.message("GPU 层数或上下文大小无效。")
        }
    }

    func writeConfig() throws -> URL {
        let directory = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/VoiceRewrite-OneClick", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let path = directory.appendingPathComponent("config.json")
        let object: [String: Any] = [
            "llama_server_path": llamaServer,
            "model_path": model,
            "mmproj_path": audioEncoder,
            "listen_address": "127.0.0.1:\(publicPort)",
            "backend_host": "127.0.0.1",
            "backend_port": backendPort,
            "gpu_layers": gpuLayers,
            "context_size": contextSize,
            "allow_cpu": allowCPU,
            "attach_to_existing_server": attachToExisting
        ]
        let data = try JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys])
        try data.write(to: path, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: path.path)
        return path
    }
}

enum LauncherError: LocalizedError {
    case message(String)
    var errorDescription: String? {
        if case let .message(message) = self { return message }
        return nil
    }
}

private struct ModelsResponse: Decodable {
    struct Entry: Decodable { let id: String }
    let data: [Entry]
}

@MainActor
final class OneClickController: ObservableObject {
    @Published private(set) var phase = "未启动"
    @Published private(set) var logText = ""
    @Published private(set) var modelID = ""
    @Published private(set) var activeBaseURL = ""
    @Published private(set) var ready = false
    @Published private(set) var isRunning = false

    private var process: Process?
    private var outputPipe: Pipe?
    private var healthTask: Task<Void, Never>?
    private var pendingRestart: OneClickConfiguration?
    private var generation = UUID()

    func start(_ config: OneClickConfiguration) {
        guard process == nil else { return }
        do {
            try config.validate()
            let path = try config.writeConfig()
            guard let executable = Bundle.main.executableURL?
                .deletingLastPathComponent().appendingPathComponent("qwen3asr-oneclick"),
                  FileManager.default.isExecutableFile(atPath: executable.path) else {
                throw LauncherError.message("缺少捆绑的 Go OneClick 服务程序，请重新构建或安装启动器。")
            }
            let child = Process()
            child.executableURL = executable
            child.arguments = ["-config", path.path]
            child.currentDirectoryURL = executable.deletingLastPathComponent()
            let pipe = Pipe()
            child.standardOutput = pipe
            child.standardError = pipe
            pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
                let data = handle.availableData
                guard !data.isEmpty else { return }
                let line = String(decoding: data, as: UTF8.self)
                Task { @MainActor [weak self] in self?.appendLog(line) }
            }
            child.terminationHandler = { [weak self] finished in
                Task { @MainActor [weak self] in self?.didTerminate(finished) }
            }
            try child.run()
            process = child
            outputPipe = pipe
            isRunning = true
            ready = false
            modelID = ""
            activeBaseURL = config.baseURL
            phase = "正在启动并加载模型…"
            generation = UUID()
            appendLog("启动共享 OneClick 代理；接口：\(config.baseURL)\n")
            pollHealth(port: config.publicPort, generation: generation)
        } catch {
            phase = "启动失败：\(error.localizedDescription)"
            ready = false
            isRunning = false
            appendLog(phase + "\n")
        }
    }

    func stop() {
        pendingRestart = nil
        stopOwnedProcess()
    }

    func restart(_ config: OneClickConfiguration) {
        if process == nil {
            start(config)
        } else {
            pendingRestart = config
            stopOwnedProcess()
        }
    }

    func shutdown() {
        pendingRestart = nil
        healthTask?.cancel()
        healthTask = nil
        if let process, process.isRunning {
            process.terminate()
            // On application termination, allow the Go helper a bounded grace
            // period to reap its own llama child before the native host exits.
            let deadline = Date().addingTimeInterval(2)
            while process.isRunning && Date() < deadline {
                Thread.sleep(forTimeInterval: 0.03)
            }
            if process.isRunning {
                _ = Darwin.kill(process.processIdentifier, SIGKILL)
            }
        }
    }

    func copy(_ value: String) {
        guard !value.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
    }

    private func stopOwnedProcess() {
        healthTask?.cancel()
        healthTask = nil
        ready = false
        guard let child = process else { phase = "已停止"; return }
        phase = "正在停止服务…"
        if child.isRunning {
            child.terminate()
            DispatchQueue.main.asyncAfter(deadline: .now() + 6) { [weak self, weak child] in
                guard let self, let child, self.process === child, child.isRunning else { return }
                self.appendLog("Go 服务未在 6 秒内退出，强制停止所属进程。\n")
                _ = Darwin.kill(child.processIdentifier, SIGKILL)
            }
        }
    }

    private func didTerminate(_ child: Process) {
        guard process === child else { return }
        healthTask?.cancel()
        healthTask = nil
        outputPipe?.fileHandleForReading.readabilityHandler = nil
        outputPipe = nil
        process = nil
        isRunning = false
        ready = false
        modelID = ""
        activeBaseURL = ""
        let restart = pendingRestart
        pendingRestart = nil
        if let restart {
            phase = "正在重启…"
            start(restart)
        } else {
            phase = child.terminationStatus == 0 ? "已停止" :
                "服务已退出（状态码 \(child.terminationStatus)），请查看下方诊断日志"
            appendLog("\n\(phase)\n")
        }
    }

    private func appendLog(_ text: String) {
        // Keep the UI bounded even if llama.cpp produces verbose diagnostics.
        logText = String((logText + text).suffix(24_000))
    }

    private func pollHealth(port: Int, generation: UUID) {
        healthTask?.cancel()
        healthTask = Task { [weak self] in
            guard let self else { return }
            for _ in 0..<120 {
                if Task.isCancelled || self.generation != generation || self.process == nil { return }
                var request = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/health")!)
                request.timeoutInterval = 1.5
                if let (_, response) = try? await URLSession.shared.data(for: request),
                   let http = response as? HTTPURLResponse, http.statusCode == 200 {
                    guard !Task.isCancelled, self.generation == generation,
                          self.process != nil else { return }
                    self.ready = true
                    self.phase = "服务已就绪"
                    self.appendLog("健康检查通过；这只证明 HTTP 模拟/真实服务就绪，不代表 GPU 推理已验证。\n")
                    await self.loadModelID(port: port, generation: generation)
                    return
                }
                try? await Task.sleep(nanoseconds: 2_000_000_000)
            }
            if !Task.isCancelled, self.generation == generation, self.process != nil {
                self.phase = "健康检查超时；请检查模型加载和日志"
            }
        }
    }

    private func loadModelID(port: Int, generation: UUID) async {
        var request = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/v1/models")!)
        request.timeoutInterval = 3
        guard let (data, response) = try? await URLSession.shared.data(for: request),
              (response as? HTTPURLResponse)?.statusCode == 200,
              let models = try? JSONDecoder().decode(ModelsResponse.self, from: data) else {
            appendLog("模型发现失败：可在 VoiceRewrite 设置中输入服务实际报告的模型 ID。\n")
            return
        }
        guard self.generation == generation, ready, process != nil else { return }
        modelID = models.data.first?.id ?? ""
    }
}

final class OneClickLifecycleDelegate: NSObject, NSApplicationDelegate {
    var terminate: (() -> Void)?
    func applicationWillTerminate(_ notification: Notification) { terminate?() }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

private struct FileField: View {
    let title: String
    @Binding var path: String

    var body: some View {
        HStack {
            TextField(title, text: $path)
                .textFieldStyle(.roundedBorder)
            Button("选择…") {
                let panel = NSOpenPanel()
                panel.canChooseFiles = true
                panel.canChooseDirectories = false
                panel.allowsMultipleSelection = false
                if panel.runModal() == .OK, let selected = panel.url {
                    path = selected.path
                }
            }
        }
    }
}

private struct OneClickWindow: View {
    @ObservedObject var controller: OneClickController

    @AppStorage("oneclick.llamaServer") private var llamaServer = ""
    @AppStorage("oneclick.model") private var model = ""
    @AppStorage("oneclick.audioEncoder") private var audioEncoder = ""
    @AppStorage("oneclick.attach") private var attach = false
    @AppStorage("oneclick.publicPort") private var publicPort = 8081
    @AppStorage("oneclick.backendPort") private var backendPort = 8080
    @AppStorage("oneclick.gpuLayers") private var gpuLayers = 99
    @AppStorage("oneclick.contextSize") private var contextSize = 8192
    @AppStorage("oneclick.allowCPU") private var allowCPU = false

    private var configuration: OneClickConfiguration {
        .init(llamaServer: llamaServer, model: model, audioEncoder: audioEncoder,
              attachToExisting: attach, publicPort: publicPort, backendPort: backendPort,
              gpuLayers: gpuLayers, contextSize: contextSize, allowCPU: allowCPU)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("VoiceRewrite · macOS OneClick")
                .font(.title2.bold())
            Text("在本机运行 llama.cpp Qwen3-ASR，与 Windows OneClick 共用 Go 代理。不会下载模型或访问外部部署服务。")
                .foregroundStyle(.secondary)

            GroupBox("本地文件") {
                VStack(alignment: .leading, spacing: 10) {
                    Toggle("连接已运行的本地 llama-server（仅管理 OneClick 代理）", isOn: $attach)
                    FileField(title: "llama-server 可执行文件", path: $llamaServer).disabled(attach)
                    FileField(title: "语音模型 GGUF", path: $model).disabled(attach)
                    FileField(title: "音频编码器 mmproj GGUF", path: $audioEncoder).disabled(attach)
                }
                .padding(.vertical, 6)
            }

            GroupBox("服务选项") {
                VStack(spacing: 9) {
                    HStack {
                        Text("对外端口")
                        TextField("8081", value: $publicPort, format: .number)
                            .frame(width: 75)
                        Text("内部端口")
                        TextField("8080", value: $backendPort, format: .number)
                            .frame(width: 75)
                        Spacer()
                        Text("仅监听 127.0.0.1").foregroundStyle(.secondary)
                    }
                    HStack {
                        Text("GPU 层数")
                        TextField("99", value: $gpuLayers, format: .number).frame(width: 75)
                        Text("上下文大小")
                        TextField("8192", value: $contextSize, format: .number).frame(width: 85)
                        Toggle("跳过 GPU 预检", isOn: $allowCPU)
                        Spacer()
                    }
                    Text("纯 CPU 请设置 GPU 层数为 0；跳过 GPU 预检并不保证后端自动 CPU 回退。Metal 取决于 llama-server 与 Mac 硬件。")
                        .font(.caption).foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(.vertical, 6)
            }

            HStack {
                Button("启动") { controller.start(configuration) }
                    .disabled(controller.isRunning)
                    .keyboardShortcut(.defaultAction)
                Button("停止") { controller.stop() }.disabled(!controller.isRunning)
                Button("重启") { controller.restart(configuration) }
                Spacer()
                Text(controller.phase)
                    .foregroundStyle(controller.ready ? Color.green : Color.secondary)
                    .lineLimit(2)
            }

            GroupBox("连接 VoiceRewrite") {
                VStack(alignment: .leading, spacing: 8) {
                    Text("在 VoiceRewrite → 设置 → ASR 服务中填写以下基础 URL；启动完成后使用“检测 ASR 连接”。")
                    HStack {
                        Text(controller.isRunning ? controller.activeBaseURL : configuration.baseURL)
                            .font(.system(.body, design: .monospaced))
                            .textSelection(.enabled)
                        Button("复制 URL") {
                            controller.copy(controller.isRunning ? controller.activeBaseURL : configuration.baseURL)
                        }
                    }
                    if !controller.modelID.isEmpty {
                        HStack {
                            Text("模型 ID：\(controller.modelID)").textSelection(.enabled)
                            Button("复制模型 ID") { controller.copy(controller.modelID) }
                        }
                    } else {
                        Text("模型 ID 将在服务就绪后从 /v1/models 自动读取。")
                            .foregroundStyle(.secondary)
                    }
                    Text("聊天补全和 tokenize 与 Windows OneClick 相同；语音模型并不等同于通用改写 LLM。改写需要另行配置适用的 LLM 服务。")
                        .font(.caption).foregroundStyle(.secondary)
                }
                .padding(.vertical, 6)
            }

            Text("运行诊断（启动错误、端口冲突、模型加载和异常退出）")
                .font(.headline)
            ScrollView {
                Text(controller.logText.isEmpty ? "尚无服务输出" : controller.logText)
                    .font(.system(size: 11, design: .monospaced))
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .textSelection(.enabled)
                    .padding(8)
            }
            .frame(minHeight: 120)
            .background(Color(nsColor: .textBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 6))
        }
        .padding(20)
        .frame(minWidth: 660, minHeight: 690)
    }
}

@main
struct OneClickMacApp: App {
    @NSApplicationDelegateAdaptor(OneClickLifecycleDelegate.self) private var lifecycle
    @StateObject private var controller = OneClickController()

    var body: some Scene {
        WindowGroup("VoiceRewrite OneClick") {
            OneClickWindow(controller: controller)
                .onAppear {
                    lifecycle.terminate = { [weak controller] in controller?.shutdown() }
                }
        }
        .windowResizability(.contentSize)
    }
}
