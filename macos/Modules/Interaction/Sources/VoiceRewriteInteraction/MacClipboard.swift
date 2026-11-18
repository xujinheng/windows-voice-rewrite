#if os(macOS)
import AppKit
import Foundation

/// A promised string is rendered only when the recipient actually reads it.
private final class PasteTextProvider: NSObject, NSPasteboardItemDataProvider {
    let text: String
    private let lock = NSLock()
    private var lastRead: TimeInterval?
    init(text: String) { self.text = text }

    var lastReadAt: TimeInterval? {
        lock.lock(); defer { lock.unlock() }
        return lastRead
    }

    func pasteboard(_ pasteboard: NSPasteboard?, item: NSPasteboardItem,
                    provideDataForType type: NSPasteboard.PasteboardType) {
        guard type == .string else { return }
        lock.lock()
        lastRead = ProcessInfo.processInfo.systemUptime
        lock.unlock()
        _ = item.setString(text, forType: .string)
    }

    func pasteboardFinishedWithDataProvider(_ pasteboard: NSPasteboard) {}
}

/// NSPasteboard owner and complete-format clipboard snapshot implementation.
public final class MacClipboardBoard: ClipboardReadReceiptBoard, @unchecked Sendable {
    private let board: NSPasteboard
    private let ownerType = NSPasteboard.PasteboardType("com.voicerewrite.interaction.temp-owner")
    private let maxBackupBytes = 8 * 1024 * 1024
    private let providerLock = NSLock()
    private var provider: PasteTextProvider?

    public func lastTextReadAt() -> TimeInterval? {
        providerLock.lock()
        defer { providerLock.unlock() }
        return provider?.lastReadAt
    }

    public init(board: NSPasteboard = .general) { self.board = board }

    public func capture() throws -> ClipboardContents {
        let count = board.changeCount
        var bytes = 0
        var snapshots: [[String: Data]] = []
        // A nil item list may indicate a pasteboard-server error, not an empty
        // clipboard. Never overwrite unreadable original data as "empty".
        let items: [NSPasteboardItem]
        if let available = board.pasteboardItems {
            items = available
        } else if board.types?.isEmpty == true {
            items = []
        } else {
            throw ClipboardFailure.snapshotUnavailable
        }
        // Rich binary/file and promised formats may demand a large or delayed
        // response from another process. Fail closed to the existing Unicode
        // delivery path *before* requesting those formats on the UI thread.
        guard items.count <= 16 else { throw ClipboardFailure.snapshotUnavailable }
        for item in items {
            let itemTypes = item.types
            guard itemTypes.count <= 16 else { throw ClipboardFailure.snapshotUnavailable }
            var types: [String: Data] = [:]
            for type in itemTypes {
                let name = type.rawValue.lowercased()
                let potentiallyRemoteBinary = [
                    "promise", "file-url", "public.tiff", "public.png", "public.jpeg",
                    "public.heic", "public.pdf", "public.data", "com.apple.pict",
                ].contains { name.contains($0) }
                guard !potentiallyRemoteBinary,
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

    public func installTemporaryText(_ text: String, owner: UUID, expectedChangeCount: Int) throws -> Int {
        // Never overwrite a newer clipboard owner with a previously captured snapshot.
        guard board.changeCount == expectedChangeCount else { throw ClipboardFailure.snapshotUnavailable }
        let item = NSPasteboardItem()
        // Convention-based hints to clipboard history apps; not a security boundary.
        let concealed = NSPasteboard.PasteboardType("org.nspasteboard.ConcealedType")
        let transient = NSPasteboard.PasteboardType("org.nspasteboard.TransientType")
        let nextProvider = PasteTextProvider(text: text)
        guard item.setDataProvider(nextProvider, forTypes: [.string]),
              item.setString(owner.uuidString, forType: ownerType),
              item.setData(Data(), forType: concealed),
              item.setData(Data(), forType: transient) else {
            throw ClipboardFailure.writeFailed
        }
        // Building a pasteboard item can invoke other code. Recheck immediately
        // before replacing the clipboard; NSPasteboard has no atomic CAS API.
        guard board.changeCount == expectedChangeCount else { throw ClipboardFailure.snapshotUnavailable }
        board.clearContents()
        guard board.writeObjects([item]) else { throw ClipboardFailure.writeFailed }
        providerLock.lock()
        provider = nextProvider
        providerLock.unlock()
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
        providerLock.lock()
        provider = nil
        providerLock.unlock()
    }
}

/// Both selected-text replacement and dictation use one clipboard lease owner.
public enum MacClipboardTransactions {
    public static let shared = ClipboardProtector(board: MacClipboardBoard())
}
#endif
