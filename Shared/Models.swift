import Foundation

/// Bundle ids and the app group, shared by app and extension.
enum Tether {
    static let appGroup = "group.dev.sstrabe.tether"
    static let tunnelBundleID = "dev.sstrabe.tether.tunnel"

    /// The app group container, or (unsigned simulator builds, which have no app group) the app's
    /// own Application Support directory.
    static var containerURL: URL {
        if let url = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroup) {
            return url
        }
        let url = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }
}

/// What the core takes on start (core/tether/config.go).
struct CoreConfig: Codable, Equatable {
    var controlURL: String
    var hostname: String = ""
    var stateDir: String = ""
    var tunFD: Int32 = -1
    var memoryLimitMB: Int = 30
    var acceptRoutes: Bool = true
    var acceptDNS: Bool = true
    var exitNode: String = ""
}

/// The user's settings. Stored in the app; the parts the extension needs travel in the VPN
/// configuration's providerConfiguration.
struct TetherSettings: Codable, Equatable {
    var controlURL: String = ""
    var hostname: String = ""
    var acceptRoutes: Bool = true
    var acceptDNS: Bool = true
    var exitNode: String = ""
    var alwaysOn: Bool = true

    static let key = "settings"

    static func load() -> TetherSettings {
        let d = UserDefaults(suiteName: Tether.appGroup) ?? .standard
        var s = (d.data(forKey: key)).flatMap { try? JSONDecoder().decode(TetherSettings.self, from: $0) } ?? TetherSettings()
        // An MDM-managed app configuration can supply the control server.
        if s.controlURL.isEmpty,
           let managed = UserDefaults.standard.dictionary(forKey: "com.apple.configuration.managed"),
           let url = managed["ControlURL"] as? String {
            s.controlURL = url
        }
        return s
    }

    func save() {
        let d = UserDefaults(suiteName: Tether.appGroup) ?? .standard
        if let data = try? JSONEncoder().encode(self) { d.set(data, forKey: Self.key) }
    }

    var providerConfiguration: [String: Any] {
        ["settings": (try? JSONEncoder().encode(self)) ?? Data()]
    }

    init() {}

    init?(providerConfiguration: [String: Any]?) {
        guard let data = providerConfiguration?["settings"] as? Data,
              let s = try? JSONDecoder().decode(TetherSettings.self, from: data) else { return nil }
        self = s
    }

    func coreConfig(stateDir: URL, tunFD: Int32) -> CoreConfig {
        CoreConfig(controlURL: controlURL, hostname: hostname, stateDir: stateDir.path, tunFD: tunFD,
                   acceptRoutes: acceptRoutes, acceptDNS: acceptDNS, exitNode: exitNode)
    }
}

/// core/tether/status.go
struct CoreStatus: Codable, Equatable {
    struct Node: Codable, Equatable, Identifiable {
        var name: String
        var dnsName: String?
        var os: String?
        var ips: [String]
        var online: Bool
        var relay: String?
        var curAddr: String?
        var exitNode: Bool?
        var exitNodeOption: Bool?
        var lastSeen: String?
        var rxBytes: Int64?
        var txBytes: Int64?
        var lastHandshake: String?
        var id: String { (ips.first ?? "") + name }
        var shortName: String { dnsName?.split(separator: ".").first.map(String.init) ?? name }
        var isDirect: Bool { !(curAddr ?? "").isEmpty }
    }
    struct Counters: Codable, Equatable {
        var pathUpdates: Int
        var pathChanges: Int
        var settingsApplied: Int
        var settingsSkipped: Int
    }
    struct Memory: Codable, Equatable {
        var heapMB: Double
        var sysMB: Double
        var limitMB: Int
        var numGC: Int
    }
    struct Path: Codable, Equatable {
        var satisfied: Bool
        var interface: String
        var ipv4: Bool
        var ipv6: Bool
        var expensive: Bool
    }
    var state: String
    var `self`: Node?
    var peers: [Node]
    var health: [String]
    var authURL: String?
    var tailnetName: String?
    var uptimeSeconds: Double?
    var counters: Counters?
    var lastPath: Path?
    var memory: Memory?

    static let notRunning = CoreStatus(state: "NotRunning", peers: [], health: [])
}

/// core/tether/engine.go Event
struct CoreEvent: Codable {
    var kind: String
    var state: String?
    var url: String?
    var error: String?
    var path: CoreStatus.Path?
}

/// Messages between app and extension (sendProviderMessage).
struct ProviderRequest: Codable {
    enum Command: String, Codable { case status, logs, login, logout }
    var command: Command
    var lines: Int?
}

struct ProviderReply: Codable {
    var status: CoreStatus?
    var logs: [String]?
    var error: String?
}
