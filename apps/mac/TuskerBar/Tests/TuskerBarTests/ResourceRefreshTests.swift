import XCTest
import JavaScriptCore
@testable import TuskerBar

final class ResourceRefreshTests: XCTestCase {
    func testSSEParserPreservesSplitUnicodeAndDropsOversizedEvents() {
        let title = "தமிழ் café 🐘"
        let json = "{\"id\":7,\"kind\":\"task_review\",\"title\":\"\(title)\",\"keys\":[\"tasks\"]}"
        var parser = SSEParser()
        var messages: [(id: String?, data: String)] = []
        // Exercise every possible UTF-8 boundary, including within emoji.
        for byte in Data("id: 7\r\ndata: \(json)\r\n\r\n".utf8) { messages += parser.feed(Data([byte])) }
        XCTAssertEqual(messages.count, 1)
        XCTAssertEqual(messages[0].data, json)
        XCTAssertEqual(try? JSONDecoder().decode(TuskerStreamEvent.self, from: Data(messages[0].data.utf8)).title, title)

        XCTAssertTrue(parser.feed(Data(("data: " + String(repeating: "x", count: SSEParser.maximumEventBytes + 1) + "\n\n").utf8)).isEmpty)
        let recovered = parser.feed(Data("id: 8\ndata: recovered\n\n".utf8))
        XCTAssertEqual(recovered.count, 1)
        XCTAssertEqual(recovered[0].id, "8")
        XCTAssertEqual(recovered[0].data, "recovered")
    }

    @MainActor
    func testSummaryPublishesDuringSustainedSameEndpointEvents() async {
        let url = URL(string: "http://127.0.0.1:7420")!
        let starts = (0..<3).map { expectation(description: "summary \($0)") }
        let published = (0..<3).map { expectation(description: "badge \($0)") }
        var replies: [CheckedContinuation<TuskerSummary, Never>] = []
        var requests = 0
        var updates: [Int] = []
        let refresher = SummaryRefresher { _ in
            requests += 1
            return await withCheckedContinuation { continuation in
                replies.append(continuation)
                starts[requests - 1].fulfill()
            }
        }
        let update: (TuskerSummary) -> Void = { summary in
            updates.append(summary.attention)
            published[updates.count - 1].fulfill()
        }
        refresher.refresh(baseURL: url, onUpdate: update)
        for index in 0..<3 {
            await fulfillment(of: [starts[index]], timeout: 1)
            if index < 2 {
                for _ in 0..<100 { refresher.refresh(baseURL: url, onUpdate: update) }
            }
            replies.removeFirst().resume(returning: summary(attention: index + 1))
            await fulfillment(of: [published[index]], timeout: 1)
        }
        XCTAssertEqual(updates, [1, 2, 3], "same-endpoint traffic must not starve badge updates")
        XCTAssertEqual(requests, 3)
    }

    @MainActor
    func testSummaryBurstHasOneTrailingRequestAndDropsSupersededEndpointResponse() async {
        let oldURL = URL(string: "http://127.0.0.1:7420")!
        let newURL = URL(string: "http://127.0.0.1:7421")!
        let firstStarted = expectation(description: "first summary")
        let trailingStarted = expectation(description: "trailing summary")
        let published = expectation(description: "latest summary published")
        var urls: [URL] = []
        var replies: [CheckedContinuation<TuskerSummary, Never>] = []
        var updates: [Int] = []
        let refresher = SummaryRefresher { url in
            urls.append(url)
            return await withCheckedContinuation { continuation in
                replies.append(continuation)
                (urls.count == 1 ? firstStarted : trailingStarted).fulfill()
            }
        }
        let update: (TuskerSummary) -> Void = { summary in
            updates.append(summary.attention)
            published.fulfill()
        }
        refresher.refresh(baseURL: oldURL, onUpdate: update)
        await fulfillment(of: [firstStarted], timeout: 1)
        for _ in 0..<100 { refresher.refresh(baseURL: newURL, onUpdate: update) }
        XCTAssertEqual(urls, [oldURL], "an event burst must not start concurrent requests")
        replies.removeFirst().resume(returning: summary(attention: 9))
        await fulfillment(of: [trailingStarted], timeout: 1)
        XCTAssertEqual(urls, [oldURL, newURL])
        XCTAssertTrue(updates.isEmpty, "a response superseded by a newer event or endpoint must not change the badge")
        replies.removeFirst().resume(returning: summary(attention: 2))
        await fulfillment(of: [published], timeout: 1)
        XCTAssertEqual(updates, [2])
    }

