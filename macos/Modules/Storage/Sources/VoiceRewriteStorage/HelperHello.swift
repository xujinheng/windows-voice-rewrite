public func helperHelloAccepted(ok: Bool, name: String?, version: Int?) -> Bool {
    ok && name == ContractLimits.helperName && version == ContractLimits.protocolVersion
}

public func helperConnectedLabel() -> String {
    "\(ContractLimits.helperName) / JSONL v\(ContractLimits.protocolVersion)"
}
