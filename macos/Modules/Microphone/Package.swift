// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Microphone",
    platforms: [.macOS(.v12)],
    products: [.library(name: "Microphone", targets: ["Microphone"])],
    targets: [
        .target(name: "Microphone"),
        .testTarget(name: "MicrophoneTests", dependencies: ["Microphone"]),
    ]
)
