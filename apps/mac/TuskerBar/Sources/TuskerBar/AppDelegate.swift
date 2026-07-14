import AppKit
import CoreSpotlight
import KeyboardShortcuts
import UserNotifications

@MainActor
final class ShellRouter {
    static let shared = ShellRouter()
    weak var appDelegate: AppDelegate?
    func toggle() { appDelegate?.togglePanel() }
    func show(path: String) { appDelegate?.showPanel(path: path) }
    func showTask(id: String) { appDelegate?.showTask(id: id) }
    func showMain(path: String) { appDelegate?.showMainWindow(path: path) }
    func showSpotlight(identifier: String) { appDelegate?.showSpotlight(identifier: identifier) }
    func setBadge(_ count: Int) { appDelegate?.setBadge(count) }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let config = AppConfig.shared
    private let settings = SettingsWindowController()
    private let sse = SSEClient()
    private var panel: PanelController!
    private var mainWindow: MainWindowController!
    private var notifications: NotificationCoordinator!
    private let spotlight = SpotlightIndexer()
    private var statusItem: NSStatusItem!
    private var connected = false { didSet { updateStatusAppearance() } }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(config.showDockIcon ? .regular : .accessory)
        RuntimeSupervisor.shared.ensureRunning()
        panel = PanelController(config: config) { [weak self] title, body, path in self?.notifications.postBridgeNotification(title: title, body: body, path: path) }
        mainWindow = MainWindowController(config: config)
        notifications = NotificationCoordinator(panel: panel, config: config)
        ShellRouter.shared.appDelegate = self
        configureStatusItem()
        configureMainMenu()
        KeyboardShortcuts.onKeyUp(for: .toggleTuskerPanel) { [weak self] in self?.togglePanel() }
        NotificationCenter.default.addObserver(self, selector: #selector(configChanged), name: .tuskerConfigChanged, object: config)
        notifications.requestAuthorization()
        connectStream()
        spotlight.refresh(baseURL: config.baseURL)
        mainWindow.show()
    }

    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls {
            guard let link = TuskerDeepLink.parse(url) else { NSLog("Ignoring malformed Tusker URL: %@", url.absoluteString); continue }
            switch link {
            case let .open(path): showPanel(path: path)
            case let .task(id): showTask(id: id)
            case let .spotlight(identifier): showSpotlight(identifier: identifier)
            }
        }
    }

    func application(_ application: NSApplication, continue userActivity: NSUserActivity,
                     restorationHandler: @escaping ([any NSUserActivityRestoring]) -> Void) -> Bool {
        guard userActivity.activityType == CSSearchableItemActionType,
              let identifier = userActivity.userInfo?[CSSearchableItemActivityIdentifier] as? String else { return false }
        showSpotlight(identifier: identifier)
        return true
    }

    @objc private func configChanged() {
        NSApp.setActivationPolicy(config.showDockIcon ? .regular : .accessory)
        RuntimeSupervisor.shared.ensureRunning(force: true)
        spotlight.refresh(baseURL: config.baseURL)
        connectStream()
    }

    private func configureStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        guard let button = statusItem.button else { return }
        button.image = NSImage(systemSymbolName: "checkmark.circle", accessibilityDescription: "Tusker")
        button.image?.isTemplate = true
        button.toolTip = "Tusker"
        button.target = self
        button.action = #selector(statusItemPressed)
        button.sendAction(on: [.leftMouseUp, .rightMouseUp])
    }

    @objc private func statusItemPressed() {
        if let event = NSApp.currentEvent, event.type == .rightMouseUp, let button = statusItem.button {
            NSMenu.popUpContextMenu(statusMenu(), with: event, for: button)
        }
        else { togglePanel() }
    }

    private func statusMenu() -> NSMenu {
        let menu = NSMenu()
        let shortcut = KeyboardShortcuts.Name.toggleTuskerPanel.shortcut?.description ?? "⌥⌘T"
        menu.addItem(withTitle: "Toggle panel\t\(shortcut)", action: #selector(togglePanel), keyEquivalent: "")
        menu.addItem(withTitle: "Open Tusker Window", action: #selector(openMainWindow), keyEquivalent: "")
        menu.addItem(withTitle: "Enter Full Screen", action: #selector(openFullScreen), keyEquivalent: "")
        menu.addItem(withTitle: "Open in Browser", action: #selector(openTusker), keyEquivalent: "")
        menu.addItem(withTitle: "Settings…", action: #selector(openSettings), keyEquivalent: ",")
        menu.addItem(.separator())
        menu.addItem(withTitle: "Quit TuskerBar", action: #selector(quit), keyEquivalent: "q")
        for item in menu.items { item.target = self }
        return menu
    }

    @objc func togglePanel() { panel.toggle(anchor: statusItem.button) }
    func showPanel(path: String) { panel.show(path: path, anchor: statusItem.button) }
    func showTask(id: String) {
        Task {
            if let path = await TaskRouteResolver.resolve(taskID: id, baseURL: config.baseURL) {
                showPanel(path: path)
            } else {
                NSLog("Could not resolve project for Tusker task %@", id)
                showPanel(path: "/panel?shell=1")
            }
        }
    }
    func showMainWindow(path: String) { mainWindow.show(path: path) }
    func showSpotlight(identifier: String) {
        guard let route = SpotlightRoute.from(identifier: identifier) else { return }
        mainWindow.show(path: route.path)
    }
    @objc private func openMainWindow() { mainWindow.show() }
    @objc private func openFullScreen() { mainWindow.enterFullScreen() }
    @objc private func openTusker() { NSWorkspace.shared.open(config.baseURL) }
    @objc private func openSettings() { settings.show(config: config) }
    @objc private func quit() { NSApp.terminate(nil) }

    private func connectStream() {
        let streamURL = config.baseURL.appendingPathComponent("api/stream")
        sse.connect(url: streamURL, onEvent: { [weak self] event in
            self?.notifications.handle(event)
            self?.refreshSummary()
            if let baseURL = self?.config.baseURL { self?.spotlight.refresh(baseURL: baseURL) }
        }, onConnection: { [weak self] connected in
            self?.connected = connected
            if connected, let baseURL = self?.config.baseURL { self?.spotlight.refresh(baseURL: baseURL) }
        })
        refreshSummary()
    }

    private func refreshSummary() {
        let url = config.baseURL.appendingPathComponent("api/summary")
        URLSession.shared.dataTask(with: url) { [weak self] data, _, _ in
            guard let data, let summary = try? JSONDecoder().decode(TuskerSummary.self, from: data) else { return }
            DispatchQueue.main.async { self?.setBadge(summary.attention + summary.review) }
        }.resume()
    }

    func setBadge(_ count: Int) {
        statusItem.button?.title = count > 0 ? " \(count)" : ""
        NSApp.dockTile.badgeLabel = config.showDockIcon && count > 0 ? String(count) : nil
    }

    private func updateStatusAppearance() { statusItem.button?.alphaValue = connected ? 1 : 0.45 }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { mainWindow.show() }
        return true
    }

    private func configureMainMenu() {
        let main = NSMenu()
        let appItem = NSMenuItem()
        main.addItem(appItem)
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "Settings…", action: #selector(openSettings), keyEquivalent: ",")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Quit TuskerBar", action: #selector(quit), keyEquivalent: "q")
        appItem.submenu = appMenu

        let windowItem = NSMenuItem()
        main.addItem(windowItem)
        let windowMenu = NSMenu(title: "Window")
        let openWindow = windowMenu.addItem(withTitle: "Open Tusker Window", action: #selector(openMainWindow), keyEquivalent: "o")
        openWindow.target = self
        windowMenu.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        let fullScreen = windowMenu.addItem(withTitle: "Enter Full Screen", action: #selector(openFullScreen), keyEquivalent: "f")
        fullScreen.target = self
        fullScreen.keyEquivalentModifierMask = [.control, .command]
        windowItem.submenu = windowMenu
        for item in appMenu.items where !item.isSeparatorItem { item.target = self }
        NSApp.mainMenu = main
    }
}
