import Foundation
import VoiceRewriteInteraction

struct ShortcutBinding {
    var normalized: String
    var key: String
    var mouse: Bool
    var ctrl: Bool
    var alt: Bool
    var shift: Bool

    init?(canonical: String) {
        let parts = canonical.split(separator: "+").map { $0.trimmingCharacters(in: .whitespaces) }
        guard let last = parts.last, !last.isEmpty else { return nil }
        var ctrl = false
        var alt = false
        var shift = false
        var usedModifiers = Set<String>()
        for token in parts.dropLast() {
            let name: String
            switch token.lowercased() {
            case "ctrl", "control": name = "ctrl"; ctrl = true
            case "alt", "option": name = "alt"; alt = true
            case "shift": name = "shift"; shift = true
            default: return nil
            }
            guard usedModifiers.insert(name).inserted else { return nil }
        }
        let isMouse = last.lowercased() == "mouse4" || last.lowercased() == "mouse5"
        guard !isMouse || usedModifiers.isEmpty else { return nil }
        normalized = canonical
        key = String(last)
        mouse = isMouse
        self.ctrl = ctrl
        self.alt = alt
        self.shift = shift
    }

    init?(_ result: [String: JSONValue]?) {
        guard let result,
              let key = result["key"]?.string,
              let normalized = result["normalized"]?.string else { return nil }
        self.normalized = normalized
        self.key = key
        self.mouse = result["mouse"]?.boolValue == true
        self.ctrl = result["ctrl"]?.boolValue == true
        self.alt = result["alt"]?.boolValue == true
        self.shift = result["shift"]?.boolValue == true
    }
}

/// A completely validated native binding plan. Invalid or colliding shortcuts
/// never replace the currently registered global hotkeys.
struct MacShortcutPlan: Equatable {
    let dictation: HotKeyShortcut?
    let rewrite: HotKeyShortcut?
    let textEdit: HotKeyShortcut?
    let sideButtons: SideMouseBindings

    init?(dictation: ShortcutBinding, rewrite: ShortcutBinding, textEdit: ShortcutBinding) {
        guard !textEdit.mouse else { return nil }
        let dictationKey = dictation.mouse ? nil : MacShortcut.hotKey(
            key: dictation.key, ctrl: dictation.ctrl, alt: dictation.alt, shift: dictation.shift)
        let rewriteKey = rewrite.mouse ? nil : MacShortcut.hotKey(
            key: rewrite.key, ctrl: rewrite.ctrl, alt: rewrite.alt, shift: rewrite.shift)
        let textKey = MacShortcut.hotKey(
            key: textEdit.key, ctrl: textEdit.ctrl, alt: textEdit.alt, shift: textEdit.shift)
        guard (dictation.mouse || dictationKey != nil),
              (rewrite.mouse || rewriteKey != nil),
              let textKey else { return nil }
        let keys = [dictationKey, rewriteKey, textKey].compactMap { $0 }
        guard Set(keys).count == keys.count else { return nil }

        let explicitMouse = dictation.mouse || rewrite.mouse
        func button(_ binding: ShortcutBinding) -> Int? {
            guard binding.mouse else { return nil }
            switch binding.key.lowercased() {
            case "mouse4": return 3
            case "mouse5": return 4
            default: return nil
            }
        }
        let mouse = explicitMouse
            ? SideMouseBindings(dictationButton: button(dictation), rewriteButton: button(rewrite))
            : SideMouseBindings()
        guard mouse.isValid,
              (!dictation.mouse || button(dictation) != nil),
              (!rewrite.mouse || button(rewrite) != nil) else { return nil }
        self.dictation = dictationKey
        self.rewrite = rewriteKey
        self.textEdit = textKey
        self.sideButtons = mouse
    }

    var keyboard: [HotKeyShortcut] {
        [dictation, rewrite, textEdit].compactMap { $0 }
    }

}

@MainActor
enum ShortcutSaveTransaction {
    static func run(activate: () -> Bool, persist: () throws -> Void,
                    restore: () -> Void) throws -> Bool {
        guard activate() else { return false }
        do {
            try persist()
            return true
        } catch {
            restore()
            throw error
        }
    }
}

enum MacShortcut {
    static func hotKey(key: String, ctrl: Bool, alt: Bool, shift: Bool) -> HotKeyShortcut? {
        guard let code = keyCodes[key] ?? keyCodes[key.uppercased()] else { return nil }
        var modifiers: HotKeyModifiers = []
        if ctrl { modifiers.insert(.control) }
        if alt { modifiers.insert(.option) }
        if shift { modifiers.insert(.shift) }
        if !functionKeyCodes.contains(code) && (modifiers.isEmpty || modifiers == [.shift]) { return nil }
        return HotKeyShortcut(keyCode: code, modifiers: modifiers)
    }

    static func display(_ canonical: String) -> String {
        canonical.replacingOccurrences(of: "Alt", with: "Option")
    }

    private static let functionKeyCodes: Set<UInt32> = [122, 120, 99, 118, 96, 97, 98, 100, 101, 109, 103, 111, 105, 107, 113, 106, 64, 79, 80, 90]

    private static let keyCodes: [String: UInt32] = [
        "A": 0, "S": 1, "D": 2, "F": 3, "H": 4, "G": 5, "Z": 6, "X": 7, "C": 8, "V": 9,
        "B": 11, "Q": 12, "W": 13, "E": 14, "R": 15, "Y": 16, "T": 17, "O": 31, "U": 32,
        "I": 34, "P": 35, "L": 37, "J": 38, "K": 40, "N": 45, "M": 46,
        "0": 29, "1": 18, "2": 19, "3": 20, "4": 21, "5": 23, "6": 22, "7": 26, "8": 28, "9": 25,
        "F1": 122, "F2": 120, "F3": 99, "F4": 118, "F5": 96, "F6": 97, "F7": 98, "F8": 100,
        "F9": 101, "F10": 109, "F11": 103, "F12": 111,
        "F13": 105, "F14": 107, "F15": 113, "F16": 106, "F17": 64,
        "F18": 79, "F19": 80, "F20": 90,
        "Space": 49, "Tab": 48, "Enter": 36,
        "Left": 123, "Right": 124, "Down": 125, "Up": 126,
        "Home": 115, "End": 119, "PageUp": 116, "PageDown": 121,
    ]
}
