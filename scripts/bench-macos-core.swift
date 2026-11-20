import Foundation

// Executed on both native macOS architectures against the actual Go C archive.
// Validate hold/cancel/clock behavior first, then measure Swift -> Go costs.
let profile = VRProfile("{}")
precondition(profile != nil && VRAction(54) == 1 && VRAction(61) == 2 && VRAction(103) == 0)
VRFree(profile)
let first = VRPress(103, 0)
precondition(first != 0)
precondition(VRPress(104, 0) == 0)
precondition(VRRelease(104) == 0 && VRDown(104) == 0)
precondition(VRActivate(103, first) == 1)
precondition(VRRelease(103) == 2)
precondition(VRPress(54, 1) == 0 && VRDown(54) == 0)
let stale = VRPress(54, 0)
precondition(stale != 0 && VRRelease(54) == 3)
precondition(VRActivate(54, stale) == 0)
let epoch = VREpochStart()
precondition(VREpochValid(epoch) == 1)
VREpochCancel()
precondition(VREpochValid(epoch) == 0)
// Lost Quartz key-up events must release Go's Hold without reusing the token:
// an already-scheduled activation must not activate the user's next press.
let canceledHold = VRPress(54, 0)
precondition(canceledHold != 0 && VRDown(54) == 1 && VRActivate(54, canceledHold) == 1)
VREpochCancel()
precondition(VRDown(54) == 0 && VRArmed() == 0 && VRActivate(54, canceledHold) == 0)
let resumedHold = VRPress(54, 0)
precondition(resumedHold > canceledHold && VRActivate(54, canceledHold) == 0)
precondition(VRActivate(54, resumedHold) == 1 && VRRelease(54) == 2)
VREpochCancel()

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
