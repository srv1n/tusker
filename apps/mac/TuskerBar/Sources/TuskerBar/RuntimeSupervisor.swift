import Darwin
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

    static func shouldStart(from state: ManagedRuntimeState, force: Bool) -> Bool {
        if force { return true }
        switch state {
        case .running, .checking, .starting, .failed:
            return false
        case .idle, .external:
            return true
        }
    }

    /// A healthy daemon is adopted when it runs this bundle's runtime and was
    /// started after that runtime was installed; otherwise it is replaced. The
    /// daemon outlives the app (M8), so a relaunched app must adopt, not restart.
    static func shouldReuseHealthyRuntime(daemonExecutable: String?, daemonStarted: Date?,
                                          bundledExecutable: String, bundledModified: Date?) -> Bool {
        guard let daemonExecutable, let daemonStarted, let bundledModified else { return false }
        return daemonExecutable == bundledExecutable && daemonStarted >= bundledModified
    }

    static func manages(_ baseURL: URL) -> Bool {
        guard baseURL.scheme?.lowercased() == "http" else { return false }
        let host = baseURL.host?.lowercased()
        guard ["127.0.0.1", "localhost", "::1"].contains(host ?? ""),
              let port = baseURL.port else { return false }
        return (1 ... 65_535).contains(port)
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

    static func daemonEnvironment(inheriting environment: [String: String]) -> [String: String] {
        var environment = environment
        // TuskerBar is the independent resident-daemon supervisor. Developer
        // launches via `open` can inherit the invoking Codex/Claude shell's
        // identity, which would make the bundled runtime reject a legitimate
        // app-owned launch as a nested agent launch.
        for key in ["TUSKER_ATTEMPT_ID", "CODEX_SHELL", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"] {
            environment.removeValue(forKey: key)
        }
        environment["TUSKER_SERVE_REQUIRED"] = "1"
        return environment
    }
}

enum RuntimeShellLoadKind {
    case optimistic
    case live
}

enum RuntimeShellLoadPlan {
    static func cachePolicy(for kind: RuntimeShellLoadKind) -> URLRequest.CachePolicy {
        switch kind {
        case .optimistic:
            // The HTML names content-hashed bundles. Reusing an older document
            // after an app update can point WebKit at bundles the new daemon no
            // longer serves and leave the window blank.
            return .reloadIgnoringLocalCacheData
        case .live:
            return .reloadIgnoringLocalCacheData
        }
    }

    static func shouldCoverWebView(hasCommittedContent: Bool) -> Bool {
        !hasCommittedContent
    }
}

@MainActor
final class RuntimeSupervisor {
    static let shared = RuntimeSupervisor(config: .shared)

