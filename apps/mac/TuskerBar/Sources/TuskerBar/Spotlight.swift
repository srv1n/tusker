@preconcurrency import CoreSpotlight
import Foundation
import UniformTypeIdentifiers

enum SpotlightRoute: Equatable {
    case project(String)
    case task(projectID: String, taskID: String)
    case gate(projectID: String, gateID: String, taskID: String?)

    var projectID: String {
        switch self {
        case let .project(id), let .task(id, _), let .gate(id, _, _): return id
        }
    }

    var identifier: String {
        switch self {
        case let .project(projectID): return "project:\(projectID)"
        case let .task(projectID, taskID): return "task:\(projectID):\(taskID)"
        case let .gate(projectID, gateID, taskID):
            return "gate:\(projectID):\(gateID):\(taskID ?? "")"
        }
    }

    var path: String {
        switch self {
        case let .project(projectID): return TuskerDeepLink.projectPath(projectID: projectID)
        case let .task(projectID, taskID): return TuskerDeepLink.mainTaskPath(projectID: projectID, taskID: taskID)
        case let .gate(projectID, gateID, taskID):
            guard let taskID, !taskID.isEmpty else { return TuskerDeepLink.projectPath(projectID: projectID) }
            var components = URLComponents()
            components.path = "/p/\(projectID)/docs"
            components.queryItems = [URLQueryItem(name: "path", value: taskID), URLQueryItem(name: "gate", value: gateID)]
            return components.string ?? TuskerDeepLink.projectPath(projectID: projectID)
        }
    }

    static func from(identifier: String) -> SpotlightRoute? {
        let parts = identifier.split(separator: ":", maxSplits: 3).map(String.init)
        guard parts.count >= 2 else { return nil }
        switch parts[0] {
        case "project":
            return parts.count == 2 && !parts[1].isEmpty ? .project(parts[1]) : nil
        case "task":
            guard parts.count == 3, !parts[1].isEmpty, !parts[2].isEmpty else { return nil }
            return .task(projectID: parts[1], taskID: parts[2])
        case "gate":
            guard parts.count == 4, !parts[1].isEmpty, !parts[2].isEmpty else { return nil }
            return .gate(projectID: parts[1], gateID: parts[2], taskID: parts[3].isEmpty ? nil : parts[3])
        default:
            return nil
        }
    }
}

struct SpotlightProject: Decodable, Equatable {
    let id: String
    let name: String
}

struct SpotlightTask: Decodable, Equatable {
    let id: String
    let title: String
    let projectID: String
    let status: String
    let readiness: String
    let epicTitle: String

    enum CodingKeys: String, CodingKey {
        case id, title, status, readiness, epicTitle
        case projectID = "projectId"
    }
}

struct SpotlightGate: Decodable, Equatable {
    let id: String
    let title: String
    let status: String
    let blocks: [String]
}

struct SpotlightRecord: Equatable {
    let route: SpotlightRoute
    let title: String
    let subtitle: String
    let description: String
    let keywords: [String]
}

enum SpotlightRecordBuilder {
    private static func idTerms(_ id: String) -> [String] {
        var terms = [id, id.replacingOccurrences(of: "-", with: " "), id.replacingOccurrences(of: "-", with: "")]
        let parts = id.split(separator: "-").map(String.init)
        if parts.count == 3, parts[2].count > 1 {
            let shortNumber = String(parts[2].dropFirst())
            terms += ["\(parts[0])-\(parts[1])\(shortNumber)", "\(parts[0]) \(parts[1])\(shortNumber)", "\(parts[0])\(parts[1])\(shortNumber)"]
        }
        return terms
    }

    static func project(_ project: SpotlightProject) -> SpotlightRecord {
        SpotlightRecord(
            route: .project(project.id),
            title: project.name,
            subtitle: "Tusker project",
            description: "Open the \(project.name) project in Tusker",
            keywords: [project.id, project.name, "tusker", "project"]
        )
    }

