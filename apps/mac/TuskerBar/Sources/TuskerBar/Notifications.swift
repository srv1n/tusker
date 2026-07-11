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

func notificationPlan(for event: TuskerStreamEvent, preferences: NotificationPreferences) -> NotificationPlan? {
    guard let taskID = event.taskID, let urgency = event.urgency else { return nil }
    let permitted = (urgency == "critical" && preferences.criticalEnabled) || (urgency == "attention" && preferences.attentionEnabled)
    return NotificationPlan(
        identifier: "\(taskID).\(event.kind)", threadIdentifier: taskID,
        shouldNotify: permitted, timeSensitive: urgency == "critical"
    )
}

@MainActor
final class NotificationCoordinator: NSObject, UNUserNotificationCenterDelegate {
    private var delivered = Set<String>()
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
    }

    func requestAuthorization() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
    }

    func handle(_ event: TuskerStreamEvent) {
        let preferences = NotificationPreferences(attentionEnabled: config.notifyAttention, criticalEnabled: config.notifyCritical)
        guard let plan = notificationPlan(for: event, preferences: preferences), plan.shouldNotify, !delivered.contains(plan.identifier) else { return }
        let content = UNMutableNotificationContent()
        content.title = event.title ?? "Tusker needs you"
        content.body = event.kind.replacingOccurrences(of: "_", with: " ")
        content.threadIdentifier = plan.threadIdentifier
        content.categoryIdentifier = "TUSKER_EVENT"
        content.userInfo = ["path": event.deepLinkPath ?? "/panel?shell=1"]
        content.interruptionLevel = plan.timeSensitive ? .timeSensitive : .active
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: plan.identifier, content: content, trigger: nil)) { [weak self] error in
            guard error == nil else { return }
            Task { @MainActor in self?.delivered.insert(plan.identifier) }
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
        let identifier = response.notification.request.identifier
        Task { @MainActor [weak self] in
            self?.delivered.remove(identifier)
            if response.actionIdentifier != UNNotificationDismissActionIdentifier {
                self?.panel.show(path: path)
            }
            completionHandler()
        }
    }
}
