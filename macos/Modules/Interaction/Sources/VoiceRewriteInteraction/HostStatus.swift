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