    @MainActor
    func testSpotlightSerializesBurstsAndWritesOnlyChangedOrRemovedRecords() async {
        let url = URL(string: "http://127.0.0.1:7420")!
        let projectA = SpotlightRecordBuilder.project(SpotlightProject(id: "a", name: "A"))
        let projectB = SpotlightRecordBuilder.project(SpotlightProject(id: "b", name: "B"))
        let oldTask = task(project: "a", id: "APP-T-0001", title: "Old")
        let newTask = task(project: "a", id: "APP-T-0002", title: "New")
        let untouched = task(project: "b", id: "APP-T-0003", title: "Untouched")
        let starts = (0..<4).map { expectation(description: "fetch \($0)") }
        let writes = (0..<4).map { expectation(description: "write \($0)") }
        var scopes: [Set<String>?] = []
        var replies: [CheckedContinuation<[SpotlightRecord], Never>] = []
        var writeReplies: [CheckedContinuation<Void, Never>] = []
        var changes: [(SpotlightChanges, Bool)] = []
        let indexer = SpotlightIndexer(refreshDelayNanoseconds: 0, fetch: { _, scope in
            scopes.append(scope)
            return await withCheckedContinuation { continuation in
                replies.append(continuation)
                starts[scopes.count - 1].fulfill()
            }
        }, write: { change, clearDomain in
            changes.append((change, clearDomain))
            await withCheckedContinuation { continuation in
                writeReplies.append(continuation)
                writes[changes.count - 1].fulfill()
            }
        })

        indexer.refresh(baseURL: url)
        await fulfillment(of: [starts[0]], timeout: 1)
        replies.removeFirst().resume(returning: [projectA, projectB, oldTask, untouched])
        await fulfillment(of: [writes[0]], timeout: 1)
        XCTAssertNil(scopes[0])
        XCTAssertTrue(changes[0].1, "startup clears stale records from earlier launches once")

        indexer.refresh(baseURL: url, projectID: "a")
        XCTAssertEqual(scopes.count, 1, "a slow index write must finish before another fetch")
        writeReplies.removeFirst().resume()
        await fulfillment(of: [starts[1]], timeout: 1)
        for _ in 0..<100 { indexer.refresh(baseURL: url, projectID: "b") }
        XCTAssertEqual(scopes.count, 2, "scoped events during a fetch must queue one trailing pass")
        replies.removeFirst().resume(returning: [projectA, newTask])
        await fulfillment(of: [writes[1]], timeout: 1)
        XCTAssertEqual(scopes.count, 2, "trailing fetches wait for indexing to finish")
        writeReplies.removeFirst().resume()
        await fulfillment(of: [starts[2]], timeout: 1)
        XCTAssertEqual(scopes[1], Set(["a"]))
        XCTAssertEqual(scopes[2], Set(["b"]))
        XCTAssertEqual(changes[1].0.updated, [newTask])
        XCTAssertEqual(changes[1].0.removed, [oldTask.route.identifier])
        XCTAssertFalse(changes[1].1, "event refreshes must not delete the whole domain")

        replies.removeFirst().resume(returning: [projectB, untouched])
        await fulfillment(of: [writes[2]], timeout: 1)
        XCTAssertTrue(changes[2].0.updated.isEmpty, "unchanged records need no reindexing")
        XCTAssertTrue(changes[2].0.removed.isEmpty, "a scoped pass preserves other projects")

        indexer.refresh(baseURL: url)
        writeReplies.removeFirst().resume()
        await fulfillment(of: [starts[3]], timeout: 1)
        replies.removeFirst().resume(returning: [projectA])
        await fulfillment(of: [writes[3]], timeout: 1)
        XCTAssertNil(scopes[3], "replay misses and reconciliation still request all projects")
        XCTAssertTrue(changes[3].0.updated.isEmpty)
        XCTAssertEqual(changes[3].0.removed, [projectB.route.identifier, newTask.route.identifier, untouched.route.identifier].sorted())
        XCTAssertFalse(changes[3].1)
        writeReplies.removeFirst().resume()
    }

    @MainActor
    func testSpotlightIgnoresRuntimeOnlyTopics() {
        func event(keys: [String]) -> TuskerStreamEvent {
            TuskerStreamEvent(id: 1, kind: "projection_refreshed", project: "a", taskID: nil, title: nil,
                              status: nil, urgency: nil, deepLinkPath: nil, occurredAt: nil, keys: keys)
        }
        XCTAssertFalse(SpotlightIndexer.affectsIndex(event(keys: ["runs", "needs", "attempts", "roster", "agent-messages"])))
        XCTAssertTrue(SpotlightIndexer.affectsIndex(event(keys: ["tasks"])))
        XCTAssertTrue(SpotlightIndexer.affectsIndex(event(keys: ["gates"])))
        XCTAssertTrue(SpotlightIndexer.affectsIndex(event(keys: ["projects"])))
    }

