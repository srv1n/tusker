// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "TuskerBar",
    platforms: [.macOS(.v14)],
    products: [.executable(name: "TuskerBar", targets: ["TuskerBar"])],
    dependencies: [
        .package(url: "https://github.com/sindresorhus/KeyboardShortcuts", from: "2.4.0"),
    ],
    targets: [
        .executableTarget(
            name: "TuskerBar",
            dependencies: ["KeyboardShortcuts"]
        ),
        .testTarget(name: "TuskerBarTests", dependencies: ["TuskerBar"]),
    ]
)