    private let config: AppConfig
    private var startupTask: Task<Void, Never>?
    private var monitorTask: Task<Void, Never>?
    /// The daemon this app instance spawned, while it runs. A daemon left by an
    /// earlier app instance is adopted by health and receipt, not by PID here.
    private var childPID: pid_t?
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
        // A failed launch is terminal until the user explicitly retries. Visible
        // shells continue probing health, but must not turn their probe failures
        // and runtime-state notifications into an unthrottled relaunch loop.
        guard RuntimeLaunchPlan.shouldStart(from: state, force: force) else { return }
        startupTask?.cancel()
        if force {
            monitorTask?.cancel()
            monitorTask = nil
        }
        state = .checking
        startupTask = Task { [weak self] in
            guard let self else { return }
            if await self.isHealthy(), self.childPID != nil || self.runningDaemonIsBundled() {
                self.state = .running
                self.startupTask = nil
                self.startMonitoring()
                return
            }
            if await self.isHealthy() {
                do {
                    try await self.stopExistingDaemon()
                } catch {
                    self.state = .failed(error.localizedDescription)
                    self.startupTask = nil
                    return
                }
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

        var environment = RuntimeLaunchPlan.daemonEnvironment(inheriting: ProcessInfo.processInfo.environment)
        if environment["TUSKER_SERVE_OPERATOR"]?.isEmpty != false {
            environment["TUSKER_SERVE_OPERATOR"] = "human:\(NSUserName())"
        }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        environment["HOME"] = home
        environment["PATH"] = RuntimeLaunchPlan.path(home: home, inherited: environment["PATH"])

        // Rotate and redact the previous log, then hand the daemon the log file
        // itself. A pipe would tie the daemon to the app: once the app quit,
        // the daemon's next log line died of SIGPIPE.
        // ponytail: lines written while the app is not running are redacted and
        // size-bounded only at the next launch; add a daemon-side log sink if
        // that matters.
        try RuntimeLogWriter(home: home).finish(Data())
        let logPath = RuntimeLogWriter.currentURL(home: home).path
        let logFD = open(logPath, O_WRONLY | O_APPEND | O_NOFOLLOW)
        guard logFD >= 0 else { throw RuntimeStartupError("Couldn't open \(logPath) for the daemon log.") }
        defer { Darwin.close(logFD) }
        let pid = try Self.spawnDetached(executable.path, ["daemon", "run"], environment: environment, logFD: logFD)
        childPID = pid
        Thread.detachNewThread { [weak self] in
            var status: Int32 = 0
            while waitpid(pid, &status, 0) == -1 && errno == EINTR {}
            let code = (status & 0x7f) == 0 ? (status >> 8) & 0xff : 128 + (status & 0x7f)
            Task { @MainActor in self?.childExited(pid: pid, code: code) }
        }
    }

    private func childExited(pid: pid_t, code: Int32) {
        guard childPID == pid else { return }
        lastExitCode = code
        childPID = nil
        switch RuntimeLaunchPlan.terminationAction(for: state) {
        case .restart:
            ensureRunning(force: true)
        case .finishStartup:
            startupTask?.cancel()
            startupTask = Task { [weak self] in
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

    /// Spawns the daemon in its own session with only stdin (/dev/null),
    /// stdout and stderr (the log) open, so quitting or force-killing the app
    /// never reaches it.
    private static func spawnDetached(_ path: String, _ arguments: [String], environment: [String: String], logFD: Int32) throws -> pid_t {
        var attr: posix_spawnattr_t?
        posix_spawnattr_init(&attr)
        defer { posix_spawnattr_destroy(&attr) }
        posix_spawnattr_setflags(&attr, Int16(POSIX_SPAWN_SETSID | POSIX_SPAWN_CLOEXEC_DEFAULT | POSIX_SPAWN_SETSIGMASK | POSIX_SPAWN_SETSIGDEF))
        var noMask: sigset_t = 0
        posix_spawnattr_setsigmask(&attr, &noMask)
        var defaults: sigset_t = [SIGPIPE, SIGTERM, SIGINT, SIGHUP].reduce(0) { $0 | (1 << ($1 - 1)) }
        posix_spawnattr_setsigdefault(&attr, &defaults)
        var actions: posix_spawn_file_actions_t?
        posix_spawn_file_actions_init(&actions)
        defer { posix_spawn_file_actions_destroy(&actions) }
        posix_spawn_file_actions_addopen(&actions, 0, "/dev/null", O_RDONLY, 0)
        posix_spawn_file_actions_adddup2(&actions, logFD, 1)
        posix_spawn_file_actions_adddup2(&actions, logFD, 2)
        let argv: [UnsafeMutablePointer<CChar>?] = ([path] + arguments).map { strdup($0) } + [nil]
        let envp: [UnsafeMutablePointer<CChar>?] = environment.map { strdup("\($0.key)=\($0.value)") } + [nil]
        defer { (argv + envp).forEach { free($0) } }
        var pid: pid_t = 0
        let result = posix_spawn(&pid, path, &actions, &attr, argv, envp)
        guard result == 0 else {
            throw RuntimeStartupError("Couldn't launch the bundled daemon: \(String(cString: strerror(result)))")
        }
        return pid
    }

    /// Whether the daemon named by the receipt runs this bundle's runtime and
    /// started after it was installed (so an app update still replaces it).
    private func runningDaemonIsBundled() -> Bool {
        guard let receipt = receiptLiveness(), let executable = RuntimeLaunchPlan.executableURL(in: .main) else { return false }
        var buffer = [CChar](repeating: 0, count: 4 * Int(MAXPATHLEN))
        let length = proc_pidpath(Int32(receipt.pid), &buffer, UInt32(buffer.count))
        let daemonPath = length > 0 ? URL(fileURLWithPath: String(cString: buffer)).resolvingSymlinksInPath().path : nil
        let bundled = executable.resolvingSymlinksInPath().path
        let modified = (try? FileManager.default.attributesOfItem(atPath: bundled))?[.modificationDate] as? Date
        return RuntimeLaunchPlan.shouldReuseHealthyRuntime(
            daemonExecutable: daemonPath, daemonStarted: receipt.startedDate,
            bundledExecutable: bundled, bundledModified: modified)
    }

    /// Stops the daemon and every process Tusker started (`tusker stop --all`).
    /// The supervisor goes idle first so it does not relaunch the daemon.
    func stopEverything() async throws -> String {
        startupTask?.cancel()
        monitorTask?.cancel()
        startupTask = nil
        monitorTask = nil
        state = .idle
        let (status, output) = try await runBundled(["stop", "--all"])
        guard status == 0 else {
            throw RuntimeStartupError(output.isEmpty ? "tusker stop --all failed (exit \(status))." : output)
        }
        return output
    }

    private func stopExistingDaemon() async throws {
        let (status, _) = try await runBundled(["daemon", "stop", "--json"])
        guard status == 0 else {
            throw RuntimeStartupError("TuskerBar could not replace the existing local daemon.")
        }
    }

    private func runBundled(_ arguments: [String]) async throws -> (Int32, String) {
        guard let executable = RuntimeLaunchPlan.executableURL(in: .main) else {
            throw RuntimeStartupError("The app bundle is missing its Tusker runtime. Reinstall with `make install`.")
        }
        var environment = RuntimeLaunchPlan.daemonEnvironment(inheriting: ProcessInfo.processInfo.environment)
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        environment["HOME"] = home
        environment["PATH"] = RuntimeLaunchPlan.path(home: home, inherited: environment["PATH"])
        let launchEnvironment = environment
        return try await Task.detached {
            let process = Process()
            process.executableURL = executable
            process.arguments = arguments
            process.environment = launchEnvironment
            let pipe = Pipe()
            process.standardOutput = pipe
            process.standardError = pipe
            try process.run()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            let output = String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
            return (process.terminationStatus, output)
        }.value
    }

    private func startupFailureMessage() -> String {
        if childPID != nil, let liveness = receiptLiveness(), liveness.serveEnabled {
            return "The bundled runtime bound http://\(liveness.serveAddr), but TuskerBar is configured for \(config.baseURL.absoluteString). Match runtime.serve.addr to the configured local URL."
        }
        let suffix = lastExitCode.map { " (exit \($0))" } ?? ""
        return "The bundled daemon didn't become ready\(suffix). See ~/Library/Application Support/tusker/logs/app-daemon.log."
    }

    private func receiptLiveness() -> RuntimeReceiptLiveness? {
        let environment = ProcessInfo.processInfo.environment
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let stateRoot = environment["TUSKER_STATE_ROOT"].flatMap { $0.hasPrefix("/") ? $0 : nil } ?? "\(home)/Library/Application Support/tusker"
        let pidPath = URL(fileURLWithPath: stateRoot).appendingPathComponent("daemon.pid")
        guard let data = try? Data(contentsOf: pidPath) else { return nil }
        return try? JSONDecoder().decode(RuntimeReceiptLiveness.self, from: data)
    }
}

private struct RuntimeReceiptLiveness: Decodable {
    let pid: Int
    let startedAt: String?
    let serveEnabled: Bool
    let serveAddr: String

    enum CodingKeys: String, CodingKey {
        case pid
        case startedAt = "started_at"
        case serveEnabled = "serve_enabled"
        case serveAddr = "serve_addr"
    }

    var startedDate: Date? {
        guard let startedAt else { return nil }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fractional.date(from: startedAt) ?? ISO8601DateFormatter().date(from: startedAt)
    }
}

/// A synchronous serial sink gives FileHandle's readability callback natural
/// backpressure: at most one 64 KiB read plus one bounded line is resident.
/// All mutable state is confined to `queue`.
final class RuntimeLogWriter: @unchecked Sendable {
    private static let maxBytes: UInt64 = 512 * 1024
    private static let archiveCount = 3
    private static let maxLineBytes = 64 * 1024
    private static let maxAge: TimeInterval = 24 * 60 * 60

    private let queue = DispatchQueue(label: "com.tusker.runtime-log-writer")
    private let fm = FileManager.default
    private let home: URL
    private let directory: URL
    private let current: URL
    private var handle: FileHandle
    private var pending = Data()
    private var discardingLongLine = false
    private var closed = false

    static func currentURL(home: String) -> URL {
        URL(fileURLWithPath: home).standardizedFileURL
            .appendingPathComponent("Library/Application Support/tusker/logs", isDirectory: true)
            .appendingPathComponent("app-daemon.log")
    }

    init(home: String) throws {
        let homeURL = URL(fileURLWithPath: home).standardizedFileURL
        self.home = homeURL
        current = Self.currentURL(home: home)
        directory = current.deletingLastPathComponent()
        try fm.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        try Self.validateDirectoryChain(home: homeURL)
        try Self.validateDirectory(directory)
        try Self.validateLogFamily(current)
        if try Self.shouldRotate(current) { try Self.rotate(current) }
        try Self.sanitizeAndBoundFamily(current)
        handle = try Self.openCurrent(current)
    }

    func append(_ data: Data) {
        queue.sync {
            guard !closed else { return }
            do { try consume(data, finishing: false) }
            catch { failClosed(error) }
        }
    }

    func finish(_ tail: Data) {
        queue.sync {
            guard !closed else { return }
            do {
                try consume(tail, finishing: true)
                try handle.synchronize()
                try handle.close()
                closed = true
            } catch { failClosed(error) }
        }
    }

    private func consume(_ incoming: Data, finishing: Bool) throws {
        var data = incoming
        if discardingLongLine {
            if let newline = data.firstRange(of: Data([0x0A])) {
                data.removeSubrange(..<newline.upperBound)
                discardingLongLine = false
            } else {
                return
            }
        }
        pending.append(data)
        while let newline = pending.firstRange(of: Data([0x0A])) {
            let line = pending.subdata(in: pending.startIndex..<newline.upperBound)
            pending.removeSubrange(pending.startIndex..<newline.upperBound)
            try writeSanitized(line)
        }
        if pending.count > Self.maxLineBytes {
            pending.removeAll(keepingCapacity: false)
            discardingLongLine = true
            try writeSanitized(Data("[Tusker omitted an overlong daemon log line]\n".utf8))
        } else if finishing, !pending.isEmpty {
            try writeSanitized(pending)
            pending.removeAll(keepingCapacity: false)
        }
    }

    private func writeSanitized(_ data: Data) throws {
        let bytes = Self.redact(data)
        guard !bytes.isEmpty else { return }
        try Self.validateDirectoryChain(home: home)
        try Self.validateDirectory(directory)
        try Self.validateCurrentPath(current, handle: handle)
        let attributes = try fm.attributesOfItem(atPath: current.path)
        guard let size = attributes[.size] as? NSNumber else { throw RuntimeLogError("cannot read current log size") }
        let aged = (attributes[.modificationDate] as? Date).map { Date().timeIntervalSince($0) >= Self.maxAge } ?? false
        if size.uint64Value + UInt64(bytes.count) > Self.maxBytes || aged {
            try handle.synchronize()
            try handle.close()
            try Self.validateLogFamily(current)
            try Self.rotate(current)
            try Self.sanitizeAndBoundFamily(current)
            handle = try Self.openCurrent(current)
        }
        try handle.write(contentsOf: bytes)
    }

    private func failClosed(_ error: Error) {
        closed = true
        pending.removeAll(keepingCapacity: false)
        // The daemon may continue, but this sink never writes through a path
        // whose ownership or link authority became ambiguous.
        NSLog("Tusker daemon logging stopped safely: %@", error.localizedDescription)
        do {
            try Self.validateDirectoryChain(home: home)
            try Self.validateDirectory(directory)
            let errorURL = directory.appendingPathComponent("app-daemon.log-error")
            let errorHandle = try Self.openCurrent(errorURL)
            defer { errorHandle.closeFile() }
            try Self.validateCurrentPath(errorURL, handle: errorHandle)
            let reason = error.localizedDescription.replacingOccurrences(of: "\n", with: " ").replacingOccurrences(of: "\r", with: " ")
            try errorHandle.write(contentsOf: Self.redact(Data("[Tusker daemon logging stopped safely: \(reason)]\n".utf8)))
        } catch {
            NSLog("Tusker daemon log error file unavailable: %@", error.localizedDescription)
        }
        do { try handle.close() } catch {
            NSLog("Tusker daemon log close failed: %@", error.localizedDescription)
        }
    }

    static func redact(_ data: Data) -> Data {
        let raw = String(decoding: data, as: UTF8.self)
        let bearer = raw.replacingOccurrences(
            of: "(?i)bearer\\s+(?:\"[^\"]*\"|'[^']*'|[^\\s,;]+)",
            with: "Bearer [REDACTED]", options: .regularExpression
        )
        let secrets = bearer.replacingOccurrences(
            of: "(?i)(authorization|access[_-]?token|api[_-]?key|token|secret|password|credential|cookie|capability)(?:[\"']?\\s*[:=]\\s*|[\"']?\\s+)(?:\"[^\"]*\"|'[^']*'|[^\\s,;]+)",
            with: "$1=[REDACTED]", options: .regularExpression
        )
        return Data(Data(secrets.utf8).prefix(Int(maxBytes)))
    }

    private static func validateDirectory(_ url: URL) throws {
        let status = try fileStatus(url)
        guard (status.st_mode & mode_t(S_IFMT)) == mode_t(S_IFDIR) else { throw RuntimeLogError("log directory is not a directory") }
        guard status.st_uid == geteuid() else { throw RuntimeLogError("log directory is owned by another user") }
        guard status.st_nlink >= 1 else { throw RuntimeLogError("log directory link authority is invalid") }
        guard status.st_mode & 0o077 == 0 else { throw RuntimeLogError("log directory permissions must be owner-only") }
    }

    private static func validateDirectoryChain(home: URL) throws {
        var cursor = home
        for component in ["Library", "Application Support", "tusker", "logs"] {
            cursor.appendPathComponent(component, isDirectory: true)
            let status = try fileStatus(cursor)
            guard (status.st_mode & mode_t(S_IFMT)) == mode_t(S_IFDIR) else {
                throw RuntimeLogError("refusing symlink or non-directory log path component at \(cursor.path)")
            }
            guard status.st_uid == geteuid() else {
                throw RuntimeLogError("refusing foreign-owned log path component at \(cursor.path)")
            }
        }
    }

    private static func validateLogFamily(_ current: URL) throws {
        let allowed = Set(logURLs(current).map(\.lastPathComponent))
        for name in try FileManager.default.contentsOfDirectory(atPath: current.deletingLastPathComponent().path)
        where name.hasPrefix(current.lastPathComponent + ".") && !allowed.contains(name) {
            throw RuntimeLogError("refusing unexpected daemon log archive \(name)")
        }
        for url in logURLs(current) {
            guard let status = try fileStatusIfExists(url) else { continue }
            guard (status.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG) else { throw RuntimeLogError("refusing non-regular log file at \(url.path)") }
            guard status.st_uid == geteuid() else { throw RuntimeLogError("refusing foreign-owned log file at \(url.path)") }
            guard status.st_nlink == 1 else { throw RuntimeLogError("refusing hard-linked log file at \(url.path)") }
            guard status.st_mode & 0o077 == 0 else { throw RuntimeLogError("refusing insecure log permissions at \(url.path)") }
        }
    }

    private static func validateOpenHandle(_ handle: FileHandle) throws {
        var status = stat()
        guard fstat(handle.fileDescriptor, &status) == 0 else { throw RuntimeLogError("cannot validate open log handle") }
        guard (status.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG), status.st_uid == geteuid(), status.st_nlink == 1, status.st_mode & 0o077 == 0 else {
            throw RuntimeLogError("open log handle lost owner-only authority")
        }
    }

    private static func validateCurrentPath(_ url: URL, handle: FileHandle) throws {
        let pathStatus = try fileStatus(url)
        var openStatus = stat()
        guard fstat(handle.fileDescriptor, &openStatus) == 0 else { throw RuntimeLogError("cannot validate open log handle") }
        try validateOpenHandle(handle)
        guard pathStatus.st_dev == openStatus.st_dev, pathStatus.st_ino == openStatus.st_ino else {
            throw RuntimeLogError("current log path no longer names the open owner file")
        }
    }

    private static func shouldRotate(_ current: URL) throws -> Bool {
        guard let status = try fileStatusIfExists(current) else { return false }
        return UInt64(status.st_size) >= maxBytes || Date().timeIntervalSince1970 - TimeInterval(status.st_mtimespec.tv_sec) >= maxAge
    }

    private static func rotate(_ current: URL) throws {
        let urls = logURLs(current)
        if try fileStatusIfExists(urls[archiveCount]) != nil { try FileManager.default.removeItem(at: urls[archiveCount]) }
        for index in stride(from: archiveCount - 1, through: 1, by: -1) {
            if try fileStatusIfExists(urls[index]) != nil {
                try FileManager.default.moveItem(at: urls[index], to: urls[index + 1])
            }
        }
        if try fileStatusIfExists(current) != nil { try FileManager.default.moveItem(at: current, to: urls[1]) }
    }

    private static func sanitizeAndBoundFamily(_ current: URL) throws {
        for url in logURLs(current) {
            guard try fileStatusIfExists(url) != nil else { continue }
            let input = try boundedTail(url)
            let output = redact(input)
            let fd = open(url.path, O_WRONLY | O_TRUNC | O_NOFOLLOW)
            guard fd >= 0 else { throw RuntimeLogError("cannot securely rewrite \(url.path)") }
            defer { Darwin.close(fd) }
            try writeAll(fd: fd, data: output)
            guard fchmod(fd, S_IRUSR | S_IWUSR) == 0 else { throw RuntimeLogError("cannot secure permissions for \(url.path)") }
        }
    }

    private static func boundedTail(_ url: URL) throws -> Data {
        let handle = try FileHandle(forReadingFrom: url)
        defer { handle.closeFile() }
        let size = try handle.seekToEnd()
        if size > maxBytes { try handle.seek(toOffset: size - maxBytes) }
        else { try handle.seek(toOffset: 0) }
        return try handle.read(upToCount: Int(maxBytes)) ?? Data()
    }

    private static func openCurrent(_ url: URL) throws -> FileHandle {
        let fd = open(url.path, O_WRONLY | O_CREAT | O_APPEND | O_NOFOLLOW, S_IRUSR | S_IWUSR)
        guard fd >= 0 else { throw RuntimeLogError("cannot securely open \(url.path)") }
        let handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
        do { try validateOpenHandle(handle) }
        catch { handle.closeFile(); throw error }
        return handle
    }

    private static func writeAll(fd: Int32, data: Data) throws {
        try data.withUnsafeBytes { buffer in
            var offset = 0
            while offset < buffer.count {
                let written = Darwin.write(fd, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
                guard written > 0 else { throw RuntimeLogError("short write while securing daemon log") }
                offset += written
            }
        }
    }

    private static func fileStatus(_ url: URL) throws -> stat {
        var status = stat()
        let result = url.path.withCString { lstat($0, &status) }
        guard result == 0 else { throw RuntimeLogError("cannot inspect \(url.path)") }
        return status
    }

    private static func fileStatusIfExists(_ url: URL) throws -> stat? {
        var status = stat()
        let result = url.path.withCString { lstat($0, &status) }
        if result == 0 { return status }
        if errno == ENOENT { return nil }
        throw RuntimeLogError("cannot inspect \(url.path)")
    }

    private static func logURLs(_ current: URL) -> [URL] {
        [current] + (1...archiveCount).map { current.appendingPathExtension("\($0)") }
    }
}

private struct RuntimeLogError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

private struct RuntimeStartupError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

extension Notification.Name {
    static let tuskerRuntimeChanged = Notification.Name("TuskerRuntimeChanged")
}
