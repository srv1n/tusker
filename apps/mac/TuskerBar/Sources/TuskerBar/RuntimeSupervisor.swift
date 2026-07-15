import Foundation

enum ManagedRuntimeState: Equatable {
    case idle
    case checking
    case starting
    case running
    case external
    case failed(String)
}

enum RuntimeLaunchPlan {
    enum TerminationAction: Equatable {
        case ignore
        case finishStartup
        case restart
    }

    static let healthTimeout: TimeInterval = 1
    static let monitorHealthTimeout: TimeInterval = 3
    static let startupProbeDelay = Duration.milliseconds(250)
    static let startupWindow = Duration.seconds(15)

    static func terminationAction(for state: ManagedRuntimeState) -> TerminationAction {
        switch state {
        case .starting: return .finishStartup
        case .running: return .restart
        default: return .ignore
        }
    }

    static func manages(_ baseURL: URL) -> Bool {
        guard baseURL.scheme?.lowercased() == "http" else { return false }
        let host = baseURL.host?.lowercased()
        return (host == "127.0.0.1" || host == "localhost") && (baseURL.port ?? 80) == 7420
    }

    static func executableURL(in bundle: Bundle) -> URL? {
        bundle.url(forResource: "tusker", withExtension: nil)
    }

    static func path(home: String, inherited: String?) -> String {
        let preferred = [
            "\(home)/.local/bin",
            "/opt/homebrew/bin",
            "/usr/local/bin",
            "/usr/bin",
            "/bin",
            "/usr/sbin",
            "/sbin",
        ]
        let inheritedParts = (inherited ?? "").split(separator: ":").map(String.init)
        return (preferred + inheritedParts).reduce(into: [String]()) { result, item in
            if !item.isEmpty && !result.contains(item) { result.append(item) }
        }.joined(separator: ":")
    }
}

struct RuntimeProbeSequence {
    private(set) var generation = 0

    mutating func begin() -> Int {
        generation &+= 1
        return generation
    }

    func accepts(_ candidate: Int) -> Bool {
        candidate == generation
    }
}

enum RuntimeShellDisplay: Equatable {
    case runtimeState
    case connecting
}

struct RuntimeShellProbePlan: Equatable {
    let display: RuntimeShellDisplay
    let shouldProbe: Bool

    static func make(for state: ManagedRuntimeState) -> RuntimeShellProbePlan {
        switch state {
        case .running, .external:
            return RuntimeShellProbePlan(display: .connecting, shouldProbe: true)
        case .idle, .checking, .starting, .failed:
            // Runtime state controls the copy, never whether a visible shell
            // probes health. Notifications are hints and may be missed.
            return RuntimeShellProbePlan(display: .runtimeState, shouldProbe: true)
        }
    }
}

@MainActor
final class RuntimeSupervisor {
    static let shared = RuntimeSupervisor(config: .shared)

    private let config: AppConfig
    private var startupTask: Task<Void, Never>?
    private var monitorTask: Task<Void, Never>?
    private var process: Process?
    private var logHandle: FileHandle?
    private var lastExitCode: Int32?
    private lazy var healthSession: URLSession = {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = RuntimeLaunchPlan.monitorHealthTimeout
        configuration.timeoutIntervalForResource = RuntimeLaunchPlan.monitorHealthTimeout
        configuration.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        return URLSession(configuration: configuration)
    }()

    private(set) var state: ManagedRuntimeState = .idle {
        didSet {
            guard state != oldValue else { return }
            NotificationCenter.default.post(name: .tuskerRuntimeChanged, object: self)
        }
    }

    private init(config: AppConfig) {
        self.config = config
    }

    var title: String {
        switch state {
        case .checking, .starting: return "Starting local Tusker…"
        case .failed: return "Tusker couldn't start"
        default: return "Connecting to Tusker…"
        }
    }

    var hint: String {
        switch state {
        case .checking: return "Checking for an existing daemon…"
        case .starting: return "Launching the bundled daemon and Serve UI…"
        case let .failed(message): return message
        case .external: return config.baseURL.absoluteString
        default: return config.baseURL.absoluteString
        }
    }

