// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "VoiceRewriteInteraction",
    platforms: [.macOS(.v12)],
    products: [.library(name: "VoiceRewriteInteraction", targets: ["VoiceRewriteInteraction"])],
    targets: [
        .target(name: "VoiceRewriteInteraction"),
        .testTarget(name: "VoiceRewriteInteractionTests", dependencies: ["VoiceRewriteInteraction"])
    ]
)
