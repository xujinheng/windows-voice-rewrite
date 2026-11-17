import XCTest
@testable import VoiceRewrite

final class MeetingReaderTests: XCTestCase {
    func testDefaultSixPaneOrderMatchesWindows() {
        XCTAssertEqual(MeetingReaderLayout.visible(captionsOnly: false, expanded: false),
                       [0, 1, 2, 3, 4, 5])
        XCTAssertEqual(MeetingReaderLayout.defaults.map { Int($0) }, [85, 50, 85, 65, 230, 40])
    }

    func testFocusedCaptionsAndExpandedEnglishKeepReadingRegions() {
        XCTAssertEqual(MeetingReaderLayout.visible(captionsOnly: true, expanded: false), [0, 1, 2])
        XCTAssertEqual(MeetingReaderLayout.visible(captionsOnly: false, expanded: true),
                       [0, 1, 2, 4, 5])
    }

    func testSavedSplitterWeightsRoundTripAndRejectCorruption() {
        let values = [46.0, 32.0, 34.0, 36.0, 280.0, 28.0].map { CGFloat($0) }
        XCTAssertEqual(MeetingReaderLayout.decode(MeetingReaderLayout.encode(values)), values)
        XCTAssertEqual(MeetingReaderLayout.decode("1,2,3"), MeetingReaderLayout.defaults)
        XCTAssertEqual(MeetingReaderLayout.decode("-1,1,1,1,1,1"), MeetingReaderLayout.defaults)
        XCTAssertEqual(MeetingReaderLayout.decode("0,0,0,0,0,0"), MeetingReaderLayout.defaults)
    }

    func testSixRegionsResizeWithoutGoingUnderReadingMinimums() {
        let all = MeetingReaderLayout.visible(captionsOnly: false, expanded: false)
        let start = MeetingReaderLayout.heights(space: 650, weights: MeetingReaderLayout.defaults,
                                                visible: all)
        XCTAssertEqual(start.reduce(0, +), 650, accuracy: 0.001)
        let newWeights = MeetingReaderLayout.resized(weights: MeetingReaderLayout.defaults,
            visible: all, after: 0, delta: 10000, space: 650)
        let heights = MeetingReaderLayout.heights(space: 650, weights: newWeights, visible: all)
        XCTAssertGreaterThanOrEqual(heights[0], MeetingReaderLayout.minimums[0])
        XCTAssertEqual(heights[1], MeetingReaderLayout.minimums[1], accuracy: 0.001)
        XCTAssertEqual(heights.reduce(0, +), 650, accuracy: 0.001)
    }

    func testExpandedEnglishResizesNonAdjacentVisibleRegions() {
        let visible = MeetingReaderLayout.visible(captionsOnly: false, expanded: true)
        let adjusted = MeetingReaderLayout.resized(weights: MeetingReaderLayout.defaults,
            visible: visible, after: 2, delta: 18, space: 550)
        XCTAssertEqual(adjusted[3], MeetingReaderLayout.defaults[3])
        let result = MeetingReaderLayout.heights(space: 550, weights: adjusted, visible: visible)
        XCTAssertEqual(result[3], 0)
        XCTAssertGreaterThanOrEqual(result[4], MeetingReaderLayout.minimums[4])
    }

    func testNarrowReadingAreaRetainsAccessibleMinimumSizes() {
        let all = MeetingReaderLayout.visible(captionsOnly: false, expanded: false)
        let heights = MeetingReaderLayout.heights(space: 100, weights: MeetingReaderLayout.defaults,
                                                  visible: all)
        for index in all {
            XCTAssertGreaterThanOrEqual(heights[index], MeetingReaderLayout.minimums[index])
        }
    }

    func testReadingHistoryStopsFollowingAndCountsOnlyNewCompletedTurns() {
        XCTAssertEqual(MeetingReaderLayout.unread(previous: 4, current: 5, unread: 2,
                                                   following: false), 3)
        XCTAssertEqual(MeetingReaderLayout.unread(previous: 4, current: 4, unread: 2,
                                                   following: false), 2)
        XCTAssertEqual(MeetingReaderLayout.unread(previous: 4, current: 5, unread: 2,
                                                   following: true), 0)
        XCTAssertEqual(MeetingReaderLayout.unread(previous: 4, current: 0, unread: 2,
                                                   following: false), 0)
    }

    func testLongTranscriptDoesNotAffectPaneGeometry() {
        let visible = MeetingReaderLayout.visible(captionsOnly: false, expanded: false)
        let heights = MeetingReaderLayout.heights(space: 780,
            weights: MeetingReaderLayout.defaults, visible: visible)
        let longEnglish = String(repeating: "Very long uninterrupted caption. ", count: 2000)
        XCTAssertGreaterThan(longEnglish.count, 50000)
        XCTAssertEqual(heights.reduce(0, +), 780, accuracy: 0.001)
    }

