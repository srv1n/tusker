import AppKit
import WebKit

@MainActor
final class MainWindowController: NSObject, WKNavigationDelegate, WKScriptMessageHandler, NSWindowDelegate {
    private let config: AppConfig
    private let window: NSWindow
    private let webView: WKWebView
    private let stateView = NSView()
    private let stateTitle = NSTextField(labelWithString: "Connecting to Tusker…")
    private let stateHint = NSTextField(labelWithString: "")
    private var loaded = false

    init(config: AppConfig) {
        self.config = config
        let frame = NSRect(x: 0, y: 0, width: 1180, height: 780)
        window = NSWindow(contentRect: frame, styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        let content = WKUserContentController()
        content.addUserScript(WKUserScript(source: PanelController.folderPickerScript(origin: PanelController.configuredOrigin(config.baseURL) ?? ""), injectionTime: .atDocumentStart, forMainFrameOnly: true))
        let webConfig = WKWebViewConfiguration()
        webConfig.userContentController = content
        webView = WKWebView(frame: frame, configuration: webConfig)
        super.init()
        content.add(self, name: "tuskerShell")
        window.title = "Tusker"
        window.collectionBehavior = [.fullScreenPrimary]
        window.isReleasedWhenClosed = false
        window.delegate = self
        window.setFrameAutosaveName("TuskerMainWindow")
        window.center()
        let contentView = NSView()
        window.contentView = contentView
        webView.translatesAutoresizingMaskIntoConstraints = false
        webView.navigationDelegate = self
        if #available(macOS 13.3, *) { webView.isInspectable = true }
        contentView.addSubview(webView)
        NSLayoutConstraint.activate([
            webView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            webView.topAnchor.constraint(equalTo: contentView.topAnchor),
            webView.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
        ])
        configureStateView()
        NotificationCenter.default.addObserver(self, selector: #selector(configurationChanged), name: .tuskerConfigChanged, object: config)
        NotificationCenter.default.addObserver(self, selector: #selector(runtimeChanged), name: .tuskerRuntimeChanged, object: RuntimeSupervisor.shared)
    }

    func show(path: String? = nil) {
        window.contentView?.layoutSubtreeIfNeeded()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        if let path {
            loaded ? load(path: path) : probeAndLoad(path: path)
        } else if !loaded {
            probeAndLoad(path: "/")
        }
    }

    func enterFullScreen() {
        show()
        if !window.styleMask.contains(.fullScreen) { window.toggleFullScreen(nil) }
    }

    private func probeAndLoad(path: String) {
        let runtime = RuntimeSupervisor.shared
        if runtime.state == .idle { runtime.ensureRunning() }
        switch runtime.state {
        case .checking, .starting, .failed:
            showState(title: runtime.title, hint: runtime.hint)
            return
        case .idle:
            showState(title: runtime.title, hint: runtime.hint)
            return
        case .running, .external:
            showState(title: "Connecting to Tusker…", hint: config.baseURL.absoluteString)
        }
        let healthURL = config.baseURL.appendingPathComponent("api/summary")
        URLSession.shared.dataTask(with: healthURL) { [weak self] _, response, error in
            DispatchQueue.main.async {
                guard let self else { return }
                if error == nil, (response as? HTTPURLResponse)?.statusCode == 200 {
                    self.load(path: path)
                } else {
                    runtime.ensureRunning(force: true)
                    self.showState(title: runtime.title, hint: runtime.hint)
                }
            }
        }.resume()
    }

    private func load(path: String) {
        guard var components = URLComponents(url: config.baseURL, resolvingAgainstBaseURL: false) else { return }
        let parts = path.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false)
        components.path = String(parts.first ?? "/")
        components.query = parts.count > 1 ? String(parts[1]) : nil
        guard let url = components.url else { return }
        loaded = true
        webView.load(URLRequest(url: url))
    }

    private func configureStateView() {
        stateView.translatesAutoresizingMaskIntoConstraints = false
        stateView.wantsLayer = true
        stateView.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        stateTitle.font = .preferredFont(forTextStyle: .title2)
        stateHint.textColor = .secondaryLabelColor
        stateHint.maximumNumberOfLines = 3
        stateHint.alignment = .center
        let retry = NSButton(title: "Retry", target: self, action: #selector(retry))
        let stack = NSStackView(views: [stateTitle, stateHint, retry])
        stack.orientation = .vertical
        stack.alignment = .centerX
        stack.spacing = 12
        stack.translatesAutoresizingMaskIntoConstraints = false
        stateView.addSubview(stack)
        window.contentView?.addSubview(stateView)
        NSLayoutConstraint.activate([
            stateView.leadingAnchor.constraint(equalTo: window.contentView!.leadingAnchor),
            stateView.trailingAnchor.constraint(equalTo: window.contentView!.trailingAnchor),
            stateView.topAnchor.constraint(equalTo: window.contentView!.topAnchor),
            stateView.bottomAnchor.constraint(equalTo: window.contentView!.bottomAnchor),
            stack.centerXAnchor.constraint(equalTo: stateView.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: stateView.centerYAnchor),
        ])
    }

    private func showState(title: String, hint: String) {
        stateTitle.stringValue = title
        stateHint.stringValue = hint
        stateView.isHidden = false
    }

    @objc private func retry() { RuntimeSupervisor.shared.ensureRunning(force: true); loaded = false; probeAndLoad(path: "/") }
    @objc private func configurationChanged() { loaded = false; if window.isVisible { probeAndLoad(path: "/") } }
    @objc private func runtimeChanged() {
        guard window.isVisible else { return }
        loaded = false
        probeAndLoad(path: "/")
    }
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { stateView.isHidden = true }
    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { showState(title: "Tusker failed to load", hint: error.localizedDescription) }
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        let runtime = RuntimeSupervisor.shared
        runtime.ensureRunning(force: true)
        showState(title: runtime.title, hint: runtime.hint)
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.name == "tuskerShell", isConfiguredOrigin(webView.url), let payload = message.body as? [String: Any], payload["method"] as? String == "pickFolder", let requestID = payload["requestId"] as? String else { return }
        let picker = NSOpenPanel()
        PanelController.configureFolderPicker(picker)
        picker.beginSheetModal(for: window) { [weak self] response in
            self?.webView.evaluateJavaScript(PanelController.folderPickerResponseScript(requestID: requestID, path: response == .OK ? picker.url?.path : nil))
        }
    }

    private func isConfiguredOrigin(_ url: URL?) -> Bool {
        guard let url, let configuredOrigin = PanelController.configuredOrigin(config.baseURL) else { return false }
        return PanelController.configuredOrigin(url) == configuredOrigin
    }
}
