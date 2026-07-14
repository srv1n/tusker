import XCTest
@testable import TuskerBar

final class TuskerBarTests: XCTestCase {
    func testSSEParserHandlesChunkedMultilineEventsAndComments() {
        var parser = SSEParser()
        XCTAssertTrue(parser.feed(Data(": keep-alive\nid: 7\ndata: {\"id\":7,\n".utf8)).isEmpty)
        let messages = parser.feed(Data("data: \"kind\":\"task_review\",\"keys\":[\"needs\"]}\n\n".utf8))
        XCTAssertEqual(messages.count, 1)
        XCTAssertEqual(messages[0].id, "7")
        XCTAssertEqual(messages[0].data, "{\"id\":7,\n\"kind\":\"task_review\",\"keys\":[\"needs\"]}")
    }

    func testDeepLinksAcceptKnownRoutesOnly() {
        XCTAssertEqual(TuskerDeepLink.parse(URL(string: "tusker://task/RUN-T-0043")!), .task(id: "RUN-T-0043"))
        XCTAssertEqual(TuskerDeepLink.parse(URL(string: "tusker://open?path=%2Fp%2Ftusker%2Fwork")!), .open(path: "/p/tusker/work"))
        XCTAssertNil(TuskerDeepLink.parse(URL(string: "tusker://task/not-a-task")!))
        XCTAssertEqual(TuskerDeepLink.taskPath(projectID: "01ABC", taskID: "MAC-T-0001"), "/p/01ABC/work?task=MAC-T-0001")
    }

    func testNotificationPlanUsesTaskThreadAndCriticalInterruption() {
        let event = TuskerStreamEvent(id: 8, kind: "task_waiting_human", project: "tusker", taskID: "MAC-T-0001", title: "Panel", status: "review", urgency: "critical", deepLinkPath: "/p/tusker/work?task=MAC-T-0001", occurredAt: nil, keys: [])
        let plan = notificationPlan(for: event, preferences: NotificationPreferences(attentionEnabled: true, criticalEnabled: true))
        XCTAssertEqual(plan, NotificationPlan(identifier: "MAC-T-0001.task_waiting_human", threadIdentifier: "MAC-T-0001", shouldNotify: true, timeSensitive: true))
        XCTAssertFalse(notificationPlan(for: event, preferences: NotificationPreferences(attentionEnabled: true, criticalEnabled: false))!.shouldNotify)
    }

    func testRuntimeLaunchPlanOnlyOwnsTheDefaultLocalEndpoint() {
        XCTAssertTrue(RuntimeLaunchPlan.manages(URL(string: "http://127.0.0.1:7420")!))
        XCTAssertTrue(RuntimeLaunchPlan.manages(URL(string: "http://localhost:7420")!))
        XCTAssertFalse(RuntimeLaunchPlan.manages(URL(string: "https://tusker.example")!))
        XCTAssertFalse(RuntimeLaunchPlan.manages(URL(string: "http://127.0.0.1:9000")!))
        let path = RuntimeLaunchPlan.path(home: "/Users/test", inherited: "/custom/bin:/usr/bin")
        XCTAssertTrue(path.hasPrefix("/Users/test/.local/bin:/opt/homebrew/bin"))
        XCTAssertEqual(path.components(separatedBy: ":").filter { $0 == "/usr/bin" }.count, 1)
    }

    @MainActor
    func testFolderPickerIsDirectoryOnlyAndSingleSelection() {
        let picker = NSOpenPanel()
        PanelController.configureFolderPicker(picker)
        XCTAssertTrue(picker.canChooseDirectories)
        XCTAssertFalse(picker.canChooseFiles)
        XCTAssertFalse(picker.allowsMultipleSelection)
        XCTAssertEqual(picker.prompt, "Choose")
    }

    @MainActor
    func testFolderPickerBridgeScopesToOriginAndRepliesForSelectionOrCancellation() {
        XCTAssertEqual(PanelController.configuredOrigin(URL(string: "https://TUSKER.example:443")!), "https://tusker.example")
        let bridge = PanelController.bridgeScript(appVersion: "1.2.3", origin: "http://127.0.0.1:7420")
        XCTAssertTrue(bridge.contains("window.location.origin !== config.origin"))
        XCTAssertTrue(bridge.contains("shell.pickFolder"))
        XCTAssertTrue(bridge.contains("shell.receiveFolderPick"))

        let selected = PanelController.folderPickerResponseScript(requestID: "request-1", path: "/Users/me/project")
        XCTAssertTrue(selected.contains("request-1"))
        XCTAssertTrue(selected.contains("\\/Users\\/me\\/project"))

        let cancelled = PanelController.folderPickerResponseScript(requestID: "request-2", path: nil)
        XCTAssertTrue(cancelled.contains("request-2"))
        XCTAssertTrue(cancelled.contains("\"path\":null"))
    }
}
