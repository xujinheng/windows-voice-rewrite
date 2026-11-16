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
