import Foundation

public enum ClipboardFailure: Error, Equatable, Sendable {
    case busy
    case snapshotUnavailable
    case writeFailed
    case ownershipLost
    case restoreFailed
}

/// Matches core `clipboard.MayRestore`: a zero sequence never proves ownership,
/// and a changed sequence belongs to someone else.
public enum ClipboardOwnership {
    public static func mayRestore(hasOriginal: Bool, owned: Int, current: Int?) -> Bool {
        guard let current else { return false }
        return hasOriginal && owned != 0 && current == owned
    }
}

/// Exact opaque bytes for every type in every pasteboard item. Empty items are preserved.
public struct ClipboardContents: Sendable, Equatable {
    public let items: [[String: Data]]
    public let changeCount: Int
    public init(items: [[String: Data]], changeCount: Int) {
        self.items = items
        self.changeCount = changeCount
    }
}

/// The board implementation must never discard unrepresentable types silently.
public protocol ClipboardBoard: Sendable {
    func capture() throws -> ClipboardContents
    func installTemporaryText(_ text: String, owner: UUID) throws -> Int
    /// Returns its change count only if our marker is still present.
    func ownedChangeCount(owner: UUID) -> Int?
    func restore(_ previous: ClipboardContents) throws
}

/// Native boards may expose a timestamp for the last actual read of staged text.
public protocol ClipboardReadReceiptBoard: ClipboardBoard {
    func lastTextReadAt() -> TimeInterval?
}

/// Serializes pasteboard leases so staging cannot race with another VoiceRewrite write.
/// A different process may take ownership at any point; never restore over its clipboard.
public final class ClipboardProtector: @unchecked Sendable {
    private let board: any ClipboardBoard
    private let lock = NSLock()
    private var lease: (id: UUID, before: ClipboardContents, ownedChangeCount: Int)?

    public init(board: any ClipboardBoard) { self.board = board }

    private func synchronized<T>(_ body: () throws -> T) rethrows -> T {
        lock.lock()
        defer { lock.unlock() }
        return try body()
    }

    public func beginTemporaryText(_ text: String) throws -> UUID {
        try synchronized {
            guard lease == nil else { throw ClipboardFailure.busy }
            let before = try board.capture()
            let id = UUID()
            do {
                let count = try board.installTemporaryText(text, owner: id)
                guard ClipboardOwnership.mayRestore(hasOriginal: true, owned: count, current: board.ownedChangeCount(owner: id)) else {
                    throw ClipboardFailure.ownershipLost
                }
                lease = (id, before, count)
                return id
            } catch {
                // A partial write may only be undone while our marker proves ownership.
                if board.ownedChangeCount(owner: id) != nil {
                    do { try board.restore(before) }
                    catch { throw ClipboardFailure.restoreFailed }
                }
                throw error
            }
        }
    }

    public func isOwned(_ id: UUID) -> Bool {
        synchronized {
            guard let held = lease, held.id == id else { return false }
            return ClipboardOwnership.mayRestore(
                hasOriginal: true, owned: held.ownedChangeCount, current: board.ownedChangeCount(owner: id))
        }
    }

    /// Restore after the target has read the pasteboard and further reads have
    /// gone quiet. If no read is observed, leave the text available until the
    /// bounded timeout; the paste keystroke may still be queued.
    public func restoreAfterPaste(_ id: UUID) {
        let started = ProcessInfo.processInfo.systemUptime
        pollRestore(id, started: started)
    }

    private func pollRestore(_ id: UUID, started: TimeInterval) {
        let now = ProcessInfo.processInfo.systemUptime
        let status = synchronized { () -> (Bool, TimeInterval?) in
            guard let held = lease, held.id == id else { return (false, nil) }
            return (true, (board as? any ClipboardReadReceiptBoard)?.lastTextReadAt())
        }
        guard status.0 else { return }
        let quiet = Double(PasteTiming.quietMilliseconds) / 1000
        let timeout = Double(PasteTiming.maxMilliseconds) / 1000
        // Ignore eager clipboard-manager reads that precede the paste event.
        let postPasteRead = status.1 != nil && status.1! >= started
        if (postPasteRead && now - status.1! >= quiet) || now - started >= timeout {
            try? finish(id)
            return
        }
        DispatchQueue.global(qos: .utility).asyncAfter(
            deadline: .now() + Double(PasteTiming.pollMilliseconds) / 1000) { [self] in
            pollRestore(id, started: started)
        }
    }

    /// Relinquishes the lease even if restoration is impossible.
    public func finish(_ id: UUID) throws {
        try synchronized {
            guard let held = lease, held.id == id else { throw ClipboardFailure.ownershipLost }
            lease = nil
            guard ClipboardOwnership.mayRestore(
                hasOriginal: true, owned: held.ownedChangeCount, current: board.ownedChangeCount(owner: id)) else {
                throw ClipboardFailure.ownershipLost
            }
            do { try board.restore(held.before) }
            catch { throw ClipboardFailure.restoreFailed }
        }
    }
}
