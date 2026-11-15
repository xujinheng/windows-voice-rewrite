// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "VoiceRewriteStorage",
    platforms: [.macOS(.v12)],
    products: [.library(name: "VoiceRewriteStorage", targets: ["VoiceRewriteStorage"])],
    targets: [
        .target(name: "VoiceRewriteStorage"),
        .testTarget(name: "VoiceRewriteStorageTests", dependencies: ["VoiceRewriteStorage"]),
    ]
)
