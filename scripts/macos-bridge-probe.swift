import Foundation

// Real macOS Process/Pipe integration smoke test against a compiled Go executable.
guard CommandLine.arguments.count == 2 else {
    fatalError("usage: swift macos-bridge-probe.swift /path/to/voicebridge")
}

let process = Process()
process.executableURL = URL(fileURLWithPath: CommandLine.arguments[1])
let input = Pipe()
let output = Pipe()
let errors = Pipe()
process.standardInput = input
process.standardOutput = output
process.standardError = errors

try process.run()
let payload = Data(#"{"v":1,"id":"swift-probe","op":"hello"}"#.utf8) + Data([10])
try input.fileHandleForWriting.write(contentsOf: payload)
try input.fileHandleForWriting.close()
let bytes = output.fileHandleForReading.readDataToEndOfFile()
process.waitUntilExit()

guard process.terminationStatus == 0 else {
    fatalError("Go helper terminated unsuccessfully")
}
let lines = String(decoding: bytes, as: UTF8.self).split(separator: "\n")
guard lines.count == 1,
      let raw = lines.first?.data(using: .utf8),
      let reply = try JSONSerialization.jsonObject(with: raw) as? [String: Any],
      reply["id"] as? String == "swift-probe",
      reply["v"] as? Int == 1,
      reply["ok"] as? Bool == true,
      let result = reply["result"] as? [String: Any],
      result["protocol"] as? Int == 1,
      result["name"] as? String == "VoiceRewrite" else {
    fatalError("invalid Go helper JSON handshake")
}

print("macOS Swift -> Go helper Process/Pipe handshake: PASS")
