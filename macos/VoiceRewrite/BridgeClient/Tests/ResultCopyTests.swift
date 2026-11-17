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
    func testResultCardCopyIsLiveAndClosedCardIgnoresLateUpdates() {
        let card = ResultCardActionSession(text: "改写正文")
        XCTAssertEqual(card.text, "改写正文")
        XCTAssertEqual(card.note, "")
        card.copyCompleted(true)
        XCTAssertEqual(card.note, ResultCopy.copySucceeded)
        card.copyCompleted(false)
        XCTAssertEqual(card.note, ResultCopy.copyFailed)
        card.close()
        card.copyCompleted(true)
        XCTAssertEqual(card.note, ResultCopy.copyFailed)
        XCTAssertTrue(card.isClosed)
    }

    @MainActor
    func testResultCardLightDarkSnapshotsAndCopyOnlyControls() {
        let light = ResultCardPalette(dark: false)
        let dark = ResultCardPalette(dark: true)
        let lightColor = NSColor(light.background).usingColorSpace(.deviceRGB)
        let darkColor = NSColor(dark.background).usingColorSpace(.deviceRGB)
        XCTAssertNotNil(lightColor)
        XCTAssertNotNil(darkColor)
        XCTAssertGreaterThan(lightColor?.redComponent ?? 0, darkColor?.redComponent ?? 1)
        let card = ResultCardActionSession(text: String(repeating: "完整结果和滚动正文。", count: 90))
        for darkMode in [false, true] {
            let view = ResultCardView(
                title: "改写预览", session: card,
                width: ResultCardMetrics.width, height: 500,
                onCopy: {}, onClose: {})
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
                attachment.name = darkMode ? "macOS-result-card-dark-copy" : "macOS-result-card-light-copy"
                attachment.lifetime = .keepAlways
                add(attachment)
            }
        }
    }

    func testCopyButtonPreservesSharedPopupHitArea() {
        let width: Int32 = 476
        let height: Int32 = 258
        let copy = ResultCardChrome.copyButton(width: width, height: height)
        let window = ResultHitRect(left: 0, top: 0, right: width, bottom: height)
        let editor = ResultCardChrome.editor(width: width, height: height).hit
        XCTAssertFalse(ResultCardMetrics.clickDismisses(
            x: copy.x + 4, y: copy.y + 4, window: window,
            editor: editor, buttons: [copy.hit]))
    }

}
