import AppKit
import WebKit

@MainActor
final class PanelController: NSObject, WKNavigationDelegate, WKScriptMessageHandler, NSWindowDelegate {
    private let config: AppConfig
    private let onBridgeNotify: (String, String, String?) -> Void
    private let panel: NSPanel
    private let webView: WKWebView
    private let unavailableView = NSView()
    private let stateTitle = NSTextField(labelWithString: "Connecting to Tusker…")
    private let stateHint = NSTextField(labelWithString: "Checking http://127.0.0.1:7420")
    private var retryWorkItem: DispatchWorkItem?
    private var retryDelay: TimeInterval = 1
    private var loaded = false
    private var globalMonitor: Any?
    private var localMonitor: Any?

    init(config: AppConfig, onBridgeNotify: @escaping (String, String, String?) -> Void) {
        self.config = config
        self.onBridgeNotify = onBridgeNotify
        let frame = NSRect(
            x: 0, y: 0,
            width: UserDefaults.standard.double(forKey: "panelWidth").nonZero(or: 420).clamped(to: 360...600),
            height: UserDefaults.standard.double(forKey: "panelHeight").nonZero(or: 640).clamped(to: 480...800)
        )
        panel = NSPanel(contentRect: frame, styleMask: [.titled, .closable, .resizable, .nonactivatingPanel], backing: .buffered, defer: false)
        let content = WKUserContentController()
        content.addUserScript(WKUserScript(source: PanelController.bridgeScript(appVersion: config.appVersion, origin: PanelController.configuredOrigin(config.baseURL) ?? ""), injectionTime: .atDocumentStart, forMainFrameOnly: true))
        let webConfig = WKWebViewConfiguration()
        webConfig.userContentController = content
        webView = WKWebView(frame: frame, configuration: webConfig)
        super.init()
        content.add(self, name: "tuskerShell")
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.hidesOnDeactivate = false
        panel.isReleasedWhenClosed = false
        panel.delegate = self
        let contentView = NSView()
        panel.contentView = contentView
        webView.translatesAutoresizingMaskIntoConstraints = false
        webView.navigationDelegate = self
        webView.customUserAgent = "TuskerShell/\(config.appVersion)"
        if #available(macOS 13.3, *) { webView.isInspectable = true }
        contentView.addSubview(webView)
        NSLayoutConstraint.activate([
            webView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            webView.topAnchor.constraint(equalTo: contentView.topAnchor),
            webView.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
        ])
        configureUnavailableView()
        globalMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] event in
            guard let self, self.panel.isVisible, event.window !== self.panel else { return }
            self.hide()
        }
        localMonitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown]) { [weak self] event in
            if event.keyCode == 53, self?.panel.isVisible == true { self?.hide(); return nil }
            return event
        }
        NotificationCenter.default.addObserver(self, selector: #selector(configurationChanged), name: .tuskerConfigChanged, object: config)
        NotificationCenter.default.addObserver(self, selector: #selector(runtimeChanged), name: .tuskerRuntimeChanged, object: RuntimeSupervisor.shared)
    }

    deinit {
        if let globalMonitor { NSEvent.removeMonitor(globalMonitor) }
        if let localMonitor { NSEvent.removeMonitor(localMonitor) }
    }

    func toggle(anchor: NSStatusBarButton? = nil) { panel.isVisible ? hide() : show(anchor: anchor) }

    func show(path: String = "/panel?shell=1", anchor: NSStatusBarButton? = nil) {
        if let anchor {
            let origin = anchor.convert(anchor.bounds, to: nil).origin
            let point = anchor.window?.convertPoint(toScreen: origin) ?? .zero
            panel.setFrameTopLeftPoint(NSPoint(x: point.x, y: point.y))
        }
        panel.orderFrontRegardless()
        if !loaded { probeAndLoad(path: path) } else if path != "/panel?shell=1" { navigateInPageOrLoad(path) }
    }

    func hide() { retryWorkItem?.cancel(); panel.orderOut(nil) }

    func windowDidResize(_ notification: Notification) {
        let size = panel.frame.size
        UserDefaults.standard.set(size.width, forKey: "panelWidth")
        UserDefaults.standard.set(size.height, forKey: "panelHeight")
    }

    private func load(path: String) {
        guard let url = url(for: path) else { showUnavailable(); return }
        loaded = true
        webView.load(URLRequest(url: url))
    }

    private func probeAndLoad(path: String) {
        let runtime = RuntimeSupervisor.shared
        if runtime.state == .idle { runtime.ensureRunning() }
        switch runtime.state {
        case .checking, .starting, .failed, .idle:
            showRuntimeState()
            return
        case .running, .external:
            showConnecting()
        }
        let healthURL = config.baseURL.appendingPathComponent("api/summary")
        URLSession.shared.dataTask(with: healthURL) { [weak self] _, response, error in
            DispatchQueue.main.async {
                guard let self else { return }
                if error == nil, (response as? HTTPURLResponse)?.statusCode == 200 {
                    self.load(path: path)
                } else {
                    self.showUnavailable()
                }
            }
        }.resume()
    }

    private func navigateInPageOrLoad(_ path: String) {
        let escaped = path.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
        webView.evaluateJavaScript("(function(){ const handler = window.tuskerShell?.onNavigate; return handler ? handler(\"\(escaped)\") : false; })()") { [weak self] value, error in
            if error != nil || (value as? Bool) != true { self?.load(path: path) }
        }
    }

    private func url(for path: String) -> URL? {
        guard var components = URLComponents(url: config.baseURL, resolvingAgainstBaseURL: false) else { return nil }
        let parts = path.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false)
        components.path = String(parts.first ?? "/panel")
        components.query = parts.count > 1 ? String(parts[1]) : nil
        return components.url
    }

    @objc private func configurationChanged() {
        loaded = false
        if panel.isVisible { probeAndLoad(path: "/panel?shell=1") }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        if !isConfiguredOrigin(webView.url) { webView.evaluateJavaScript("delete window.tuskerShell") }
        hideUnavailable()
        retryDelay = 1
    }
    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { showUnavailable() }
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) { showUnavailable() }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard isConfiguredOrigin(webView.url), message.name == "tuskerShell", let payload = message.body as? [String: Any], let method = payload["method"] as? String else { return }
        switch method {
        case "openFull":
            if let path = payload["path"] as? String {
                hide()
                ShellRouter.shared.showMain(path: path)
            }
        case "closePanel": hide()
        case "setBadge":
            if let count = payload["count"] as? Int { ShellRouter.shared.setBadge(count) }
        case "notify":
            onBridgeNotify(payload["title"] as? String ?? "Tusker", payload["body"] as? String ?? "", payload["path"] as? String)
        case "pickFolder":
            if let requestID = payload["requestId"] as? String { presentFolderPicker(requestID: requestID) }
        default: break
        }
    }

    private func configureUnavailableView() {
        unavailableView.translatesAutoresizingMaskIntoConstraints = false
        unavailableView.wantsLayer = true
        unavailableView.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        stateTitle.font = .preferredFont(forTextStyle: .headline)
        stateHint.textColor = .secondaryLabelColor
        stateHint.maximumNumberOfLines = 3
        stateHint.alignment = .center
        let retry = NSButton(title: "Retry", target: self, action: #selector(retryNow))
        let stack = NSStackView(views: [stateTitle, stateHint, retry])
        stack.orientation = .vertical
        stack.spacing = 10
        stack.alignment = .centerX
        unavailableView.addSubview(stack)
        stack.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: unavailableView.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: unavailableView.centerYAnchor),
        ])
        panel.contentView?.addSubview(unavailableView)
        NSLayoutConstraint.activate([
            unavailableView.leadingAnchor.constraint(equalTo: panel.contentView!.leadingAnchor),
            unavailableView.trailingAnchor.constraint(equalTo: panel.contentView!.trailingAnchor),
            unavailableView.topAnchor.constraint(equalTo: panel.contentView!.topAnchor),
            unavailableView.bottomAnchor.constraint(equalTo: panel.contentView!.bottomAnchor),
        ])
        unavailableView.isHidden = true
    }

    private func showUnavailable() {
        RuntimeSupervisor.shared.ensureRunning(force: true)
        showRuntimeState()
        scheduleRetry()
    }
    private func showRuntimeState() {
        stateTitle.stringValue = RuntimeSupervisor.shared.title
        stateHint.stringValue = RuntimeSupervisor.shared.hint
        unavailableView.isHidden = false
    }
    private func showConnecting() {
        stateTitle.stringValue = "Connecting to Tusker…"
        stateHint.stringValue = config.baseURL.absoluteString
        unavailableView.isHidden = false
    }
    private func hideUnavailable() { unavailableView.isHidden = true; retryWorkItem?.cancel() }
    @objc private func retryNow() { RuntimeSupervisor.shared.ensureRunning(force: true); retryDelay = 1; loaded = false; probeAndLoad(path: "/panel?shell=1") }
    @objc private func runtimeChanged() {
        guard panel.isVisible else { return }
        loaded = false
        probeAndLoad(path: "/panel?shell=1")
    }
    private func scheduleRetry() {
        retryWorkItem?.cancel()
        guard panel.isVisible else { return }
        let work = DispatchWorkItem { [weak self] in self?.probeAndLoad(path: "/panel?shell=1") }
        retryWorkItem = work
        DispatchQueue.main.asyncAfter(deadline: .now() + retryDelay, execute: work)
        retryDelay = min(retryDelay * 2, 30)
    }

    private func presentFolderPicker(requestID: String) {
        let picker = NSOpenPanel()
        Self.configureFolderPicker(picker)
        picker.beginSheetModal(for: panel) { [weak self] response in
            self?.webView.evaluateJavaScript(Self.folderPickerResponseScript(requestID: requestID, path: response == .OK ? picker.url?.path : nil))
        }
    }

    private func isConfiguredOrigin(_ url: URL?) -> Bool {
        guard let url, let configuredOrigin = Self.configuredOrigin(config.baseURL) else { return false }
        return Self.configuredOrigin(url) == configuredOrigin
    }

    static func configuredOrigin(_ url: URL) -> String? {
        guard let scheme = url.scheme?.lowercased(), let host = url.host?.lowercased() else { return nil }
        if let port = url.port, !(scheme == "http" && port == 80), !(scheme == "https" && port == 443) { return "\(scheme)://\(host):\(port)" }
        return "\(scheme)://\(host)"
    }

    static func configureFolderPicker(_ picker: NSOpenPanel) {
        picker.canChooseFiles = false
        picker.canChooseDirectories = true
        picker.allowsMultipleSelection = false
        picker.prompt = "Choose"
    }

    static func folderPickerResponseScript(requestID: String, path: String?) -> String {
        let payload: [String: Any] = ["requestId": requestID, "path": path ?? NSNull()]
        guard let data = try? JSONSerialization.data(withJSONObject: payload), let json = String(data: data, encoding: .utf8) else { return "" }
        return "window.tuskerShell?.receiveFolderPick(\(json));"
    }

    static func bridgeScript(appVersion: String, origin: String) -> String {
        let configuration: [String: String] = ["appVersion": appVersion, "origin": origin]
        guard let data = try? JSONSerialization.data(withJSONObject: configuration), let json = String(data: data, encoding: .utf8) else { return "" }
        return """
        \(folderPickerScript(origin: origin))
        (() => {
          const config = \(json);
          if (window.location.origin !== config.origin) return;
          const shell = window.tuskerShell = window.tuskerShell || {};
          shell.openFull = (path) => window.webkit.messageHandlers.tuskerShell.postMessage({method:'openFull', path});
          shell.closePanel = () => window.webkit.messageHandlers.tuskerShell.postMessage({method:'closePanel'});
          shell.notify = (payload) => window.webkit.messageHandlers.tuskerShell.postMessage({method:'notify', ...payload});
          shell.setBadge = (count) => window.webkit.messageHandlers.tuskerShell.postMessage({method:'setBadge', count});
          shell.version = config.appVersion;
        })();
        """
    }

    static func folderPickerScript(origin: String) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: ["origin": origin]), let json = String(data: data, encoding: .utf8) else { return "" }
        return """
        (() => {
          const config = \(json);
          if (window.location.origin !== config.origin) return;
          const shell = window.tuskerShell = window.tuskerShell || {};
          const folderRequests = new Map();
          shell.pickFolder = () => new Promise((resolve) => {
            const requestId = window.crypto?.randomUUID?.() || `folder-${Date.now()}-${Math.random()}`;
            folderRequests.set(requestId, resolve);
            window.webkit.messageHandlers.tuskerShell.postMessage({method:'pickFolder', requestId});
          });
          shell.receiveFolderPick = ({requestId, path}) => {
            const resolve = folderRequests.get(requestId);
            if (!resolve) return;
            folderRequests.delete(requestId);
            resolve(typeof path === 'string' && path.length > 0 ? path : undefined);
          };
        })();
        """
    }
}

private extension Double {
    func nonZero(or fallback: Double) -> Double { self > 0 ? self : fallback }
    func clamped(to range: ClosedRange<Double>) -> Double { min(max(self, range.lowerBound), range.upperBound) }
}