    func testLiveTurnNeverEntersHistoryUntilAnotherTurnStarts() {
        let a = MeetingCaptionRow(id: 1, source: "First", chinese: "第一句", final: true)
        let b = MeetingCaptionRow(id: 2, source: "Second", chinese: "第二句", final: true)
        let partial = MeetingCaptionRow(id: 3, source: "Third...", chinese: "", final: false)
        var sections = MeetingReaderLayout.split([a, b, partial])
        XCTAssertEqual(sections.current?.id, 3)
        XCTAssertEqual(sections.history.map(\.id), [1, 2])
        sections = MeetingReaderLayout.split([a, b, MeetingCaptionRow(
            id: 3, source: "Third confirmed", chinese: "第三句", final: true)])
        XCTAssertEqual(sections.current?.id, 3)
        XCTAssertEqual(sections.history.map(\.id), [1, 2])
        XCTAssertNil(MeetingReaderLayout.split([]).current)
    }

    func testUnreadStillIncreasesWhenBoundedHistoryCountStaysConstant() {
        XCTAssertEqual(MeetingReaderLayout.unread(previous: 512, current: 512, unread: 3,
            following: false, previousID: 600, currentID: 601), 4)
        XCTAssertEqual(MeetingReaderLayout.unread(previous: 512, current: 512, unread: 4,
            following: true, previousID: 600, currentID: 601), 0)
        XCTAssertEqual(MeetingReaderLayout.unread(previous: 512, current: 1, unread: 4,
            following: false, previousID: 600, currentID: 1), 0)
    }

    func testSharedWindowsReadingLayoutStateSnapshots() {
        // Fixed reference states from core/meeting PaneMinimums, DefaultPaneWeights,
        // PaneHeights(650). This tests layout before native fonts and DPI painting.
        let samples: [(Bool, Bool, [Int], [CGFloat])] = [
            (false, false, [0, 1, 2, 3, 4, 5],
             [109.10, 69.12, 97.10, 84.25, 232.74, 57.69]),
            (true, false, [0, 1, 2],
             [253.86, 154.27, 241.86]),
            (false, true, [0, 1, 2, 4, 5],
             [123.71, 77.71, 111.71, 272.29, 64.57]),
        ]
        for (captionsOnly, expanded, indexes, expected) in samples {
            let visible = MeetingReaderLayout.visible(captionsOnly: captionsOnly, expanded: expanded)
            XCTAssertEqual(visible, indexes)
            let result = MeetingReaderLayout.heights(space: 650,
                weights: MeetingReaderLayout.defaults, visible: visible)
            XCTAssertEqual(result.reduce(0, +), 650, accuracy: 0.01)
            for (index, target) in zip(indexes, expected) {
                XCTAssertEqual(result[index], target, accuracy: 0.02)
            }
        }
    }

    func testRestoredMeetingWindowStaysOnConnectedScreen() {
        let primary = CGRect(x: 0, y: 0, width: 1440, height: 900)
        let left = CGRect(x: -1920, y: 0, width: 1920, height: 1080)
        let onLeft = CGRect(x: -1600, y: 120, width: 780, height: 840)
        XCTAssertEqual(MeetingReaderLayout.restoredFrame(onLeft, displays: [primary, left]), onLeft)
        let disconnected = MeetingReaderLayout.restoredFrame(onLeft, displays: [primary])
        XCTAssertEqual(disconnected.minX, 0)
        XCTAssertEqual(disconnected.width, 780)
        XCTAssertEqual(disconnected.height, 840)
        let partiallyOffscreen = CGRect(x: 1300, y: 850, width: 780, height: 840)
        let clamped = MeetingReaderLayout.restoredFrame(partiallyOffscreen, displays: [primary])
        XCTAssertEqual(clamped.maxX, primary.maxX)
        XCTAssertEqual(clamped.maxY, primary.maxY)
    }


    func testNewEnglishExpressionNeverStealsBusyFrozenOrHistoricalReader() {
        let ready: (String, String, Bool, Bool, Bool, Bool) -> Bool =
            MeetingReaderLayout.shouldFocusExpression
        XCTAssertTrue(ready("Let's proceed.", "", true, false, true, false))
        XCTAssertFalse(ready("Let's proceed.", "", false, false, true, false))
        XCTAssertFalse(ready("Let's proceed.", "", true, true, true, false))
        XCTAssertFalse(ready("Let's proceed.", "", true, false, true, true))
        XCTAssertFalse(ready("Let's proceed.", "Let's proceed.", true, false, true, false))
        XCTAssertFalse(ready("", "", true, false, true, false))
        XCTAssertFalse(ready("Let's proceed.", "", true, false, false, false))
    }

}
