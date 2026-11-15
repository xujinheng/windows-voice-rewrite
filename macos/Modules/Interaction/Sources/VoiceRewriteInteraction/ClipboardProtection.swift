import Foundation

public enum ClipboardFailure: Error, Equatable, Sendable {
    case busy
    case snapshotUnavailable
    case writeFailed
    case ownershipLost
    case restoreFailed
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
                guard board.ownedChangeCount(owner: id) == count else {
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
            return board.ownedChangeCount(owner: id) == held.ownedChangeCount
        }
    }

    /// Relinquishes the lease even if restoration is impossible.
    public func finish(_ id: UUID) throws {
        try synchronized {
            guard let held = lease, held.id == id else { throw ClipboardFailure.ownershipLost }
            lease = nil
            guard board.ownedChangeCount(owner: id) == held.ownedChangeCount else {
                throw ClipboardFailure.ownershipLost
            }
            do { try board.restore(held.before) }
            catch { throw ClipboardFailure.restoreFailed }
        }
    }
}
