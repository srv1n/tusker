import Foundation

enum TuskerDeepLink: Equatable {
    case open(path: String)
    case task(id: String)
    case spotlight(identifier: String)

    static func parse(_ url: URL) -> TuskerDeepLink? {
        guard url.scheme?.lowercased() == "tusker" else { return nil }
        let host = (url.host ?? "").lowercased()
        if host == "open" {
            let path = URLComponents(url: url, resolvingAgainstBaseURL: false)?
                .queryItems?.first(where: { $0.name == "path" })?.value
            guard let path, path.hasPrefix("/") else { return nil }
            return .open(path: path)
        }
        if host == "task" {
            let id = url.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            guard id.range(of: "^[A-Za-z][A-Za-z0-9]*-T-[0-9]+$", options: .regularExpression) != nil else { return nil }
            return .task(id: id)
        }
        if host == "spotlight" {
            let identifier = url.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            guard SpotlightRoute.from(identifier: identifier) != nil else { return nil }
            return .spotlight(identifier: identifier)
        }
        return nil
    }

    static func taskPath(projectID: String, taskID: String) -> String {
        mainTaskPath(projectID: projectID, taskID: taskID)
    }

    static func mainTaskPath(projectID: String, taskID: String) -> String {
        var components = URLComponents()
        components.path = "/p/\(projectID)/docs"
        components.queryItems = [URLQueryItem(name: "path", value: taskID)]
        return components.string ?? "/"
    }

    static func projectPath(projectID: String) -> String {
        "/p/\(projectID)/"
    }
}

private struct TaskProjectReference: Decodable {
    let projectID: String

    private enum CodingKeys: String, CodingKey {
        case projectID = "projectId"
    }
}

enum TaskRouteResolver {
    static func resolve(taskID: String, baseURL: URL) async -> String? {
        let url = baseURL
            .appendingPathComponent("api/tasks")
            .appendingPathComponent(taskID)
        do {
            let (data, response) = try await URLSession.shared.data(from: url)
            guard let http = response as? HTTPURLResponse, http.statusCode == 200 else { return nil }
            let task = try JSONDecoder().decode(TaskProjectReference.self, from: data)
            guard !task.projectID.isEmpty else { return nil }
            return TuskerDeepLink.taskPath(projectID: task.projectID, taskID: taskID)
        } catch {
            return nil
        }
    }
}
