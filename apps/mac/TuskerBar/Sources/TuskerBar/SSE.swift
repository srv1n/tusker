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

final class SSEClient {
    private var task: Task<Void, Never>?
    private var lastEventID = 0

    func connect(url: URL, onEvent: @escaping (TuskerStreamEvent) -> Void, onConnection: @escaping (Bool) -> Void) {
        disconnect()
        task = Task { [weak self] in
            var delay: UInt64 = 1_000_000_000
            while !Task.isCancelled {
                do {
                    let (bytes, response) = try await URLSession.shared.bytes(for: URLRequest(url: url))
                    guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
                    await MainActor.run { onConnection(true) }
                    delay = 1_000_000_000
                    var parser = SSEParser()
                    for try await byte in bytes {
                        for message in parser.feed(Data([byte])) {
                            guard let event = try? JSONDecoder().decode(TuskerStreamEvent.self, from: Data(message.data.utf8)) else { continue }
                            guard event.id > (self?.lastEventID ?? 0) else { continue }
                            self?.lastEventID = event.id
                            await MainActor.run { onEvent(event) }
                        }
                    }
                } catch {
                    await MainActor.run { onConnection(false) }
                }
                let jitter = UInt64.random(in: 0...250_000_000)
                try? await Task.sleep(nanoseconds: delay + jitter)
                delay = min(delay * 2, 30_000_000_000)
            }
        }
    }

    func disconnect() { task?.cancel(); task = nil }
}
