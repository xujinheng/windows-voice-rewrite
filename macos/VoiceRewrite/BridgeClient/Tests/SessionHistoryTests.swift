import XCTest
@testable import VoiceRewrite

final class SessionHistoryTests: XCTestCase {
    func testKeepsTenNewestAndDropsOversizedText() {
        var history = SessionHistory()
        for index in 0..<14 {
            history.remember(title: "听写结果", value: String(format: "内容%02d", index))
        }
        XCTAssertEqual(history.entries.count, SessionHistory.limit)
        XCTAssertEqual(history.entries.first?.text, "内容13")
        XCTAssertEqual(history.entries.last?.text, "内容04")
        XCTAssertNotNil(history.entry(id: history.entries[0].id))
        XCTAssertNil(history.entry(id: 1))
        history.remember(title: "", value: "   ")
        XCTAssertEqual(history.entries.count, SessionHistory.limit)
        history.remember(title: "大结果", value: String(repeating: "x", count: SessionHistory.maxBytes + 1))
        XCTAssertEqual(history.entries.count, SessionHistory.limit)
        history.clear()
        XCTAssertTrue(history.entries.isEmpty)
    }

    func testMenuLineShowsKindAndTimeOnly() {
        var components = DateComponents()
        components.year = 2026
        components.month = 1
        components.day = 2
        components.hour = 15
        components.minute = 4
        let created = Calendar(identifier: .gregorian).date(from: components)!
        let dictation = SessionHistory.menuLine(index: 0, title: "听写结果", createdAt: created)
        let answer = SessionHistory.menuLine(index: 2, title: "AI translate", createdAt: created)
        XCTAssertEqual(dictation, "1. 听写 · 15:04")
        XCTAssertEqual(answer, "3. AI 结果 · 15:04")
        XCTAssertFalse(dictation.contains("内容"))
        XCTAssertFalse(answer.contains("secret"))
    }
}
