import XCTest
@testable import VoiceRewriteInteraction

final class HostStatusTests: XCTestCase {
    func testCaptureStatusKeepsTheMacSentences() {
        XCTAssertEqual(
            captureStatus(InteractionFailure.secureField, password: "密码"),
            "密码")
        XCTAssertEqual(
            captureStatus(InteractionFailure.invalidSelection, password: "密码"),
            "听写需要空光标位置；如有选中文字，请先取消选择")
        XCTAssertEqual(
            captureStatus(InteractionFailure.systemError(7), password: "密码"),
            "读取目标文本框失败（Accessibility 错误 7）")
        XCTAssertTrue(
            captureStatus(InteractionFailure.cancelled, password: "密码")
                .hasPrefix("无法安全锁定外部光标（"))
        XCTAssertEqual(
            rewriteCaptureStatus(InteractionFailure.secureField, password: "选区"),
            "选区")
        XCTAssertTrue(rewriteCaptureStatus(InteractionFailure.notEditable, password: "选区").hasPrefix("无法开始语音选区处理："))
    }

    func testDictationRecordingSentencesStayMacOnly() {
        XCTAssertEqual(recordingIdle(), "未录音")
        XCTAssertEqual(dictationCaretLocked(), "已锁定原应用光标；识别后将验证并写入")
        XCTAssertTrue(dictationNoTarget().contains("未指定外部文本框"))
        XCTAssertEqual(dictationMicrophoneFailed("denied"), "无法启动麦克风：denied")
        XCTAssertEqual(dictationInserted(), "已安全插入原目标")
        XCTAssertEqual(dictationRecognizeFailed("timeout"), "识别失败：timeout")
    }

    func testShortcutInstallReusesTheSameKeyboard() {
        XCTAssertEqual(shortcutInstall(validPlan: false, sameKeyboard: true), .rejected)
        XCTAssertEqual(shortcutInstall(validPlan: true, sameKeyboard: true), .reuse)
        XCTAssertEqual(shortcutInstall(validPlan: true, sameKeyboard: false), .replace)
        XCTAssertTrue(shortcutRestoreKeepsKeyboard(hasPrevious: true, sameKeyboard: true))
        XCTAssertFalse(shortcutRestoreKeepsKeyboard(hasPrevious: false, sameKeyboard: true))
        XCTAssertEqual(shortcutPlanLabel(dictation: "Option+R", textEdit: "Option+T", rewrite: "Option+E"), "Option+R 听写；Option+T 文本指令；Option+E 语音修改选区")
        XCTAssertEqual(shortcutSaveRejected(), "快捷键冲突或不受 macOS 支持；设置未保存")
        XCTAssertEqual(shortcutPlanRejected(), "快捷键冲突或不受 macOS 支持；原快捷键保持有效")
    }

    func testSideMouseNoticesKeepButtonNumbers() {
        XCTAssertEqual(sideMouseUnchecked(), "尚未检查鼠标侧键监听")
        XCTAssertEqual(sideMouseObserved(button: 3, action: .dictation), "已收到 Mouse4 按下；继续按住以开始听写")
        XCTAssertEqual(sideMouseObserved(button: 4, action: .rewrite), "已收到 Mouse5 按下；继续按住以开始改写")
        XCTAssertEqual(sideMouseObserved(button: 2, action: nil), "检测到辅助鼠标按钮 2；当前绑定按钮 3/4")
        XCTAssertEqual(sideMouseHoldStarted(button: nil), "已通过配置的快捷键触发")
        XCTAssertEqual(sideMouseHoldStarted(button: 3), "Mouse4 长按已触发")
        XCTAssertEqual(sideMouseStopped(), "鼠标侧键监听已停止")
    }

    func testServiceProbeKeepsAMatchingMessage() {
        XCTAssertEqual(serviceProbeRunning(name: "ASR", running: "正在检测"), "ASR：正在检测")
        XCTAssertEqual(serviceProbeStatus(name: "ASR", ok: true, kind: "ASR", message: "可用", failure: "x"), "可用")
        XCTAssertEqual(serviceProbeStatus(name: "ASR", ok: true, kind: "LLM", message: "可用", failure: "invalid_reply"), "ASR：检测失败（invalid_reply）")
        XCTAssertEqual(serviceProbeStatus(name: "LLM", ok: false, kind: "LLM", message: "可用", failure: "timeout"), "LLM：检测失败（timeout）")
        XCTAssertEqual(serviceProbeConfigStatus(name: "LLM", detail: "缺密钥"), "LLM：无法读取服务配置（缺密钥）")
    }

    func testTranscriptAndModelRepliesKeepEmptyAndPreviewRules() {
        XCTAssertEqual(transcriptReply(ok: false, text: "a"), .failed)
        XCTAssertEqual(transcriptReply(ok: true, text: nil), .failed)
        XCTAssertEqual(transcriptReply(ok: true, text: "  \n"), .empty)
        XCTAssertEqual(transcriptReply(ok: true, text: "你好"), .ready("你好"))
        XCTAssertEqual(modelReply(ok: false, text: "a", spoken: false, instruction: nil, mayWrite: true, previewOnly: false), .rejected)
        XCTAssertEqual(modelReply(ok: true, text: "a", spoken: true, instruction: "", mayWrite: true, previewOnly: false), .missingSpokenInstruction)
        XCTAssertEqual(modelReply(ok: true, text: "a", spoken: true, instruction: " ", mayWrite: false, previewOnly: true), .preview("a"))
        XCTAssertEqual(modelReply(ok: true, text: "a", spoken: false, instruction: nil, mayWrite: true, previewOnly: false), .write("a"))
    }

    func testIndicatorCancelChoosesThePreviewAndTerminalPaths() {
        XCTAssertEqual(indicatorCancel(pending: true, terminal: true), .ignoreHold)
        XCTAssertEqual(indicatorCancel(pending: false, terminal: true), .dismiss)
        XCTAssertEqual(indicatorCancel(pending: false, terminal: false), .cancelWork)
    }

    func testInsertionStatusKeepsPasswordAndClipboardSentences() {
        XCTAssertEqual(
            insertionStatus(InteractionFailure.secureField, rewrite: true, passwordRewrite: "改写", passwordDictation: "听写"),
            "改写")
        XCTAssertEqual(
            insertionStatus(InteractionFailure.secureField, rewrite: false, passwordRewrite: "改写", passwordDictation: "听写"),
            "听写")
        XCTAssertEqual(
            insertionStatus(InteractionFailure.writeNotConfirmed, rewrite: true, passwordRewrite: "改写", passwordDictation: "听写"),
            "已尝试写入，但无法确认是否生效；请检查目标或复制结果")
        XCTAssertEqual(
            insertionStatus(ClipboardFailure.restoreFailed, rewrite: false, passwordRewrite: "改写", passwordDictation: "听写"),
            "安全剪贴板操作失败；写入状态可能不确定，请检查目标并复制结果")
    }
}
