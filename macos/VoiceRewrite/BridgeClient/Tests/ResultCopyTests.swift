import AppKit
import SwiftUI
import XCTest
@testable import VoiceRewrite

final class ResultCopyTests: XCTestCase {
    func testTitlesMatchSharedCopy() {
        XCTAssertEqual(ResultCopy.cardTitle(intent: "edit"), "改写预览")
        XCTAssertEqual(ResultCopy.cardTitle(intent: "summarize"), "AI 摘要")
        XCTAssertEqual(ResultCopy.cardTitle(intent: "translate"), "翻译结果")
        XCTAssertEqual(ResultCopy.cardTitle(intent: "ambiguous"), "AI 回答")
        XCTAssertEqual(ResultCopy.cardTitle(intent: "answer"), "AI 解释")
        XCTAssertEqual(ResultCopy.cardTitle(intent: ""), "AI 解释")
        XCTAssertEqual(ResultCopy.historyTitle(intent: "edit"), "AI edit")
        XCTAssertEqual(ResultCopy.historyTitle(intent: "  "), "AI 结果")
        XCTAssertEqual(ResultCopy.historyRestoreTitle("听写结果"), "听写结果 · 历史结果")
        XCTAssertEqual(ResultCopy.dictationNotInserted, "听写结果 · 未自动插入")
        XCTAssertEqual(ResultCopy.dictationCopyManually, "听写结果 · 请手动复制")
        XCTAssertEqual(ResultCopy.dictationWriteUnconfirmed, "听写结果 · 请确认是否已输入")
        XCTAssertEqual(ResultCopy.copyAction, "复制结果")
        XCTAssertEqual(ResultCopy.applyAction, "应用到原文")
        XCTAssertEqual(ResultCopy.closeGlyph, "×")
        XCTAssertEqual(ResultCopy.copySucceeded, "已复制结果到剪贴板")
        XCTAssertEqual(ResultCopy.copyFailed, "复制失败，请尝试选中正文复制")
        XCTAssertEqual(ResultCopy.passwordDictation, "密码输入框不接受自动听写，请使用普通输入方式")
        XCTAssertEqual(ResultCopy.passwordSelection, "密码输入框不允许读取选中文字或发送给 AI")
        XCTAssertEqual(ResultCopy.passwordRewrite, "已拒绝在密码输入框中自动改写")
        XCTAssertEqual(ResultCopy.dictationEmpty, "听写结果为空")
        XCTAssertEqual(ResultCopy.noSpeech, "ASR 没有识别到语音，请检查麦克风输入设备")
        XCTAssertEqual(FailureCopy.status(prefix: "ASR 请求失败：", message: ResultCopy.noSpeech), ResultCopy.noSpeech)
        XCTAssertEqual(FailureCopy.status(prefix: "ASR 请求失败：", message: "语音太短，无法转写"), "语音太短，无法转写")
        XCTAssertEqual(FailureCopy.status(prefix: "模型处理失败：", message: "选中文字超过 9 字限制"), "选中文字超过 9 字限制")
        XCTAssertEqual(FailureCopy.status(prefix: "模型处理失败：", message: "speech transcription failed"), "模型处理失败：speech transcription failed")
        XCTAssertEqual(FailureCopy.status(prefix: "快捷键无效：", message: "Shift+字母会干扰打字，请同时使用 Ctrl 或 Alt"), "Shift+字母会干扰打字，请同时使用 Ctrl 或 Alt")
        XCTAssertEqual(FailureCopy.status(prefix: "快捷键无效：", message: "未知修饰键 \"Win\""), "未知修饰键 \"Win\"")
        XCTAssertEqual(FailureCopy.status(prefix: "快捷键无效：", message: "Helper 未运行"), "快捷键无效：Helper 未运行")
        XCTAssertEqual(
            FailureCopy.status(prefix: "ASR 请求失败：", message: ResultCopy.noSpeech + " sk-secret"),
            "ASR 请求失败：" + ResultCopy.noSpeech + " sk-secret")
    }
    @MainActor
    func testApplyIsUnavailableWithoutAnExplicitOriginalSelection() async {
        let card = ResultCardActionSession(text: "preview")
        XCTAssertNil(card.apply)
        await card.applyIfAuthorized()
        XCTAssertTrue(card.note.isEmpty)
        XCTAssertFalse(card.isApplying)
    }

