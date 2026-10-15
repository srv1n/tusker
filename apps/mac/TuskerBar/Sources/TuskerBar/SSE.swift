import Foundation

struct TuskerStreamEvent: Codable, Equatable {
    let id: Int
    let kind: String
    let project: String?
    let taskID: String?
    let title: String?
    let status: String?
    let urgency: String?
    let deepLinkPath: String?
    let occurredAt: String?
    let keys: [String]

    enum CodingKeys: String, CodingKey {
        case id, kind, project, title, status, urgency, keys
        case taskID = "task_id"
        case deepLinkPath = "deep_link_path"
        case occurredAt = "occurred_at"
    }
}

struct TuskerSummary: Codable, Equatable {
    let attention: Int
    let review: Int
    let running: Int
    let failedRecent: Int
    let generatedAt: String
    enum CodingKeys: String, CodingKey {
        case attention, review, running
        case failedRecent = "failed_recent"
        case generatedAt = "generated_at"
    }
}

@MainActor
final class SummaryRefresher {
    private let fetch: (URL) async throws -> TuskerSummary
    private var baseURL: URL?
    private var pendingURL: URL?
    private var refreshTask: Task<Void, Never>?
    private var onUpdate: ((TuskerSummary) -> Void)?

    init(fetch: ((URL) async throws -> TuskerSummary)? = nil) {
        self.fetch = fetch ?? Self.fetchSummary
    }

    func refresh(baseURL: URL, onUpdate: @escaping (TuskerSummary) -> Void) {
        self.baseURL = baseURL
        pendingURL = baseURL
        self.onUpdate = onUpdate
        guard refreshTask == nil else { return }
        refreshTask = Task { [weak self] in
            guard let self else { return }
            defer { self.refreshTask = nil }
            while let baseURL = self.pendingURL {
                self.pendingURL = nil
                do {
                    let summary = try await self.fetch(baseURL)
                    // Same-endpoint results remain useful during sustained
                    // traffic; only an endpoint change supersedes a response.
                    if self.baseURL == baseURL { self.onUpdate?(summary) }
                } catch {
                    NSLog("Tusker summary refresh failed: %@", error.localizedDescription)
                }
            }
        }
    }

    private static func fetchSummary(baseURL: URL) async throws -> TuskerSummary {
        // Read-only badge networking stays outside the origin-scoped bridge.
        let (data, response) = try await URLSession.shared.data(from: baseURL.appendingPathComponent("api/summary"))
        guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
        return try JSONDecoder().decode(TuskerSummary.self, from: data)
    }
}

struct SSEParser {
    static let maximumEventBytes = 1024 * 1024
    private var lineBuffer = Data()
    private var dataLines: [String] = []
    private var eventID: String?
    private var eventBytes = 0
    private var oversizedLine = false
    private var discardingEvent = false

    mutating func feed(_ data: Data) -> [(id: String?, data: String)] {
        var result: [(id: String?, data: String)] = []
        for byte in data {
            guard byte == 10 else {
                if !oversizedLine {
                    if lineBuffer.count < Self.maximumEventBytes { lineBuffer.append(byte) }
                    else {
                        oversizedLine = true
                        discardingEvent = true
                        dataLines.removeAll()
                        eventID = nil
                        lineBuffer.removeAll(keepingCapacity: true)
                    }
                }
                continue
            }
            // Decode complete lines so split UTF-8 scalars never become U+FFFD.
            let rawBytes = lineBuffer.count + 1
            let line = oversizedLine ? nil : String(decoding: lineBuffer, as: UTF8.self)
            lineBuffer.removeAll(keepingCapacity: true)
            oversizedLine = false
            guard var line else { continue }
            if line.hasSuffix("\r") { line.removeLast() }
            if line.isEmpty {
                if !discardingEvent, !dataLines.isEmpty {
                    result.append((eventID, dataLines.joined(separator: "\n")))
                }
                dataLines.removeAll()
                eventID = nil
                eventBytes = 0
                discardingEvent = false
                continue
            }
            guard !discardingEvent else { continue }
            eventBytes += rawBytes
            guard eventBytes <= Self.maximumEventBytes else {
                discardingEvent = true
                dataLines.removeAll()
                eventID = nil
                continue
            }
            guard !line.hasPrefix(":") else { continue }
            let pair = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
            let value = pair.count > 1 ? String(pair[1]).trimmingCharacters(in: .whitespaces) : ""
            switch pair.first {
            case "data": dataLines.append(value)
            case "id": eventID = value
            default: break
            }
        }
        return result
    }
}

