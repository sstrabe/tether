import Foundation
import NetworkExtension
import SwiftUI

/// Owns the VPN configuration and talks to the extension. In test mode (simulator) it runs the core
/// in the app instead, without a tunnel, so the UI and the control-server login can be exercised
/// where no packet tunnel can run.
@MainActor
final class TunnelModel: ObservableObject {
    enum Mode: String { case tunnel, inApp }

    @Published var settings = TetherSettings.load()
    @Published private(set) var status = CoreStatus.notRunning
    @Published private(set) var vpnStatus: NEVPNStatus = .invalid
    @Published private(set) var lastError: String?
    @Published private(set) var authURL: URL?
    @Published var mode: Mode = UserDefaults.standard.string(forKey: "mode").flatMap(Mode.init) ?? TunnelModel.defaultMode

    private var manager: NETunnelProviderManager?
    private var poll: Timer?
    private var inAppRunning = false
    private var observer: NSObjectProtocol?

    static var defaultMode: Mode {
        #if targetEnvironment(simulator)
        return .inApp
        #else
        return .tunnel
        #endif
    }

    init() {
        observer = NotificationCenter.default.addObserver(forName: .NEVPNStatusDidChange, object: nil, queue: .main) { [weak self] note in
            guard let conn = note.object as? NEVPNConnection else { return }
            Task { @MainActor in self?.vpnStatus = conn.status }
        }
        Task { await load() }
        poll = Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { [weak self] _ in
            Task { @MainActor in await self?.refresh() }
        }
    }

    var isOn: Bool {
        switch mode {
        case .tunnel: return [.connected, .connecting, .reasserting].contains(vpnStatus)
        case .inApp: return inAppRunning
        }
    }

    var stateText: String {
        if mode == .tunnel {
            switch vpnStatus {
            case .invalid: return "Not set up"
            case .disconnected: return "Off"
            case .connecting: return "Connecting"
            case .reasserting: return "Reconnecting"
            case .disconnecting: return "Turning off"
            case .connected: return status.state == "Running" ? "Connected" : Self.describe(status.state)
            @unknown default: return "Unknown"
            }
        }
        return inAppRunning ? Self.describe(status.state) : "Off"
    }

    static func describe(_ state: String) -> String {
        switch state {
        case "NeedsLogin": return "Needs sign-in"
        case "NeedsMachineAuth": return "Waiting for approval"
        case "Starting": return "Starting"
        case "Running": return "Connected"
        case "Stopped": return "Stopped"
        case "NotRunning": return "Off"
        default: return state
        }
    }

    // MARK: - VPN configuration

    func load() async {
        guard mode == .tunnel else { return }
        do {
            let all = try await NETunnelProviderManager.loadAllFromPreferences()
            manager = all.first { ($0.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier == Tether.tunnelBundleID }
            vpnStatus = manager?.connection.status ?? .invalid
        } catch {
            lastError = error.localizedDescription
        }
    }

    /// Writes the configuration iOS keeps for us: our extension, the settings it needs, and the
    /// on-demand rule that brings the tunnel back by itself whenever it is down.
    private func saveConfiguration() async throws -> NETunnelProviderManager {
        let m = manager ?? NETunnelProviderManager()
        let proto = (m.protocolConfiguration as? NETunnelProviderProtocol) ?? NETunnelProviderProtocol()
        proto.providerBundleIdentifier = Tether.tunnelBundleID
        proto.serverAddress = URL(string: settings.controlURL)?.host ?? "tailnet"
        proto.providerConfiguration = settings.providerConfiguration
        proto.disconnectOnSleep = false
        m.protocolConfiguration = proto
        m.localizedDescription = "Tether"
        let connect = NEOnDemandRuleConnect()
        connect.interfaceTypeMatch = .any
        m.onDemandRules = [connect]
        m.isOnDemandEnabled = settings.alwaysOn
        m.isEnabled = true
        try await m.saveToPreferences()
        try await m.loadFromPreferences()
        manager = m
        return m
    }

    func applySettings() async {
        settings.save()
        guard mode == .tunnel, manager != nil else { return }
        do { _ = try await saveConfiguration() } catch { lastError = error.localizedDescription }
    }

    // MARK: - On/off

    func turnOn() async {
        lastError = nil
        guard !settings.controlURL.isEmpty else {
            lastError = "Set the control server in Settings first."
            return
        }
        settings.save()
        switch mode {
        case .tunnel:
            do {
                let m = try await saveConfiguration()
                try m.connection.startVPNTunnel()
            } catch {
                lastError = error.localizedDescription
            }
        case .inApp:
            startInApp()
        }
    }

    func turnOff() async {
        switch mode {
        case .tunnel:
            guard let m = manager else { return }
            // Without this, the on-demand rule would start it again at once.
            m.isOnDemandEnabled = false
            try? await m.saveToPreferences()
            m.connection.stopVPNTunnel()
        case .inApp:
            Core.shared.stop()
            inAppRunning = false
            status = .notRunning
        }
    }

    func setMode(_ m: Mode) async {
        if isOn { await turnOff() }
        mode = m
        UserDefaults.standard.set(m.rawValue, forKey: "mode")
        await load()
        await refresh()
    }

    private func startInApp() {
        let dir = Tether.containerURL.appendingPathComponent("inapp-state", isDirectory: true)
        let core = Core.shared
        core.onEvent = { [weak self] ev in
            guard ev.kind == "browseToURL", let s = ev.url, let url = URL(string: s) else { return }
            Task { @MainActor in self?.authURL = url }
        }
        do {
            try core.start(settings.coreConfig(stateDir: dir, tunFD: -1))
            inAppRunning = true
        } catch {
            lastError = error.localizedDescription
        }
    }

    // MARK: - Talking to the engine

    func refresh() async {
        switch mode {
        case .inApp:
            status = inAppRunning ? Core.shared.status() : .notRunning
        case .tunnel:
            guard let m = manager, m.connection.status == .connected || m.connection.status == .reasserting else {
                status = .notRunning
                return
            }
            if let r = await send(.init(command: .status)), let s = r.status { status = s }
            if let s = UserDefaults(suiteName: Tether.appGroup)?.string(forKey: "authURL") {
                authURL = URL(string: s)
            }
        }
        if let s = status.authURL, !s.isEmpty { authURL = URL(string: s) }
        if status.state == "Running" { authURL = nil }
    }

    func login() async {
        switch mode {
        case .inApp:
            do { try Core.shared.login() } catch { lastError = error.localizedDescription }
        case .tunnel:
            if let r = await send(.init(command: .login)), let e = r.error { lastError = e }
        }
        await refresh()
    }

    func logout() async {
        switch mode {
        case .inApp:
            do { try Core.shared.logout() } catch { lastError = error.localizedDescription }
        case .tunnel:
            if let r = await send(.init(command: .logout)), let e = r.error { lastError = e }
        }
        await refresh()
    }

    func logs() async -> [String] {
        switch mode {
        case .inApp: return Core.shared.logs(2000)
        case .tunnel: return await send(.init(command: .logs, lines: 2000))?.logs ?? []
        }
    }

    private func send(_ req: ProviderRequest) async -> ProviderReply? {
        guard let session = manager?.connection as? NETunnelProviderSession,
              let data = try? JSONEncoder().encode(req) else { return nil }
        return await withCheckedContinuation { cont in
            do {
                try session.sendProviderMessage(data) { reply in
                    cont.resume(returning: reply.flatMap { try? JSONDecoder().decode(ProviderReply.self, from: $0) })
                }
            } catch {
                cont.resume(returning: nil)
            }
        }
    }
}