    @MainActor
    func testManualApplyMustRevalidateBeforeReplacing() async {
        var checks = 0
        var writes = 0
        let card = ResultCardActionSession(text: "改写正文", apply: ResultCardApply(
            revalidate: {
                checks += 1
                return .unsafe
            },
            replace: { _ in
                writes += 1
                return .confirmedWrite
            }))
        await card.applyIfAuthorized()
        XCTAssertEqual(checks, 1)
        XCTAssertEqual(writes, 0)
        XCTAssertEqual(card.note, "无法安全替换，请使用复制结果")
        XCTAssertFalse(card.isApplying)
        await card.applyIfAuthorized()
        XCTAssertEqual(checks, 2)
        XCTAssertEqual(writes, 0)
    }

    @MainActor
    func testManualApplyDifferentiatesSentEventFromConfirmedWrite() async {
        var writes = [String]()
        let sentCard = ResultCardActionSession(text: "文本", apply: ResultCardApply(
            revalidate: { .authorized },
            replace: { value in
                writes.append(value)
                return .eventSent
            }))
        await sentCard.applyIfAuthorized()
        XCTAssertEqual(writes, ["文本"])
        XCTAssertEqual(sentCard.note, "已发送粘贴指令 · 请确认是否写入")
        XCTAssertFalse(sentCard.mayApply)
        await sentCard.applyIfAuthorized()
        XCTAssertEqual(writes, ["文本"], "An unconfirmed paste event must never be retried")
        let confirmedCard = ResultCardActionSession(text: "新文本", apply: ResultCardApply(
            revalidate: { .authorized },
            replace: { value in
                writes.append(value)
                return .confirmedWrite
            }))
        await confirmedCard.applyIfAuthorized()
        XCTAssertEqual(writes, ["文本", "新文本"])
        XCTAssertEqual(confirmedCard.note, "已确认替换原选区")
        XCTAssertFalse(confirmedCard.mayApply)
    }

    @MainActor
    func testClosedEditorAndBusySessionDoNotReplace() async {
        for result in [ResultApplyAuthorization.editorClosed, .busy] {
            var writes = 0
            let card = ResultCardActionSession(text: "正文", apply: ResultCardApply(
                revalidate: { result },
                replace: { _ in
                    writes += 1
                    return .confirmedWrite
                }))
            await card.applyIfAuthorized()
            XCTAssertEqual(writes, 0)
            XCTAssertFalse(card.note.isEmpty)
        }
    }

    @MainActor
    func testCancelDuringValidationPreventsLateWriteAndCopy() async {
        var writes = 0
        var card: ResultCardActionSession!
        card = ResultCardActionSession(text: "正文", apply: ResultCardApply(
            revalidate: {
                card.close()
                return .authorized
            },
            replace: { _ in
                writes += 1
                return .confirmedWrite
            }))
        await card.applyIfAuthorized()
        XCTAssertEqual(writes, 0)
        XCTAssertTrue(card.isClosed)
        card.copyCompleted(true)
        XCTAssertEqual(card.note, "正在核验原选区并应用…")
    }

    @MainActor
    func testCopySuccessFailureAndApplyReceiptFailures() async {
        let card = ResultCardActionSession(text: "正文", apply: ResultCardApply(
            revalidate: { .authorized },
            replace: { _ in .editorClosed }))
        card.copyCompleted(true)
        XCTAssertEqual(card.note, ResultCopy.copySucceeded)
        card.copyCompleted(false)
        XCTAssertEqual(card.note, ResultCopy.copyFailed)
        await card.applyIfAuthorized()
        XCTAssertEqual(card.note, "原编辑窗口已关闭 · 请复制结果")
        XCTAssertFalse(card.isApplying)
    }