    func ensureRunning(force: Bool = false) {
        guard RuntimeLaunchPlan.manages(config.baseURL) else {
            startupTask?.cancel()
            monitorTask?.cancel()
            startupTask = nil
            monitorTask = nil
            state = .external
            return
        }
        if !force, state == .running || state == .checking || state == .starting { return }
        startupTask?.cancel()
        if force {
            monitorTask?.cancel()
            monitorTask = nil
        }
        state = .checking
        startupTask = Task { [weak self] in
            guard let self else { return }
            if await self.isHealthy() {
                self.state = .running
                self.startupTask = nil
                self.startMonitoring()
                return
            }
            self.state = .starting
            do {
                try self.launchBundledDaemon()
            } catch {
                self.state = .failed(error.localizedDescription)
                self.startupTask = nil
                return
            }
            let clock = ContinuousClock()
            let deadline = clock.now.advanced(by: RuntimeLaunchPlan.startupWindow)
            while clock.now < deadline {
                guard !Task.isCancelled else { return }
                if await self.isHealthy() {
                    self.state = .running
                    self.startupTask = nil
                    self.startMonitoring()
                    return
                }
                guard clock.now < deadline else { break }
                try? await Task.sleep(for: RuntimeLaunchPlan.startupProbeDelay)
            }
            self.state = .failed(self.startupFailureMessage())
            self.startupTask = nil
        }
    }

    private func isHealthy(timeout: TimeInterval = RuntimeLaunchPlan.healthTimeout) async -> Bool {
        let url = config.baseURL.appendingPathComponent("api/summary")
        var request = URLRequest(url: url)
        request.timeoutInterval = timeout
        do {
            let (_, response) = try await healthSession.data(for: request)
            return (response as? HTTPURLResponse)?.statusCode == 200
        } catch {
            return false
        }
    }

    private func startMonitoring() {
        monitorTask?.cancel()
        monitorTask = Task { [weak self] in
            var consecutiveFailures = 0
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(5))
                guard !Task.isCancelled, let self else { return }
                if await self.isHealthy(timeout: RuntimeLaunchPlan.monitorHealthTimeout) {
                    consecutiveFailures = 0
                } else {
                    consecutiveFailures += 1
                }
                if consecutiveFailures >= 3 {
                    self.monitorTask = nil
                    self.ensureRunning(force: true)
                    return
                }
            }
        }
    }

    private func launchBundledDaemon() throws {
        guard let executable = RuntimeLaunchPlan.executableURL(in: .main) else {
            throw RuntimeStartupError("The app bundle is missing its Tusker runtime. Reinstall with `make install`.")
        }
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw RuntimeStartupError("The bundled Tusker runtime isn't executable. Reinstall the app.")
        }

        let process = Process()
        process.executableURL = executable
        process.arguments = ["daemon", "run"]
        var environment = ProcessInfo.processInfo.environment
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        environment["HOME"] = home
        environment["PATH"] = RuntimeLaunchPlan.path(home: home, inherited: environment["PATH"])
        process.environment = environment

        let handle = try daemonLogHandle(home: home)
        process.standardOutput = handle
        process.standardError = handle
        process.terminationHandler = { [weak self] process in
            Task { @MainActor in
                guard let self, self.process === process else { return }
                self.lastExitCode = process.terminationStatus
                self.process = nil
                try? self.logHandle?.close()
                self.logHandle = nil
                switch RuntimeLaunchPlan.terminationAction(for: self.state) {
                case .restart:
                    self.ensureRunning(force: true)
                case .finishStartup:
                    self.startupTask?.cancel()
                    self.startupTask = Task { [weak self] in
                        guard let self else { return }
                        // A competing daemon may have won the startup race. Give
                        // it one final probe before surfacing the child exit.
                        if await self.isHealthy() {
                            self.state = .running
                            self.startupTask = nil
                            self.startMonitoring()
                        } else {
                            self.state = .failed(self.startupFailureMessage())
                            self.startupTask = nil
                        }
                    }
                case .ignore:
                    break
                }
            }
        }
        self.process = process
        logHandle = handle
        do {
            try process.run()
        } catch {
            if self.process === process { self.process = nil }
            if self.logHandle === handle { self.logHandle = nil }
            try? handle.close()
            throw error
        }
    }

    private func daemonLogHandle(home: String) throws -> FileHandle {
        let directory = URL(fileURLWithPath: home)
            .appendingPathComponent("Library/Application Support/tusker/logs", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let url = directory.appendingPathComponent("app-daemon.log")
        if let attributes = try? FileManager.default.attributesOfItem(atPath: url.path),
           let size = attributes[.size] as? NSNumber, size.intValue > 1_000_000 {
            try? FileManager.default.removeItem(at: url)
        }
        if !FileManager.default.fileExists(atPath: url.path) {
            FileManager.default.createFile(atPath: url.path, contents: nil)
        }
        let handle = try FileHandle(forWritingTo: url)
        try handle.seekToEnd()
        return handle
    }

    private func startupFailureMessage() -> String {
        let suffix = lastExitCode.map { " (exit \($0))" } ?? ""
        return "The bundled daemon didn't become ready\(suffix). See ~/Library/Application Support/tusker/logs/app-daemon.log."
    }
}

private struct RuntimeStartupError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

extension Notification.Name {
    static let tuskerRuntimeChanged = Notification.Name("TuskerRuntimeChanged")
}
