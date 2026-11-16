import XCTest
@testable import VoiceRewrite

final class DesktopWorkAreaTests: XCTestCase {
    func testClampMatchesCoreOnANarrowScreen() {
        let work = DesktopWorkArea(left: 0, top: 0, right: 200, bottom: 80)
        let (x, y) = work.clamp(x: -20, y: 90, width: 232, height: 50)
        XCTAssertEqual(x, 0)
        XCTAssertEqual(y, 30)
    }

    func testResultCardHeightFollowsTextLength() {
        XCTAssertEqual(ResultCardMetrics.height(unicodeScalars: 0), 258)
        XCTAssertEqual(ResultCardMetrics.height(unicodeScalars: 200), 258)
        XCTAssertEqual(ResultCardMetrics.height(unicodeScalars: 201), 315)
        XCTAssertEqual(ResultCardMetrics.height(unicodeScalars: 601), 430)
        XCTAssertEqual(ResultCardMetrics.height(unicodeScalars: 1201), 500)
        let window = ResultHitRect(left: 10, top: 10, right: 486, bottom: 268)
        let editor = ResultHitRect(left: 30, top: 66, right: 466, bottom: 202)
        let buttons = [
            ResultHitRect(left: 258, top: 214, right: 375, bottom: 252),
            ResultHitRect(left: 383, top: 214, right: 468, bottom: 252),
        ]
        XCTAssertTrue(ResultCardMetrics.clickDismisses(x: 9, y: 125, window: window, editor: editor, buttons: buttons))
        XCTAssertFalse(ResultCardMetrics.clickDismisses(x: 50, y: 92, window: window, editor: editor, buttons: buttons))
        XCTAssertFalse(ResultCardMetrics.clickDismisses(x: 420, y: 233, window: window, editor: editor, buttons: buttons))
        XCTAssertTrue(ResultCardMetrics.clickDismisses(x: 45, y: 32, window: window, editor: editor, buttons: buttons))
    }
}