    static func task(_ task: SpotlightTask) -> SpotlightRecord {
        SpotlightRecord(
            route: .task(projectID: task.projectID, taskID: task.id),
            title: task.title.isEmpty ? task.id : "\(task.id) — \(task.title)",
            subtitle: "\(task.id) · \(task.projectID) · \(task.status)",
            description: "\(task.epicTitle) · \(task.readiness)",
            keywords: idTerms(task.id) + [task.title, task.projectID, task.status, task.readiness, task.epicTitle, "tusker"]
        )
    }

    static func gate(_ gate: SpotlightGate, projectID: String) -> SpotlightRecord {
        let taskID = gate.blocks.first
        return SpotlightRecord(
            route: .gate(projectID: projectID, gateID: gate.id, taskID: taskID),
            title: gate.title.isEmpty ? gate.id : "\(gate.id) — \(gate.title)",
            subtitle: "\(gate.id) · \(projectID) · \(gate.status)",
            description: "Tusker gate in \(projectID)",
            keywords: idTerms(gate.id) + [gate.title, projectID, gate.status, "gate", "tusker"]
        )
    }
}

struct SpotlightChanges {
    let updated: [SpotlightRecord]
    let removed: [String]

    init(previous: [String: SpotlightRecord], next: [String: SpotlightRecord]) {
        updated = next.keys.sorted().compactMap { previous[$0] != next[$0] ? next[$0] : nil }
        removed = previous.keys.filter { next[$0] == nil }.sorted()
    }
}

@MainActor
final class SpotlightIndexer {
    typealias Fetch = (URL, Set<String>?) async throws -> [SpotlightRecord]
    typealias Write = (SpotlightChanges, Bool) async throws -> Void

    private let fetch: Fetch
    private let write: Write
    private let refreshDelayNanoseconds: UInt64
    private var refreshTask: Task<Void, Never>?
    private var baseURL: URL?
    private var pendingFullRefresh = false
    private var pendingProjects = Set<String>()
    private var recordsByID: [String: SpotlightRecord] = [:]
    private var needsStartupCleanup = true

    init(index: CSSearchableIndex = .default(), refreshDelayNanoseconds: UInt64 = 1_000_000_000,
         fetch: Fetch? = nil, write: Write? = nil) {
        self.refreshDelayNanoseconds = refreshDelayNanoseconds
        self.fetch = fetch ?? Self.fetchRecords
        self.write = write ?? { changes, clearDomain in
            try await Self.writeChanges(changes, clearDomain: clearDomain, index: index)
        }
    }

    static func affectsIndex(_ event: TuskerStreamEvent) -> Bool {
        event.keys.isEmpty || event.keys.contains {
            ["projects", "tasks", "gates"].contains($0) || $0.hasPrefix("task:") || $0.hasPrefix("gate:")
        }
    }

    func refresh(baseURL: URL, projectID: String? = nil) {
        if self.baseURL != baseURL {
            self.baseURL = baseURL
            pendingFullRefresh = true
        }
        if let projectID, !projectID.isEmpty {
            pendingProjects.insert(projectID)
        } else {
            pendingFullRefresh = true
        }
        guard refreshTask == nil else { return }
        refreshTask = Task { [weak self] in
            guard let self else { return }
            defer { self.refreshTask = nil }
            while self.pendingFullRefresh || !self.pendingProjects.isEmpty {
                // Throttle instead of cancel/restart: sustained event traffic
                // still makes progress, with at most one queued trailing pass.
                try? await Task.sleep(nanoseconds: self.refreshDelayNanoseconds)
                guard let baseURL = self.baseURL else { return }
                let projects: Set<String>? = self.pendingFullRefresh || self.needsStartupCleanup ? nil : self.pendingProjects
                self.pendingFullRefresh = false
                self.pendingProjects.removeAll()
                do {
                    let records = try await self.fetch(baseURL, projects)
                    guard self.baseURL == baseURL else { continue }
                    var next = projects.map { scope in self.recordsByID.filter { !scope.contains($0.value.route.projectID) } } ?? [:]
                    for record in records { next[record.route.identifier] = record }
                    let changes = SpotlightChanges(previous: self.recordsByID, next: next)
                    do {
                        try await self.write(changes, self.needsStartupCleanup)
                    } catch {
                        // Deletion can succeed before indexing fails. The old
                        // cache no longer describes disk; rebuild on the next
                        // request, including records identical to the old cache.
                        self.recordsByID.removeAll()
                        self.needsStartupCleanup = true
                        throw error
                    }
                    self.needsStartupCleanup = false
                    self.recordsByID = next
                } catch {
                    NSLog("Tusker Spotlight refresh failed: %@", error.localizedDescription)
                }
            }
        }
    }

