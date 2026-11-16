import Foundation

public func captureStatus(_ error: Error, password: String) -> String {
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
        return password
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

public func rewriteCaptureStatus(_ error: Error, password: String) -> String {
    if let failure = error as? InteractionFailure, failure == .secureField {
        return password
    }
    return "无法开始语音选区处理：\(error.localizedDescription)"
}

public func insertionStatus(_ error: Error, rewrite: Bool, passwordRewrite: String, passwordDictation: String) -> String {
    if let failure = error as? InteractionFailure, failure == .secureField {
        return rewrite ? passwordRewrite : passwordDictation
    }
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

public func sideMouseUnchecked() -> String {
    "尚未检查鼠标侧键监听"
}

public func sideMouseAccessibilityDenied() -> String {
    "未授权辅助功能；请在系统设置授权后返回应用"
}

public func sideMouseObserved(button: Int, action: SideMouseAction?) -> String {
    guard let action else {
        return "检测到辅助鼠标按钮 \(button)；当前绑定按钮 3/4"
    }
    if action == .dictation {
        return "已收到 Mouse\(button + 1) 按下；继续按住以开始听写"
    }
    return "已收到 Mouse\(button + 1) 按下；继续按住以开始改写"
}

public func sideMouseListening() -> String {
    "监听已启动；等待已绑定的鼠标侧键事件"
}

public func sideMouseListenFailed(_ detail: String) -> String {
    "监听失败：\(detail)；键盘快捷键仍可用"
}

public func sideMouseHoldStarted(button: Int?) -> String {
    if let button {
        return "Mouse\(button + 1) 长按已触发"
    }
    return "已通过配置的快捷键触发"
}

public func sideMouseStopped() -> String {
    "鼠标侧键监听已停止"
}

public enum ShortcutInstall: Equatable, Sendable {
    case rejected
    case reuse
    case replace
}

public func shortcutInstall(validPlan: Bool, sameKeyboard: Bool) -> ShortcutInstall {
    if !validPlan {
        return .rejected
    }
    if sameKeyboard {
        return .reuse
    }
    return .replace
}

public func shortcutRestoreKeepsKeyboard(hasPrevious: Bool, sameKeyboard: Bool) -> Bool {
    hasPrevious && sameKeyboard
}

public func shortcutPlanLabel(dictation: String, textEdit: String, rewrite: String) -> String {
    "\(dictation) 听写；\(textEdit) 文本指令；\(rewrite) 语音修改选区"
}

public func shortcutPlanRejected() -> String {
    "快捷键冲突或不受 macOS 支持；原快捷键保持有效"
}

public func shortcutRegisterFailed(_ detail: String) -> String {
    "快捷键注册失败：\(detail)；已尝试恢复先前的快捷键"
}

public func shortcutPendingHelper() -> String {
    "快捷键将在 Go Helper 连接后确认"
}

public func shortcutSaveRejected() -> String {
    "快捷键冲突或不受 macOS 支持；设置未保存"
}

public func shortcutNotRegistered() -> String {
    "快捷键未能注册；设置未保存，原快捷键保持有效"
}

public func serviceProbeRunning(name: String, running: String) -> String {
    return name + "：" + running
}

public func serviceProbeConfigStatus(name: String, detail: String) -> String {
    return "\(name)：无法读取服务配置（\(detail)）"
}

public func serviceProbeStatus(name: String, ok: Bool, kind: String?, message: String?, failure: String) -> String {
    if ok, kind == name, let message {
        return message
    }
    return "\(name)：检测失败（\(failure)）"
}

public enum TranscriptReply: Equatable, Sendable {
    case failed
    case empty
    case ready(String)
}

public func transcriptReply(ok: Bool, text: String?) -> TranscriptReply {
    guard ok, let text else { return .failed }
    if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
        return .empty
    }
    return .ready(text)
}

public enum ModelReply: Equatable, Sendable {
    case rejected
    case missingSpokenInstruction
    case preview(String)
    case write(String)
}

public func modelReply(ok: Bool, text: String?, spoken: Bool, instruction: String?, mayWrite: Bool, previewOnly: Bool) -> ModelReply {
    guard ok, let text else { return .rejected }
    if spoken {
        guard let instruction, !instruction.isEmpty else { return .missingSpokenInstruction }
    }
    if mayWrite && !previewOnly {
        return .write(text)
    }
    return .preview(text)
}
