import Foundation

public enum IndicatorCancelChoice: Equatable, Sendable {
    case ignoreHold
    case dismiss
    case cancelWork
}

/// A pending preview releases the side button without starting work.
/// A finished phase only closes the pill. Anything else cancels the task.
public func indicatorCancel(pending: Bool, terminal: Bool) -> IndicatorCancelChoice {
    if pending {
        return .ignoreHold
    }
    if terminal {
        return .dismiss
    }
    return .cancelWork
}
