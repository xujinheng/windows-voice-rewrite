import Foundation

// Executed on both native macOS architectures against the actual Go C archive.
// Validate hold/cancel/clock behavior first, then measure Swift -> Go costs.
let profile = VRProfile("{}")
precondition(profile != nil && VRSnapshot(54, 0).role == 1 && VRSnapshot(61, 0).role == 2 && VRSnapshot(103, 0).role == 0)
VRFree(profile)
precondition(VRCommand(VR_COMMAND_PRESS, 103, 0, 0).token == 0 && VRSnapshot(103, 0).down == 0) // unbound is never armed
let mouseProfile = VRProfile("{\"dictation_hotkey\":\"Mouse4\",\"hotkey_record\":\"RightOption\"}")
precondition(mouseProfile != nil && VRSnapshot(103, 0).role == 1)
VRFree(mouseProfile)
let first = VRCommand(VR_COMMAND_PRESS, 103, 0, 0).token
precondition(first != 0)
precondition(VRCommand(VR_COMMAND_PRESS, 104, 0, 0).token == 0)
precondition(VRCommand(VR_COMMAND_RELEASE, 104, 0, 0).release == 0 && VRSnapshot(104, 0).down == 0)
precondition(VRCommand(VR_COMMAND_ACTIVATE, 103, first, 0).kind == VR_EVENT_ACTIVATED)
precondition(VRCommand(VR_COMMAND_RELEASE, 103, 0, 0).release == 2)
let resetProfile = VRProfile("{}")
precondition(resetProfile != nil && VRSnapshot(54, 0).role == 1 && VRSnapshot(103, 0).role == 0)
VRFree(resetProfile)
precondition(VRCommand(VR_COMMAND_PRESS, 54, 0, 1).token == 0 && VRSnapshot(54, 0).down == 0)
let stale = VRCommand(VR_COMMAND_PRESS, 54, 0, 0).token
precondition(stale != 0 && VRCommand(VR_COMMAND_RELEASE, 54, 0, 0).release == 3)
precondition(VRCommand(VR_COMMAND_ACTIVATE, 54, stale, 0).kind != VR_EVENT_ACTIVATED)
let epoch = VRCommand(VR_COMMAND_START, 0, 0, 0).token
precondition(VRSnapshot(0, epoch).valid == 1)
VRCommand(VR_COMMAND_CANCEL, 0, 0, 0)
precondition(VRSnapshot(0, epoch).valid == 0)
// Lost Quartz key-up events must release Go's Hold without reusing the token:
// an already-scheduled activation must not activate the user's next press.
let canceledHold = VRCommand(VR_COMMAND_PRESS, 54, 0, 0).token
precondition(canceledHold != 0 && VRSnapshot(54, 0).down == 1 && VRCommand(VR_COMMAND_ACTIVATE, 54, canceledHold, 0).kind == VR_EVENT_ACTIVATED)
VRCommand(VR_COMMAND_CANCEL, 0, 0, 0)
precondition(VRSnapshot(54, 0).down == 0 && VRSnapshot(0, 0).armed == 0 && VRCommand(VR_COMMAND_ACTIVATE, 54, canceledHold, 0).kind != VR_EVENT_ACTIVATED)
let resumedHold = VRCommand(VR_COMMAND_PRESS, 54, 0, 0).token
precondition(resumedHold > canceledHold && VRCommand(VR_COMMAND_ACTIVATE, 54, canceledHold, 0).kind != VR_EVENT_ACTIVATED)
precondition(VRCommand(VR_COMMAND_ACTIVATE, 54, resumedHold, 0).kind == VR_EVENT_ACTIVATED && VRCommand(VR_COMMAND_RELEASE, 54, 0, 0).release == 2)
VRCommand(VR_COMMAND_CANCEL, 0, 0, 0)

var clock = [CChar](repeating: 0, count: 24)
clock.withUnsafeMutableBufferPointer { VRDuration(125, $0.baseAddress, Int32($0.count)) }
precondition(String(cString: clock) == "02:05")

let count = 150_000
var checksum = 0.0
func measure(_ name: String, _ operation: (Int) -> Void) {
    let began = DispatchTime.now().uptimeNanoseconds
    for index in 0..<count { operation(index) }
    let ns = DispatchTime.now().uptimeNanoseconds - began
    print("native Go ABI \(name): \(Double(ns) / Double(count)) ns/call")
}

measure("wave") { i in checksum += Double(VRWave(Int32(i % 9), 0.6, UInt32(i))) }
measure("spinner") { i in checksum += Double(VRSpinner(Int32(i % 8), UInt32(i))) }
measure("level") { i in checksum += VRLevel(0.5, Double(i % 100) / 1000) }
measure("clock") { i in
    clock.withUnsafeMutableBufferPointer {
        VRDuration(Int64(i % 3600), $0.baseAddress, Int32($0.count))
    }
    checksum += Double(clock[0])
}
precondition(checksum.isFinite && checksum > 0)
print("native Go ABI smoke passed; 70ms recording frame uses at most 11 direct calls")
