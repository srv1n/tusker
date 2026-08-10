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

struct SSEParser {
    private var lineBuffer = ""
    private var dataLines: [String] = []
    private var eventID: String?

    mutating func feed(_ data: Data) -> [(id: String?, data: String)] {
        lineBuffer += String(decoding: data, as: UTF8.self)
        var result: [(id: String?, data: String)] = []
        while let range = lineBuffer.range(of: "\n") {
            var line = String(lineBuffer[..<range.lowerBound])
            lineBuffer.removeSubrange(..<range.upperBound)
            if line.hasSuffix("\r") { line.removeLast() }
            if line.isEmpty {
                if !dataLines.isEmpty {
                    result.append((eventID, dataLines.joined(separator: "\n")))
                }
                dataLines = []
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
        if event.kind == "stream_replay_miss" { return .replayMiss }
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
                    for try await byte in bytes {
                        for message in parser.feed(Data([byte])) {
                            guard !Task.isCancelled, self.connectionGeneration == generation else { return }
                            guard let event = try? JSONDecoder().decode(TuskerStreamEvent.self, from: Data(message.data.utf8)) else { continue }
                            switch self.cursor.classify(event) {
                            case .replayMiss:
                                self.diagnostics.replayMisses += 1
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