    @MainActor
    func testSpotlightRebuildsAfterDeleteSucceedsAndIndexingFails() async {
        let url = URL(string: "http://127.0.0.1:7420")!
        let project = SpotlightRecordBuilder.project(SpotlightProject(id: "a", name: "A"))
        let original = task(project: "a", id: "APP-T-0001", title: "Original")
        let replacement = task(project: "a", id: "APP-T-0002", title: "Replacement")
        let writes = (0..<3).map { expectation(description: "write \($0)") }
        var fetches = 0
        var attempts = 0
        var scopes: [Set<String>?] = []
        var disk: [String: SpotlightRecord] = [:]
        let indexer = SpotlightIndexer(refreshDelayNanoseconds: 0, fetch: { _, scope in
            scopes.append(scope)
            fetches += 1
            return [project, fetches == 2 ? replacement : original]
        }, write: { changes, clearDomain in
            attempts += 1
            if clearDomain { disk.removeAll() }
            for id in changes.removed { disk.removeValue(forKey: id) }
            if attempts == 2 {
                writes[1].fulfill()
                throw URLError(.cannotWriteToFile)
            }
            for record in changes.updated { disk[record.route.identifier] = record }
            if attempts == 3 { XCTAssertTrue(clearDomain, "a partial write leaves the physical index unknown") }
            writes[attempts - 1].fulfill()
        })
        indexer.refresh(baseURL: url)
        await fulfillment(of: [writes[0]], timeout: 1)
        indexer.refresh(baseURL: url, projectID: "a")
        await fulfillment(of: [writes[1]], timeout: 1)
        XCTAssertNil(disk[original.route.identifier], "the failed pass already deleted the old item")
        indexer.refresh(baseURL: url, projectID: "a")
        await fulfillment(of: [writes[2]], timeout: 1)
        XCTAssertNil(scopes[2], "recovery must read all projects, including unchanged records")
        XCTAssertEqual(disk[original.route.identifier], original, "an identical reintroduced item must be restored after partial failure")
        XCTAssertNil(disk[replacement.route.identifier])
    }

    func testImagePickerBoundsReadsAndKeepsPayloadAndCancellationContract() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("tusker-image-\(UUID().uuidString).png")
        defer { try? FileManager.default.removeItem(at: url) }
        let data = Data([0, 1, 2, 3])
        try data.write(to: url)
        let script = await PanelController.loadImagePickerResponseScript(requestID: "image-1", url: url)
        let selected = try imagePayload(script)
        let file = try XCTUnwrap(selected["file"] as? [String: String])
        XCTAssertEqual(file["name"], url.lastPathComponent)
        XCTAssertEqual(file["mime"], "image/png")
        XCTAssertEqual(file["data"], data.base64EncodedString())

        try Data(repeating: 1, count: PanelController.imageSizeLimit).write(to: url)
        XCTAssertEqual(try PanelController.readImage(url: url).count, PanelController.imageSizeLimit)
        let handle = try FileHandle(forWritingTo: url)
        try handle.truncate(atOffset: 256 * 1024 * 1024)
        try handle.close()
        XCTAssertThrowsError(try PanelController.readImage(url: url), "oversized files are rejected before reading")
        let rejected = try imagePayload(await PanelController.loadImagePickerResponseScript(requestID: "image-2", url: url))
        XCTAssertNil(rejected["file"])
        XCTAssertEqual(rejected["error"] as? String, "That image could not be read; keep the file under 8 MB.")

        let cancelled = try imagePayload(await PanelController.loadImagePickerResponseScript(requestID: "image-3", url: nil))
        XCTAssertEqual(cancelled["requestId"] as? String, "image-3")
        XCTAssertNil(cancelled["file"])
        XCTAssertNil(cancelled["error"])
    }

    @MainActor
    func testPanelBridgePublishesExplicitVisibility() throws {
        let context = try XCTUnwrap(JSContext())
        context.evaluateScript("""
        var window = {location: {origin: 'http://127.0.0.1:7420'}, dispatchEvent: function(event) { this.lastEvent = event; }};
        function CustomEvent(type, options) { this.type = type; this.detail = options.detail; }
        """)
        let bridge = PanelController.bridgeScript(appVersion: "1", origin: "http://127.0.0.1:7420")
        context.evaluateScript(bridge)
        XCTAssertNil(context.exception)
        XCTAssertFalse(context.evaluateScript("window.tuskerShell.isVisible").toBool())
        context.evaluateScript(PanelController.visibilityScript(visible: true))
        XCTAssertTrue(context.evaluateScript("window.tuskerShell.isVisible").toBool())
        XCTAssertEqual(context.evaluateScript("window.lastEvent.type").toString(), "tusker-shell-visibility")
        XCTAssertTrue(context.evaluateScript("window.lastEvent.detail.visible").toBool())
        context.evaluateScript(PanelController.visibilityScript(visible: false))
        XCTAssertFalse(context.evaluateScript("window.lastEvent.detail.visible").toBool())
    }

    private func summary(attention: Int) -> TuskerSummary {
        TuskerSummary(attention: attention, review: 0, running: 0, failedRecent: 0, generatedAt: "now")
    }

    private func task(project: String, id: String, title: String) -> SpotlightRecord {
        SpotlightRecordBuilder.task(SpotlightTask(id: id, title: title, projectID: project, status: "ready", readiness: "ready", epicTitle: "App"))
    }

    private func imagePayload(_ script: String) throws -> [String: Any] {
        let json = script.dropFirst("window.tuskerShell?.receiveImagePick(".count).dropLast(2)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
    }
}
