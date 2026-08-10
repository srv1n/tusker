import Foundation
@preconcurrency import UserNotifications

struct NotificationPlan: Equatable {
    let identifier: String
    let threadIdentifier: String
    let shouldNotify: Bool
    let timeSensitive: Bool
}

struct NotificationPreferences: Equatable {
    let attentionEnabled: Bool
    let criticalEnabled: Bool
}

struct BoundedNotificationHistory: Equatable {
    private(set) var ordered: [String]
    private var membership: Set<String>
    let limit: Int

    init(_ stored: [String] = [], limit: Int = 256) {
        self.limit = max(1, limit)
        ordered = []
        membership = []
        var newestUnique: [String] = []
        for identifier in stored.reversed() where !membership.contains(identifier) {
            newestUnique.append(identifier)
            membership.insert(identifier)
            if newestUnique.count == self.limit { break }
        }
        ordered = Array(newestUnique.reversed())
    }

    func contains(_ identifier: String) -> Bool { membership.contains(identifier) }

    @discardableResult
    mutating func insert(_ identifier: String) -> Bool {
        guard membership.insert(identifier).inserted else { return false }
        ordered.append(identifier)
        while ordered.count > limit {
            membership.remove(ordered.removeFirst())
        }
        return true
    }

    mutating func remove(_ identifier: String) {
        guard membership.remove(identifier) != nil else { return }
        ordered.removeAll { $0 == identifier }
    }
}

func notificationPlan(for event: TuskerStreamEvent, preferences: NotificationPreferences) -> NotificationPlan? {
    guard let taskID = event.taskID, let urgency = event.urgency else { return nil }
    let permitted = (urgency == "critical" && preferences.criticalEnabled) || (urgency == "attention" && preferences.attentionEnabled)
    // Stream IDs are immutable event identity. Task/kind is not: two reviews
    // for the same task are distinct transitions and must both be observable.
    let scope = event.project.map { "\($0)." } ?? ""
    return NotificationPlan(
        identifier: "\(scope)event-\(event.id)", threadIdentifier: taskID,
        shouldNotify: permitted, timeSensitive: urgency == "critical"
    )
}

@MainActor
final class NotificationCoordinator: NSObject, UNUserNotificationCenterDelegate {
    private var delivered = BoundedNotificationHistory()
    private let historyKey = "TuskerBar.notificationEventHistory.v1"
    private let historyLimit = 256
    private let panel: PanelController
    private let config: AppConfig

    init(panel: PanelController, config: AppConfig) {
        self.panel = panel
        self.config = config
        super.init()
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        let category = UNNotificationCategory(identifier: "TUSKER_EVENT", actions: [UNNotificationAction(identifier: "OPEN", title: "Open")], intentIdentifiers: [], options: [.customDismissAction])
        center.setNotificationCategories([category])
        delivered = BoundedNotificationHistory(UserDefaults.standard.array(forKey: historyKey) as? [String] ?? [], limit: historyLimit)
    }

    func requestAuthorization() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
    }

    func handle(_ event: TuskerStreamEvent) {
        let preferences = NotificationPreferences(attentionEnabled: config.notifyAttention, criticalEnabled: config.notifyCritical)
        guard let plan = notificationPlan(for: event, preferences: preferences), plan.shouldNotify, !delivered.contains(plan.identifier) else { return }
        // Reserve before enqueueing so duplicate replay deliveries racing on
        // UNUserNotificationCenter cannot produce two alerts.
        guard delivered.insert(plan.identifier) else { return }
        persistHistory()
        let content = UNMutableNotificationContent()
        content.title = event.title ?? "Tusker needs you"
        content.body = event.kind.replacingOccurrences(of: "_", with: " ")
        content.threadIdentifier = plan.threadIdentifier
        content.categoryIdentifier = "TUSKER_EVENT"
        content.userInfo = ["path": event.deepLinkPath ?? "/panel?shell=1"]
        content.interruptionLevel = plan.timeSensitive ? .timeSensitive : .active
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: plan.identifier, content: content, trigger: nil)) { [weak self] error in
            if let error {
                Task { @MainActor in
                    self?.delivered.remove(plan.identifier)
                    self?.persistHistory()
                    NSLog("Tusker notification enqueue failed: %@", error.localizedDescription)
                }
            }
        }
    }

    private func persistHistory() {
        UserDefaults.standard.set(delivered.ordered, forKey: historyKey)
        // UserDefaults writes are normally immediate, but the API is
        // intentionally non-throwing and can fail under a damaged/read-only
        // preference domain. Detect a failed round-trip so operators have a
        // visible diagnostic instead of silently assuming replay suppression
        // survived a restart.
        let persisted = UserDefaults.standard.array(forKey: historyKey) as? [String]
        if persisted != delivered.ordered {
            NSLog("Tusker notification history could not be persisted; duplicate alerts after restart remain possible")
        }
    }

    func postBridgeNotification(title: String, body: String, path: String?) {
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.userInfo = ["path": path ?? "/panel?shell=1"]
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void) {
        let path = response.notification.request.content.userInfo["path"] as? String ?? "/panel?shell=1"
        Task { @MainActor [weak self] in
            if response.actionIdentifier != UNNotificationDismissActionIdentifier {
                self?.panel.show(path: path)
            }
            completionHandler()
        }
    }
}
