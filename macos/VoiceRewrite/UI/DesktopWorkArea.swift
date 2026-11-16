import Foundation

/// Same work-area clamp as internal/core/desktop. Coordinates increase from the origin.
struct DesktopWorkArea {
    var left, top, right, bottom: Int32

    func clamp(x: Int32, y: Int32, width: Int32, height: Int32) -> (Int32, Int32) {
        var maxX = right - width
        var maxY = bottom - height
        if maxX < left { maxX = left }
        if maxY < top { maxY = top }
        var x = x
        var y = y
        if x < left { x = left }
        if x > maxX { x = maxX }
        if y < top { y = top }
        if y > maxY { y = maxY }
        return (x, y)
    }
}

struct ResultRect {
    var x, y, w, h: Int32
    var hit: ResultHitRect { ResultHitRect(left: x, top: y, right: x + w, bottom: y + h) }
}

/// Card control frames. At scale 1 these match internal/core/popup.
enum ResultCardChrome {
    static func editor(width: Int32, height: Int32) -> ResultRect {
        ResultRect(x: 22, y: 59, w: width - 44, h: height - 119)
    }
    static func closeButton(width: Int32) -> ResultRect {
        ResultRect(x: width - 51, y: 11, w: 31, h: 29)
    }
    static func copyButton(width: Int32, height: Int32) -> ResultRect {
        ResultRect(x: width - 119, y: height - 48, w: 98, h: 33)
    }
}

enum ResultCardMetrics {
    static let width: CGFloat = 476

    /// Same reading-card heights as internal/core/popup.SizeForText.
    static func height(unicodeScalars: Int) -> CGFloat {
        switch unicodeScalars {
        case 1201...: return 500
        case 601...: return 430
        case 201...: return 315
        default: return 258
        }
    }

    static func clickDismisses(x: Int32, y: Int32, window: ResultHitRect, editor: ResultHitRect, buttons: [ResultHitRect]) -> Bool {
        if !window.contains(x: x, y: y) { return true }
        if editor.contains(x: x, y: y) { return false }
        for button in buttons where button.contains(x: x, y: y) { return false }
        return true
    }
}

struct ResultHitRect {
    var left, top, right, bottom: Int32
    func contains(x: Int32, y: Int32) -> Bool {
        right > left && bottom > top && x >= left && x < right && y >= top && y < bottom
    }
}