enum SSEEventDisposition: Equatable { case accepted, duplicate, replayMiss }

struct SSECursor: Equatable {
    private(set) var lastEventID = 0

    mutating func classify(_ event: TuskerStreamEvent) -> SSEEventDisposition {
        if event.kind == "stream_replay_miss" {
            // A broker restart resets its IDs too. The full reconciliation
            // handles the gap; lower IDs in the new stream must be accepted.
            lastEventID = 0
            return .replayMiss
        }
        guard event.id > lastEventID else { return .duplicate }
        lastEventID = event.id
        return .accepted
    }
}

@MainActor
final class SSEClient {
    struct Diagnostics: Equatable {
        fileprivate(set) var reconnects = 0
        fileprivate(set) var replayMisses = 0
        fileprivate(set) var lastEventID = 0
        fileprivate(set) var lastError: String?
    }

    private var task: Task<Void, Never>?
    // Each connection owns a generation. Cancellation alone is not enough:
    // URLSession may deliver a final error after a replacement connection has
    // already opened, and that stale callback must not change current state.
    private var connectionGeneration: UInt64 = 0
    private var cursor = SSECursor()
    private(set) var diagnostics = Diagnostics()

    func connect(url: URL, onEvent: @escaping (TuskerStreamEvent) -> Void, onConnection: @escaping (Bool) -> Void, onReplayMiss: (() -> Void)? = nil) {
        disconnect()
        let generation = connectionGeneration
        cursor = SSECursor()
        diagnostics = Diagnostics()
        task = Task { [weak self] in
            var delay: UInt64 = 1_000_000_000
            while !Task.isCancelled {
                guard let self, self.connectionGeneration == generation else { return }
                do {
                    let request = Self.request(url: url, lastEventID: self.cursor.lastEventID)
                    let (bytes, response) = try await URLSession.shared.bytes(for: request)
                    guard !Task.isCancelled, self.connectionGeneration == generation else { return }
                    guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
                    onConnection(true)
                    delay = 1_000_000_000
                    var parser = SSEParser()
                    var chunk = Data()
                    for try await byte in bytes {
                        chunk.append(byte)
                        guard byte == 10 || chunk.count >= 16 * 1024 else { continue }
                        let messages = parser.feed(chunk)
                        chunk.removeAll(keepingCapacity: true)
                        for message in messages {
                            guard !Task.isCancelled, self.connectionGeneration == generation else { return }
                            guard let event = try? JSONDecoder().decode(TuskerStreamEvent.self, from: Data(message.data.utf8)) else { continue }
                            switch self.cursor.classify(event) {
                            case .replayMiss:
                                self.diagnostics.replayMisses += 1
                                self.diagnostics.lastEventID = self.cursor.lastEventID
                                onReplayMiss?()
                                continue
                            case .duplicate:
                                continue
                            case .accepted:
                                break
                            }
                            self.diagnostics.lastEventID = event.id
                            onEvent(event)
                        }
                    }
                } catch {
                    guard !Task.isCancelled, self.connectionGeneration == generation else { return }
                    self.diagnostics.reconnects += 1
                    self.diagnostics.lastError = error.localizedDescription
                    onConnection(false)
                }
                guard !Task.isCancelled, self.connectionGeneration == generation else { return }
                let jitter = UInt64.random(in: 0...250_000_000)
                try? await Task.sleep(nanoseconds: delay + jitter)
                delay = min(delay * 2, 30_000_000_000)
            }
        }
    }

    static func request(url: URL, lastEventID: Int) -> URLRequest {
        var request = URLRequest(url: url)
        // The cursor is the only replay authority. Never reconnect without it.
        if lastEventID > 0 { request.setValue(String(lastEventID), forHTTPHeaderField: "Last-Event-ID") }
        return request
    }

    func disconnect() {
        connectionGeneration &+= 1
        task?.cancel()
        task = nil
    }
}
