@preconcurrency import CoreSpotlight
import Foundation
import UniformTypeIdentifiers

enum SpotlightRoute: Equatable {
    case project(String)
    case task(projectID: String, taskID: String)
    case gate(projectID: String, gateID: String, taskID: String?)

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
            guard let taskID, !taskID.isEmpty else { return "/p/\(projectID)/needs" }
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

final class SpotlightIndexer {
    private let index: CSSearchableIndex
    private var refreshTask: Task<Void, Never>?

    init(index: CSSearchableIndex = .default()) { self.index = index }

    func refresh(baseURL: URL) {
        refreshTask?.cancel()
        refreshTask = Task { [weak self] in
            guard let self else { return }
            do {
                try? await Task.sleep(nanoseconds: 250_000_000)
                guard !Task.isCancelled else { return }
                let records = try await Self.fetchRecords(baseURL: baseURL)
                guard !Task.isCancelled else { return }
                self.replace(records: records)
            } catch {
                NSLog("Tusker Spotlight refresh failed: %@", error.localizedDescription)
            }
        }
    }

    private func replace(records: [SpotlightRecord]) {
        let items = records.map(makeItem)
        index.deleteSearchableItems(withDomainIdentifiers: ["tusker"]) { [weak self] _ in
            guard let self else { return }
            self.index.indexSearchableItems(items) { _ in }
            }
    }

    private func makeItem(_ record: SpotlightRecord) -> CSSearchableItem {
        let attributes = CSSearchableItemAttributeSet(itemContentType: UTType.item.identifier)
        attributes.title = record.title
        attributes.displayName = record.title
        attributes.contentDescription = record.description
        attributes.contentURL = URL(string: "tusker://spotlight/\(record.route.identifier.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? record.route.identifier)")
        attributes.keywords = record.keywords.filter { !$0.isEmpty }
        attributes.thumbnailData = TuskerBranding.iconData()
        attributes.relatedUniqueIdentifier = record.route.identifier
        return CSSearchableItem(uniqueIdentifier: record.route.identifier, domainIdentifier: "tusker", attributeSet: attributes)
    }

    private static func fetchRecords(baseURL: URL) async throws -> [SpotlightRecord] {
        let projects: [SpotlightProject] = try await get(baseURL.appendingPathComponent("api/projects"))
        var records = projects.map(SpotlightRecordBuilder.project)
        for project in projects {
            var components = URLComponents(url: baseURL.appendingPathComponent("api/tasks"), resolvingAgainstBaseURL: false)!
            components.queryItems = [URLQueryItem(name: "project", value: project.id)]
            let tasks: [SpotlightTask] = try await get(components.url!)
            records.append(contentsOf: tasks.map(SpotlightRecordBuilder.task))
            var gatesURL = URLComponents(url: baseURL.appendingPathComponent("api/gates"), resolvingAgainstBaseURL: false)!
            gatesURL.queryItems = [URLQueryItem(name: "project", value: project.id)]
            let gates: [SpotlightGate] = try await get(gatesURL.url!)
            records.append(contentsOf: gates.map { SpotlightRecordBuilder.gate($0, projectID: project.id) })
        }
        return records
    }

    private static func get<T: Decodable>(_ url: URL) async throws -> T {
        let (data, response) = try await URLSession.shared.data(from: url)
        guard let http = response as? HTTPURLResponse, http.statusCode == 200 else { throw URLError(.badServerResponse) }
        return try JSONDecoder().decode(T.self, from: data)
    }
}
