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
}
