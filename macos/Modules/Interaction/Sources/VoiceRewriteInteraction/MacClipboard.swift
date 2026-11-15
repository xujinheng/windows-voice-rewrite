#if os(macOS)
import AppKit
import Foundation

/// NSPasteboard implementation for optional guarded clipboard staging. No synthetic paste.
public final class MacClipboardBoard: ClipboardBoard, @unchecked Sendable {
    private let board: NSPasteboard
    private let ownerType = NSPasteboard.PasteboardType("com.voicerewrite.interaction.temp-owner")
    private let maxBackupBytes = 8 * 1024 * 1024

    public init(board: NSPasteboard = .general) { self.board = board }

    public func capture() throws -> ClipboardContents {
        let count = board.changeCount
        var bytes = 0
        var snapshots: [[String: Data]] = []
        for item in board.pasteboardItems ?? [] {
            var types: [String: Data] = [:]
            for type in item.types {
                // A promised/virtual type cannot be guaranteed restorable; fail closed.
                guard !type.rawValue.localizedCaseInsensitiveContains("promise"),
                      let data = item.data(forType: type) else {
                    throw ClipboardFailure.snapshotUnavailable
                }
                bytes += data.count
                guard bytes <= maxBackupBytes else { throw ClipboardFailure.snapshotUnavailable }
                types[type.rawValue] = data
            }
            snapshots.append(types)
        }
        guard board.changeCount == count else { throw ClipboardFailure.snapshotUnavailable }
        return ClipboardContents(items: snapshots, changeCount: count)
    }

    public func installTemporaryText(_ text: String, owner: UUID) throws -> Int {
        let item = NSPasteboardItem()
        guard item.setString(text, forType: .string),
              item.setString(owner.uuidString, forType: ownerType) else {
            throw ClipboardFailure.writeFailed
        }
        board.clearContents()
        guard board.writeObjects([item]) else { throw ClipboardFailure.writeFailed }
        return board.changeCount
    }

    public func ownedChangeCount(owner: UUID) -> Int? {
        guard board.pasteboardItems?.count == 1,
              board.pasteboardItems?.first?.string(forType: ownerType) == owner.uuidString else {
            return nil
        }
        return board.changeCount
    }

    public func restore(_ previous: ClipboardContents) throws {
        var items: [NSPasteboardItem] = []
        for types in previous.items {
            let item = NSPasteboardItem()
            for (name, data) in types {
                guard item.setData(data, forType: NSPasteboard.PasteboardType(name)) else {
                    throw ClipboardFailure.restoreFailed
                }
            }
            items.append(item)
        }
        board.clearContents()
        if !items.isEmpty, !board.writeObjects(items) { throw ClipboardFailure.restoreFailed }
    }
}
#endif
