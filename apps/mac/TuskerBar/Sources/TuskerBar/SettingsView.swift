import AppKit
import KeyboardShortcuts
import ServiceManagement
import SwiftUI

extension KeyboardShortcuts.Name {
    static let toggleTuskerPanel = Self("toggleTuskerPanel", default: .init(.t, modifiers: [.option, .command]))
}

struct SettingsView: View {
    @ObservedObject var config: AppConfig
    @State private var draftURL: String
    @State private var error: String?
    @State private var launchAtLogin = SMAppService.mainApp.status == .enabled

    init(config: AppConfig) {
        self.config = config
        _draftURL = State(initialValue: config.baseURLString)
    }

    var body: some View {
        Form {
            Section("Panel") {
                KeyboardShortcuts.Recorder("Toggle panel", name: .toggleTuskerPanel)
                TextField("Base URL", text: $draftURL)
                if let error { Text(error).foregroundStyle(.red) }
                Button("Apply URL") { error = config.applyBaseURL(draftURL) }
            }
            Section("Application") {
                Toggle("Launch at login", isOn: Binding(get: { launchAtLogin }, set: setLaunchAtLogin))
                Toggle("Show Dock icon", isOn: $config.showDockIcon)
            }
            Section("Notifications") {
                Toggle("Attention notifications", isOn: $config.notifyAttention)
                Toggle("Critical notifications", isOn: $config.notifyCritical)
            }
        }
        .padding()
        .frame(width: 420)
    }

    private func setLaunchAtLogin(_ enabled: Bool) {
        do {
            if enabled { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            launchAtLogin = SMAppService.mainApp.status == .enabled
        } catch let caught { self.error = caught.localizedDescription; launchAtLogin = SMAppService.mainApp.status == .enabled }
    }
}

@MainActor
final class SettingsWindowController {
    private var window: NSWindow?
    func show(config: AppConfig) {
        if window == nil {
            let host = NSHostingController(rootView: SettingsView(config: config))
            window = NSWindow(contentViewController: host)
            window?.title = "TuskerBar Settings"
            window?.styleMask = [.titled, .closable, .miniaturizable]
            window?.setFrameAutosaveName("TuskerBarSettings")
        }
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
}
