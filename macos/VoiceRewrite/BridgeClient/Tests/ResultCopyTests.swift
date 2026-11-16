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
        XCTAssertEqual(ResultCopy.copySucceeded, "已复制结果到剪贴板")
        XCTAssertEqual(ResultCopy.copyFailed, "复制失败，请尝试选中正文复制")
        XCTAssertEqual(ResultCopy.passwordDictation, "密码输入框不接受自动听写，请使用普通输入方式")
        XCTAssertEqual(ResultCopy.passwordSelection, "密码输入框不允许读取选中文字或发送给 AI")
        XCTAssertEqual(ResultCopy.passwordRewrite, "已拒绝在密码输入框中自动改写")
        XCTAssertEqual(ResultCopy.dictationEmpty, "听写结果为空")
        XCTAssertEqual(ResultCopy.noSpeech, "ASR 没有识别到语音，请检查麦克风输入设备")
    }
}
