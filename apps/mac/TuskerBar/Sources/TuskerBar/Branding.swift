import AppKit
import Foundation

enum TuskerBranding {
    private static let iconName = "tusker-icon"
    private static let menuBarIconName = "TuskerMenuBarTemplate"

    static func icon(size: NSSize? = nil) -> NSImage? {
        guard let url = Bundle.main.url(forResource: iconName, withExtension: "png"),
              let image = NSImage(contentsOf: url) else { return nil }
        if let size { image.size = size }
        image.isTemplate = false
        return image
    }

    static func iconData() -> Data? {
        guard let url = Bundle.main.url(forResource: iconName, withExtension: "png") else { return nil }
        return try? Data(contentsOf: url)
    }

    static func menuBarIcon() -> NSImage? {
        guard let image = NSImage(named: menuBarIconName) else { return nil }
        image.size = NSSize(width: 18, height: 18)
        image.isTemplate = true
        return image
    }
}
