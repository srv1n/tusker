import Combine
import Foundation

@MainActor
final class AppConfig: ObservableObject {
    static let shared = AppConfig()

    static let defaultBaseURL = "http://127.0.0.1:7420"
    private enum Key {
        static let baseURL = "baseURL"
        static let showDockIcon = "showDockIcon"
        static let notifyAttention = "notifyAttention"
        static let notifyCritical = "notifyCritical"
        static let developerToolsEnabled = "developerToolsEnabled"
    }

    @Published var baseURLString: String { didSet { save(Key.baseURL, baseURLString) } }
    @Published var showDockIcon: Bool { didSet { save(Key.showDockIcon, showDockIcon) } }
    @Published var notifyAttention: Bool { didSet { save(Key.notifyAttention, notifyAttention) } }
    @Published var notifyCritical: Bool { didSet { save(Key.notifyCritical, notifyCritical) } }
    /// Release builds keep WebKit inspection disabled unless the user explicitly opts in.
    @Published var developerToolsEnabled: Bool { didSet { save(Key.developerToolsEnabled, developerToolsEnabled) } }

    private let defaults = UserDefaults.standard

    private init() {
        baseURLString = defaults.string(forKey: Key.baseURL) ?? Self.defaultBaseURL
        showDockIcon = defaults.object(forKey: Key.showDockIcon) as? Bool ?? true
        notifyAttention = defaults.object(forKey: Key.notifyAttention) as? Bool ?? true
        notifyCritical = defaults.object(forKey: Key.notifyCritical) as? Bool ?? true
        developerToolsEnabled = defaults.object(forKey: Key.developerToolsEnabled) as? Bool ?? false
    }

    var baseURL: URL { URL(string: baseURLString) ?? URL(string: Self.defaultBaseURL)! }
    var appVersion: String { Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev" }

    func applyBaseURL(_ value: String) -> String? {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmed), ["http", "https"].contains(url.scheme?.lowercased() ?? ""), url.host != nil else {
            return "Enter a complete http(s) URL."
        }
        baseURLString = trimmed.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        NotificationCenter.default.post(name: .tuskerConfigChanged, object: self)
        return nil
    }

    private func save(_ key: String, _ value: Any) {
        defaults.set(value, forKey: key)
        NotificationCenter.default.post(name: .tuskerConfigChanged, object: self)
    }
}

extension Notification.Name {
    static let tuskerConfigChanged = Notification.Name("TuskerConfigChanged")
}