    private static func writeChanges(_ changes: SpotlightChanges, clearDomain: Bool, index: CSSearchableIndex) async throws {
        if clearDomain || !changes.removed.isEmpty {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                let completion: @Sendable (Error?) -> Void = { error in
                    if let error { continuation.resume(throwing: error) } else { continuation.resume() }
                }
                if clearDomain { index.deleteSearchableItems(withDomainIdentifiers: ["tusker"], completionHandler: completion) }
                else { index.deleteSearchableItems(withIdentifiers: changes.removed, completionHandler: completion) }
            }
        }
        guard !changes.updated.isEmpty else { return }
        let thumbnailData = TuskerBranding.iconData()
        let items = changes.updated.map { makeItem($0, thumbnailData: thumbnailData) }
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            index.indexSearchableItems(items) { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            }
        }
    }

    private static func makeItem(_ record: SpotlightRecord, thumbnailData: Data?) -> CSSearchableItem {
        let attributes = CSSearchableItemAttributeSet(itemContentType: UTType.item.identifier)
        attributes.title = record.title
        attributes.displayName = record.title
        attributes.contentDescription = record.description
        attributes.contentURL = URL(string: "tusker://spotlight/\(record.route.identifier.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? record.route.identifier)")
        attributes.keywords = record.keywords.filter { !$0.isEmpty }
        attributes.thumbnailData = thumbnailData
        attributes.relatedUniqueIdentifier = record.route.identifier
        return CSSearchableItem(uniqueIdentifier: record.route.identifier, domainIdentifier: "tusker", attributeSet: attributes)
    }

    private static func fetchRecords(baseURL: URL, projectIDs: Set<String>?) async throws -> [SpotlightRecord] {
        let allProjects: [SpotlightProject] = try await get(baseURL.appendingPathComponent("api/projects"))
        let projects = allProjects.filter { projectIDs?.contains($0.id) ?? true }
        var records = projects.map(SpotlightRecordBuilder.project)
        for id in projects.map(\.id) {
            var components = URLComponents(url: baseURL.appendingPathComponent("api/tasks"), resolvingAgainstBaseURL: false)!
            components.queryItems = [URLQueryItem(name: "project", value: id)]
            let tasks: [SpotlightTask] = try await get(components.url!)
            records.append(contentsOf: tasks.map(SpotlightRecordBuilder.task))
            var gatesURL = URLComponents(url: baseURL.appendingPathComponent("api/gates"), resolvingAgainstBaseURL: false)!
            gatesURL.queryItems = [URLQueryItem(name: "project", value: id)]
            let gates: [SpotlightGate] = try await get(gatesURL.url!)
            records.append(contentsOf: gates.map { SpotlightRecordBuilder.gate($0, projectID: id) })
        }
        return records
    }

    private static func get<T: Decodable>(_ url: URL) async throws -> T {
        let (data, response) = try await URLSession.shared.data(from: url)
        guard let http = response as? HTTPURLResponse, http.statusCode == 200 else { throw URLError(.badServerResponse) }
        return try JSONDecoder().decode(T.self, from: data)
    }
}
