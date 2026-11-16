import Foundation

public enum HelperConnection: Equatable, Sendable {
    case bundleMissing
    case startFailed
    case disconnected(String)
    case protocolRejected
    case connected
    case handshakeFailed
}

public struct HelperConnectionEffect: Equatable, Sendable {
    public var failedDetail: String? = nil
    public var connectedLabel: String? = nil
    public var serviceStatus: String? = nil
    public var probeStatus: String? = nil
    public var cancelProbes = false
    public var invalidatePending = false
    public var checkServices = false
    public var refreshShortcuts = false
    public var logServiceStatus = false
}

/// The helper connection outcome. A rejected handshake and a failed handshake use different sentences.
/// A dropped connection clears both probes. A missing bundle leaves the starting status alone.
public func helperConnectionEffect(_ event: HelperConnection) -> HelperConnectionEffect {
    switch event {
    case .bundleMissing:
        return HelperConnectionEffect(failedDetail: helperBundleMissing())
    case .startFailed:
        return HelperConnectionEffect(failedDetail: helperStartFailed(), serviceStatus: helperUnavailable())
    case .disconnected(let detail):
        return HelperConnectionEffect(
            failedDetail: detail,
            serviceStatus: helperDisconnected(),
            probeStatus: helperProbeDisconnected(),
            cancelProbes: true,
            invalidatePending: true)
    case .protocolRejected:
        return HelperConnectionEffect(failedDetail: helperProtocolRejected(), serviceStatus: helperHandshakeFailedStatus())
    case .connected:
        return HelperConnectionEffect(
            connectedLabel: helperConnectedLabel(),
            serviceStatus: helperConnected(),
            checkServices: true,
            refreshShortcuts: true,
            logServiceStatus: true)
    case .handshakeFailed:
        return HelperConnectionEffect(failedDetail: helperHandshakeFailed(), serviceStatus: helperUnavailable())
    }
}
