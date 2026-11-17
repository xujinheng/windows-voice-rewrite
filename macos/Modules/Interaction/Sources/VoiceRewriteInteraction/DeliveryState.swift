// Source: voice-rewrite/internal/core/delivery
import Foundation

public enum DeliveryState: Int, Sendable {
    case blocked = 0
    case sentUnverified = 1
    case confirmed = 2
    case uncertain = 3
    public var requiresManualResult: Bool { self == .blocked || self == .uncertain }
}
