import AppIntents

struct TogglePanelIntent: AppIntent {
    static let title: LocalizedStringResource = "Toggle Tusker Panel"
    static let openAppWhenRun = true
    func perform() async throws -> some IntentResult { await MainActor.run { ShellRouter.shared.toggle() }; return .result() }
}

struct OpenReviewQueueIntent: AppIntent {
    static let title: LocalizedStringResource = "Open Tusker Review Queue"
    static let openAppWhenRun = true
    func perform() async throws -> some IntentResult { await MainActor.run { ShellRouter.shared.show(path: "/panel?shell=1") }; return .result() }
}

struct OpenTaskIntent: AppIntent {
    static let title: LocalizedStringResource = "Open Tusker Task"
    static let openAppWhenRun = true
    @Parameter(title: "Task ID") var taskID: String
    func perform() async throws -> some IntentResult { await MainActor.run { ShellRouter.shared.showTask(id: taskID) }; return .result() }
}

struct TuskerShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(intent: TogglePanelIntent(), phrases: ["Toggle \(.applicationName) panel"], shortTitle: "Toggle panel", systemImageName: "menubar.rectangle")
        AppShortcut(intent: OpenReviewQueueIntent(), phrases: ["Open \(.applicationName) review queue"], shortTitle: "Review queue", systemImageName: "checklist")
    }
}
