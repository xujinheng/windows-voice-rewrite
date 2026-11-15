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

/// Optional clipboard staging ONLY; never sends Cmd+V or bypasses guarded AX writing.
/// The default text editing path never touches the clipboard.
public actor ClipboardProtector {
    private let board: any ClipboardBoard
    private var lease: (id: UUID, before: ClipboardContents, ownedChangeCount: Int)?

    public init(board: any ClipboardBoard) { self.board = board }

    public func beginTemporaryText(_ text: String) throws -> UUID {
        guard lease == nil else { throw ClipboardFailure.busy }
        let before = try board.capture()
        let id = UUID()
        do {
            let count = try board.installTemporaryText(text, owner: id)
            guard board.ownedChangeCount(owner: id) == count else {
                // Another process modified the clipboard. Never clobber its contents.
                throw ClipboardFailure.ownershipLost
            }
            lease = (id, before, count)
            return id
        } catch {
            // Even partial write failures must not restore unless our marker proves ownership.
            if board.ownedChangeCount(owner: id) != nil {
                do { try board.restore(before) }
                catch { throw ClipboardFailure.restoreFailed }
            }
            throw error
        }
    }

    /// Unconditionally relinquishes the lease. Restores only if clipboard still belongs to us.
    public func finish(_ id: UUID) throws {
        guard let held = lease, held.id == id else { throw ClipboardFailure.ownershipLost }
        lease = nil
        guard board.ownedChangeCount(owner: id) == held.ownedChangeCount else {
            throw ClipboardFailure.ownershipLost
        }
        do { try board.restore(held.before) }
        catch { throw ClipboardFailure.restoreFailed }
    }
}