    func testApplyFrameMatchesSharedPopupRuleAndDoesNotDismiss() {
        let width: Int32 = 476
        let height: Int32 = 258
        let copy = ResultCardChrome.copyButton(width: width, height: height)
        let apply = ResultHitRect(left: copy.x - 124, top: copy.y,
                                  right: copy.x - 8, bottom: copy.y + copy.h)
        XCTAssertEqual(apply.left, width - 243)
        XCTAssertEqual(apply.right - apply.left, 116)
        XCTAssertEqual(apply.bottom - apply.top, 33)
        let window = ResultHitRect(left: 0, top: 0, right: width, bottom: height)
        let editor = ResultCardChrome.editor(width: width, height: height).hit
        XCTAssertFalse(ResultCardMetrics.clickDismisses(
            x: apply.left + 4, y: apply.top + 4, window: window,
            editor: editor, buttons: [apply, copy.hit]))
    }

    @MainActor
    func testResultCardLightDarkVisualSurfacesAndAttachedSnapshots() {
        let light = ResultCardPalette(dark: false)
        let dark = ResultCardPalette(dark: true)
        let lightColor = NSColor(light.background).usingColorSpace(.deviceRGB)
        let darkColor = NSColor(dark.background).usingColorSpace(.deviceRGB)
        XCTAssertNotNil(lightColor)
        XCTAssertNotNil(darkColor)
        XCTAssertGreaterThan(lightColor?.redComponent ?? 0, darkColor?.redComponent ?? 1)
        let original = ResultCardActionSession(text: String(repeating: "完整结果和滚动正文。", count: 90),
            apply: ResultCardApply(revalidate: { .authorized }, replace: { _ in .eventSent }))
        let preview = ResultCardActionSession(text: "只读 AI 回答，不应显示应用按钮")
        XCTAssertNotNil(original.apply)
        XCTAssertNil(preview.apply)
        for darkMode in [false, true] {
            let view = ResultCardView(
                title: "改写预览", session: original,
                width: ResultCardMetrics.width, height: 500,
                onCopy: {}, onApply: {}, onClose: {})
                .environment(\.colorScheme, darkMode ? ColorScheme.dark : .light)
            let hosted = NSHostingView(rootView: view)
            hosted.frame = NSRect(x: 0, y: 0, width: 476, height: 500)
            hosted.layoutSubtreeIfNeeded()
            XCTAssertEqual(hosted.frame.width, 476)
            XCTAssertEqual(hosted.frame.height, 500)
            if let bitmap = hosted.bitmapImageRepForCachingDisplay(in: hosted.bounds) {
                hosted.cacheDisplay(in: hosted.bounds, to: bitmap)
                let image = NSImage(size: hosted.bounds.size)
                image.addRepresentation(bitmap)
                let attachment = XCTAttachment(image: image)
                attachment.name = darkMode ? "macOS-result-card-dark-apply" : "macOS-result-card-light-apply"
                attachment.lifetime = .keepAlways
                add(attachment)
            }
        }
    }

    @MainActor
    func testConcurrentApplyIsSingleFlight() async {
        var checks = 0
        var writes = 0
        var card: ResultCardActionSession!
        card = ResultCardActionSession(text: "正文", apply: ResultCardApply(
            revalidate: {
                checks += 1
                await card.applyIfAuthorized()
                return .authorized
            },
            replace: { _ in
                writes += 1
                return .eventSent
            }))
        await card.applyIfAuthorized()
        XCTAssertEqual(checks, 1)
        XCTAssertEqual(writes, 1)
        XCTAssertFalse(card.mayApply)
    }

}
