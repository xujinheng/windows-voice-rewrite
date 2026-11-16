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
    }
}
