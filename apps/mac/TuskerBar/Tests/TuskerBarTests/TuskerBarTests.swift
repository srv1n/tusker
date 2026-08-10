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

    @MainActor
    func testSSECursorReconnectAndReplaySemantics() {
        let event = TuskerStreamEvent(id: 7, kind: "task_review", project: "tusker", taskID: "MAC-T-0001", title: nil, status: nil, urgency: nil, deepLinkPath: nil, occurredAt: nil, keys: ["tasks"])
        let miss = TuskerStreamEvent(id: 0, kind: "stream_replay_miss", project: nil, taskID: nil, title: nil, status: nil, urgency: nil, deepLinkPath: nil, occurredAt: nil, keys: ["tasks"])
        var cursor = SSECursor()
        XCTAssertEqual(cursor.classify(event), .accepted)
        XCTAssertEqual(cursor.classify(event), .duplicate)
        XCTAssertEqual(cursor.classify(miss), .replayMiss)
        XCTAssertEqual(cursor.lastEventID, 7, "replay miss must not move the authoritative cursor")
        XCTAssertEqual(SSEClient.request(url: URL(string: "http://127.0.0.1:7420/api/stream")!, lastEventID: cursor.lastEventID).value(forHTTPHeaderField: "Last-Event-ID"), "7")
    }

    func testDeepLinksAcceptKnownRoutesOnly() {
        XCTAssertEqual(TuskerDeepLink.parse(URL(string: "tusker://task/RUN-T-0043")!), .task(id: "RUN-T-0043"))
        XCTAssertEqual(TuskerDeepLink.parse(URL(string: "tusker://open?path=%2Fp%2Ftusker%2Fwork")!), .open(path: "/p/tusker/work"))
        XCTAssertNil(TuskerDeepLink.parse(URL(string: "tusker://task/not-a-task")!))
        XCTAssertEqual(TuskerDeepLink.parse(URL(string: "tusker://spotlight/task%3Atusker%3AMAC-T-0001")!), .spotlight(identifier: "task:tusker:MAC-T-0001"))
        XCTAssertEqual(TuskerDeepLink.taskPath(projectID: "01ABC", taskID: "MAC-T-0001"), "/p/01ABC/work?task=MAC-T-0001")
    }

    func testSpotlightRoutesAreProjectQualifiedAndReversible() {
        let task = SpotlightRoute.task(projectID: "tusker", taskID: "MAC-T-0001")
        XCTAssertEqual(SpotlightRoute.from(identifier: task.identifier), task)
        XCTAssertEqual(task.path, "/p/tusker/docs?path=MAC-T-0001")
        XCTAssertEqual(SpotlightRoute.from(identifier: "project:tusker"), .project("tusker"))
        XCTAssertNil(SpotlightRoute.from(identifier: "task:MAC-T-0001"))
        let gate = SpotlightRoute.gate(projectID: "tusker", gateID: "MAC-G-0007", taskID: "MAC-T-0008")
        XCTAssertEqual(SpotlightRoute.from(identifier: gate.identifier), gate)
        XCTAssertEqual(gate.path, "/p/tusker/docs?path=MAC-T-0008&gate=MAC-G-0007")
    }

    func testSpotlightRecordIncludesTypeAheadMetadata() {
        let record = SpotlightRecordBuilder.task(SpotlightTask(
            id: "MAC-T-0001", title: "Floating panel", projectID: "tusker",
            status: "ready", readiness: "dispatchable", epicTitle: "Tusker Mac shell"
        ))
        XCTAssertEqual(record.route, .task(projectID: "tusker", taskID: "MAC-T-0001"))
        XCTAssertTrue(record.keywords.contains("Floating panel"))
        XCTAssertTrue(record.keywords.contains("MAC-T-0001"))
        XCTAssertTrue(record.title.hasPrefix("MAC-T-0001 —"))
        XCTAssertTrue(record.description.contains("Tusker Mac shell"))

        let gate = SpotlightRecordBuilder.gate(
            SpotlightGate(id: "MAC-G-0007", title: "Spotlight verification", status: "open", blocks: ["MAC-T-0008"]),
            projectID: "tusker"
        )
        XCTAssertTrue(gate.title.hasPrefix("MAC-G-0007 —"))
        XCTAssertTrue(gate.keywords.contains("MAC G007"))
    }

    func testNotificationPlanUsesTaskThreadAndCriticalInterruption() {
        let event = TuskerStreamEvent(id: 8, kind: "task_waiting_human", project: "tusker", taskID: "MAC-T-0001", title: "Panel", status: "review", urgency: "critical", deepLinkPath: "/p/tusker/work?task=MAC-T-0001", occurredAt: nil, keys: [])
        let plan = notificationPlan(for: event, preferences: NotificationPreferences(attentionEnabled: true, criticalEnabled: true))
        XCTAssertEqual(plan, NotificationPlan(identifier: "tusker.event-8", threadIdentifier: "MAC-T-0001", shouldNotify: true, timeSensitive: true))
        XCTAssertFalse(notificationPlan(for: event, preferences: NotificationPreferences(attentionEnabled: true, criticalEnabled: false))!.shouldNotify)
    }

    func testNotificationPlanUsesEventIdentityForRepeatedTaskTransitions() {
        let base = TuskerStreamEvent(id: 9, kind: "task_waiting_human", project: "tusker", taskID: "MAC-T-0001", title: nil, status: "review", urgency: "attention", deepLinkPath: nil, occurredAt: nil, keys: [])
        let later = TuskerStreamEvent(id: 10, kind: base.kind, project: base.project, taskID: base.taskID, title: base.title, status: base.status, urgency: base.urgency, deepLinkPath: base.deepLinkPath, occurredAt: base.occurredAt, keys: base.keys)
        let preferences = NotificationPreferences(attentionEnabled: true, criticalEnabled: false)
        XCTAssertNotEqual(notificationPlan(for: base, preferences: preferences)?.identifier, notificationPlan(for: later, preferences: preferences)?.identifier)
    }

    func testNotificationHistoryIsOrderedBoundedAndReplaySafe() {
        var history = BoundedNotificationHistory(limit: 256)
        for id in 0..<300 { XCTAssertTrue(history.insert("event-\(id)")) }
        XCTAssertEqual(history.ordered.count, 256)
        XCTAssertEqual(history.ordered.first, "event-44")
        XCTAssertEqual(history.ordered.last, "event-299")
        XCTAssertFalse(history.contains("event-43"))
        XCTAssertFalse(history.insert("event-299"), "replayed event must remain suppressed")
        XCTAssertEqual(history.ordered.last, "event-299")

        history.remove("event-299")
        XCTAssertTrue(history.insert("event-299"), "failed enqueue rollback must permit a retry")
        XCTAssertEqual(history.ordered.count, 256)

        let restored = BoundedNotificationHistory(history.ordered + ["event-299"], limit: 256)
        XCTAssertEqual(restored.ordered.count, 256, "persisted duplicates are collapsed without losing newer history")
        XCTAssertTrue(restored.contains("event-299"))
    }

    func testRuntimeLaunchPlanOnlyOwnsTheDefaultLocalEndpoint() {
        XCTAssertTrue(RuntimeLaunchPlan.manages(URL(string: "http://127.0.0.1:7420")!))
        XCTAssertTrue(RuntimeLaunchPlan.manages(URL(string: "http://localhost:7420")!))
        XCTAssertFalse(RuntimeLaunchPlan.manages(URL(string: "https://tusker.example")!))
        XCTAssertFalse(RuntimeLaunchPlan.manages(URL(string: "http://127.0.0.1:9000")!))
        let path = RuntimeLaunchPlan.path(home: "/Users/test", inherited: "/custom/bin:/usr/bin")
        XCTAssertTrue(path.hasPrefix("/Users/test/.local/bin:/opt/homebrew/bin"))
        XCTAssertEqual(path.components(separatedBy: ":").filter { $0 == "/usr/bin" }.count, 1)
        XCTAssertEqual(RuntimeLaunchPlan.healthTimeout, 1)
        XCTAssertEqual(RuntimeLaunchPlan.monitorHealthTimeout, 3)
        XCTAssertEqual(RuntimeLaunchPlan.startupWindow, .seconds(15))
        XCTAssertEqual(RuntimeLaunchPlan.terminationAction(for: .starting), .finishStartup)
        XCTAssertEqual(RuntimeLaunchPlan.terminationAction(for: .running), .restart)
        XCTAssertEqual(RuntimeLaunchPlan.terminationAction(for: .checking), .ignore)
        XCTAssertEqual(RuntimeLaunchPlan.terminationAction(for: .failed("boom")), .ignore)
    }

    func testRuntimeLogRedactionCoversQuotedAndSpacedSecrets() {
        let raw = #"""
Authorization: Bearer "bearer value"
"token": "token value with spaces"
password='password value with spaces'
capability = capability-value
api_key = "api key value"
safe=visible
"""#
        let redacted = String(decoding: RuntimeLogWriter.redact(Data(raw.utf8)), as: UTF8.self)
        for secret in ["bearer value", "token value", "password value", "capability-value", "api key value"] {
            XCTAssertFalse(redacted.contains(secret), redacted)
        }
        XCTAssertTrue(redacted.contains("[REDACTED]"))
        XCTAssertTrue(redacted.contains("safe=visible"))
    }

    func testRuntimeLogWriterRejectsSymlinkHardlinkAndInsecureMode() throws {
        for fixture in ["symlink", "hardlink", "mode"] {
            let home = FileManager.default.temporaryDirectory.appendingPathComponent("tusker-log-authority-\(fixture)-\(UUID().uuidString)")
            defer { try? FileManager.default.removeItem(at: home) }
            let logs = home.appendingPathComponent("Library/Application Support/tusker/logs")
            try FileManager.default.createDirectory(at: logs, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: logs.path)
            let current = logs.appendingPathComponent("app-daemon.log")
            switch fixture {
            case "symlink":
                try FileManager.default.createSymbolicLink(at: current, withDestinationURL: home.appendingPathComponent("elsewhere"))
            case "hardlink":
                XCTAssertTrue(FileManager.default.createFile(atPath: current.path, contents: Data(), attributes: [.posixPermissions: 0o600]))
                try FileManager.default.linkItem(at: current, to: logs.appendingPathComponent("other-link"))
            default:
                XCTAssertTrue(FileManager.default.createFile(atPath: current.path, contents: Data(), attributes: [.posixPermissions: 0o644]))
                try FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: current.path)
            }
            XCTAssertThrowsError(try RuntimeLogWriter(home: home.path), fixture)
        }
    }

    func testRuntimeLogWriterKeepsCurrentAndArchivesStrictlyBounded() throws {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent("tusker-log-bound-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: home) }
        let writer = try RuntimeLogWriter(home: home.path)
        for index in 0..<400 {
            writer.append(Data("line \(index) \(String(repeating: "x", count: 8_000))\n".utf8))
        }
        writer.finish(Data())
        let logs = home.appendingPathComponent("Library/Application Support/tusker/logs")
        let files = try FileManager.default.contentsOfDirectory(at: logs, includingPropertiesForKeys: [.fileSizeKey])
        XCTAssertLessThanOrEqual(files.count, 4)
        for file in files {
            let size = try file.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? Int.max
            XCTAssertLessThanOrEqual(size, 512 * 1024, file.lastPathComponent)
        }
    }

    func testRuntimeLaunchPlanRequiresExplicitRetryAfterFailure() {
        XCTAssertTrue(RuntimeLaunchPlan.shouldStart(from: .idle, force: false))
        XCTAssertTrue(RuntimeLaunchPlan.shouldStart(from: .external, force: false))
        XCTAssertFalse(RuntimeLaunchPlan.shouldStart(from: .checking, force: false))
        XCTAssertFalse(RuntimeLaunchPlan.shouldStart(from: .starting, force: false))
        XCTAssertFalse(RuntimeLaunchPlan.shouldStart(from: .running, force: false))
        XCTAssertFalse(RuntimeLaunchPlan.shouldStart(from: .failed("exit 1"), force: false))
        XCTAssertTrue(RuntimeLaunchPlan.shouldStart(from: .failed("exit 1"), force: true))
    }

    func testBundledDaemonDoesNotInheritAgentSessionIdentity() {
        let environment = RuntimeLaunchPlan.daemonEnvironment(inheriting: [
            "PATH": "/usr/bin",
            "TUSKER_ATTEMPT_ID": "attempt-1",
            "CODEX_SHELL": "1",
            "CODEX_THREAD_ID": "thread-1",
            "CLAUDECODE": "1",
            "CLAUDE_CODE_ENTRYPOINT": "cli",
        ])

        XCTAssertEqual(environment, ["PATH": "/usr/bin"])
    }

    func testRuntimeShellLoadsStoredUIOptimisticallyAndNeverCoversCommittedContent() {
        XCTAssertEqual(RuntimeShellLoadPlan.cachePolicy(for: .optimistic), .returnCacheDataElseLoad)
        XCTAssertEqual(RuntimeShellLoadPlan.cachePolicy(for: .live), .reloadRevalidatingCacheData)
        XCTAssertTrue(RuntimeShellLoadPlan.shouldCoverWebView(hasCommittedContent: false))
        XCTAssertFalse(RuntimeShellLoadPlan.shouldCoverWebView(hasCommittedContent: true))
    }

    @MainActor
    func testEditMenuRoutesStandardShortcutsThroughTheFirstResponder() {
        let menu = TuskerEditMenu.make()
        let expected: [(String, String, String, NSEvent.ModifierFlags)] = [
            ("Undo", "undo:", "z", [.command]),
            ("Redo", "redo:", "z", [.command, .shift]),
            ("Cut", "cut:", "x", [.command]),
            ("Copy", "copy:", "c", [.command]),
            ("Paste", "paste:", "v", [.command]),
            ("Select All", "selectAll:", "a", [.command]),
        ]

        XCTAssertEqual(menu.title, "Edit")
        for (title, action, key, modifiers) in expected {
            let item = menu.item(withTitle: title)
            XCTAssertEqual(item?.action, Selector((action)), title)
            XCTAssertEqual(item?.keyEquivalent, key, title)
            XCTAssertEqual(item?.keyEquivalentModifierMask, modifiers, title)
            XCTAssertNil(item?.target, "\(title) must use the first-responder chain")
        }
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
