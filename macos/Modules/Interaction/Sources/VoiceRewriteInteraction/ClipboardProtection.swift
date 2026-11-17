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

    /// Keep one input method when two dictations finish close together.
    /// The previous paste must release its clipboard lease before a new
    /// transcript is published. Cancellation interrupts the queue.
    public func beginWhenAvailable(_ text: String,
                                   stillValid: () -> Bool) async throws -> UUID {
        let deadline = ProcessInfo.processInfo.systemUptime +
            Double(PasteTiming.maxMilliseconds + PasteTiming.minimumMilliseconds) / 1000
        while true {
            guard stillValid(), !Task<Never, Never>.isCancelled else {
                throw InteractionFailure.cancelled
            }
            do {
                return try beginTemporaryText(text)
            } catch ClipboardFailure.busy {
                guard ProcessInfo.processInfo.systemUptime < deadline else {
                    throw ClipboardFailure.busy
                }
                try await Task.sleep(nanoseconds: 50_000_000)
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

    /// The timestamp is taken immediately before posting the key event.
    /// Using an after-post timestamp loses a fast consumer's first and only
    /// promised-data callback. Another clipboard reader can also fetch early,
    /// so never restore before the minimum visibility interval.
    public func restoreAfterPaste(_ id: UUID, postingStartedAt: TimeInterval) {
        pollRestore(id, postingStartedAt: postingStartedAt)
    }

    private func pollRestore(_ id: UUID, postingStartedAt: TimeInterval) {
        let now = ProcessInfo.processInfo.systemUptime
        let status = synchronized { () -> (Bool, TimeInterval?) in
            guard let held = lease, held.id == id else { return (false, nil) }
            return (true, (board as? any ClipboardReadReceiptBoard)?.lastTextReadAt())
        }
        guard status.0 else { return }
        let receipt = status.1.flatMap { $0 >= postingStartedAt ? now - $0 : nil }
        if PasteTiming.restoreReady(
            elapsed: now - postingStartedAt,
            sinceLastRead: receipt,
            hasRead: receipt != nil) {
            try? finish(id)
            return
        }
        // NSPasteboard is AppKit-owned; polling and restoration run on main.
        DispatchQueue.main.asyncAfter(
            deadline: .now() + Double(PasteTiming.pollMilliseconds) / 1000) { [self] in
            pollRestore(id, postingStartedAt: postingStartedAt)
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
