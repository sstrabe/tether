import Foundation

/// Swift face of the Go core's C API. One engine per process (the Go side holds it).
final class Core {
    static let shared = Core()

    /// Called on a Go thread for every core event.
    var onEvent: ((CoreEvent) -> Void)?
    /// Called on a Go thread with network settings to apply; must return when they are in place.
    var applySettings: ((NetworkSettings) -> Bool)?

    private init() {
        let ctx = Unmanaged.passUnretained(self).toOpaque()
        TetherSetCallbacks({ json, ctx in
            guard let json, let ctx else { return }
            let core = Unmanaged<Core>.fromOpaque(ctx).takeUnretainedValue()
            if let ev = try? JSONDecoder().decode(CoreEvent.self, from: Data(String(cString: json).utf8)) {
                core.onEvent?(ev)
            }
        }, { json, ctx in
            guard let json, let ctx else { return 1 }
            let core = Unmanaged<Core>.fromOpaque(ctx).takeUnretainedValue()
            guard let s = try? JSONDecoder().decode(NetworkSettings.self, from: Data(String(cString: json).utf8)),
                  let apply = core.applySettings else { return 2 }
            return apply(s) ? 0 : 3
        }, ctx)
    }

    private func take(_ p: UnsafeMutablePointer<CChar>?) -> String? {
        guard let p else { return nil }
        defer { TetherFree(p) }
        return String(cString: p)
    }

    private func check(_ p: UnsafeMutablePointer<CChar>?) throws {
        if let msg = take(p) { throw CoreError(message: msg) }
    }

    func start(_ config: CoreConfig) throws {
        let json = String(decoding: try JSONEncoder().encode(config), as: UTF8.self)
        try json.withCString { try check(TetherStart(UnsafeMutablePointer(mutating: $0))) }
    }

    func stop() { TetherStop() }
    func login() throws { try check(TetherLogin()) }
    func logout() throws { try check(TetherLogout()) }

    func pathChanged(_ p: CoreStatus.Path) {
        guard let data = try? JSONEncoder().encode(p) else { return }
        String(decoding: data, as: UTF8.self).withCString { TetherPathChanged(UnsafeMutablePointer(mutating: $0)) }
    }

    func wake(slept: TimeInterval) { TetherWake(slept) }

    func status() -> CoreStatus {
        guard let s = take(TetherStatus()),
              let st = try? JSONDecoder().decode(CoreStatus.self, from: Data(s.utf8)) else { return .notRunning }
        return st
    }

    func logs(_ n: Int) -> [String] {
        guard let s = take(TetherLogs(Int32(n))),
              let l = try? JSONDecoder().decode([String].self, from: Data(s.utf8)) else { return [] }
        return l
    }
}

struct CoreError: LocalizedError {
    var message: String
    var errorDescription: String? { message }
}

/// core/tether/settings.go NetworkSettings
struct NetworkSettings: Codable, Equatable {
    var addresses: [String]
    var routes: [String]
    var excludeRoutes: [String]
    var mtu: Int?
    var dnsServers: [String]
    var matchDomains: [String]
    var searchDomains: [String]
}
