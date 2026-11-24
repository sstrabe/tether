import Foundation
import Network
import NetworkExtension
import os

/// Runs the node. The Go core does the networking; this class hands it the utun descriptor, applies
/// the network settings it asks for, forwards path changes and wake-ups, and answers the app.
final class PacketTunnelProvider: NEPacketTunnelProvider {
    private let log = Logger(subsystem: "dev.sstrabe.tether", category: "tunnel")
    private let core = Core.shared
    private var pathMonitor: NWPathMonitor?
    private let pathQueue = DispatchQueue(label: "dev.sstrabe.tether.path")
    private var sleptAt: Date?
    private var lastState = "NoState"

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        guard let proto = protocolConfiguration as? NETunnelProviderProtocol,
              let settings = TetherSettings(providerConfiguration: proto.providerConfiguration),
              !settings.controlURL.isEmpty else {
            completionHandler(CoreError(message: "No control server configured. Open the Tether app."))
            return
        }

        // An initial configuration brings the utun up so its descriptor can be found; the core
        // replaces it with the real addresses and routes once it has a netmap.
        let initial = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "100.100.100.100")
        initial.mtu = 1280
        setTunnelNetworkSettings(initial) { [weak self] error in
            guard let self else { return }
            if let error {
                completionHandler(error)
                return
            }
            guard let fd = Self.utunDescriptor() else {
                completionHandler(CoreError(message: "Could not find the tunnel's file descriptor."))
                return
            }
            self.core.applySettings = { [weak self] s in self?.apply(s) ?? false }
            self.core.onEvent = { [weak self] ev in self?.handle(ev) }
            let stateDir = Tether.containerURL.appendingPathComponent("state", isDirectory: true)
            do {
                try self.core.start(settings.coreConfig(stateDir: stateDir, tunFD: fd))
            } catch {
                self.log.error("core start failed: \(error.localizedDescription, privacy: .public)")
                completionHandler(error)
                return
            }
            self.startPathMonitor()
            completionHandler(nil)
        }
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        log.info("stopping, reason \(reason.rawValue)")
        pathMonitor?.cancel()
        pathMonitor = nil
        core.stop()
        completionHandler()
    }

    override func sleep(completionHandler: @escaping () -> Void) {
        sleptAt = Date()
        completionHandler()
    }

    override func wake() {
        let slept = sleptAt.map { Date().timeIntervalSince($0) } ?? 0
        sleptAt = nil
        core.wake(slept: slept)
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        var reply = ProviderReply()
        if let req = try? JSONDecoder().decode(ProviderRequest.self, from: messageData) {
            switch req.command {
            case .status:
                reply.status = core.status()
            case .logs:
                reply.logs = core.logs(req.lines ?? 500)
            case .login:
                do { try core.login() } catch { reply.error = error.localizedDescription }
                reply.status = core.status()
            case .logout:
                do { try core.logout() } catch { reply.error = error.localizedDescription }
                reply.status = core.status()
            }
        } else {
            reply.error = "bad request"
        }
        completionHandler?(try? JSONEncoder().encode(reply))
    }

    // MARK: - Core callbacks

    private func handle(_ ev: CoreEvent) {
        switch ev.kind {
        case "state":
            let state = ev.state ?? ""
            log.info("state \(state, privacy: .public)")
            // A fresh install, or a node key the server expired: ask for a login URL right away,
            // so the app has one to open.
            if state == "NeedsLogin" && lastState != "NeedsLogin" {
                DispatchQueue.global().async { try? self.core.login() }
            }
            lastState = state
        case "browseToURL":
            if let url = ev.url {
                let d = UserDefaults(suiteName: Tether.appGroup)
                d?.set(url, forKey: "authURL")
                d?.set(Date(), forKey: "authURLAt")
            }
        case "error":
            log.error("core error: \(ev.error ?? "", privacy: .public)")
        default:
            break
        }
    }

    /// Applies the core's network settings and waits for iOS to finish. Called on a Go thread.
    private func apply(_ s: NetworkSettings) -> Bool {
        let ns = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "100.100.100.100")
        ns.mtu = NSNumber(value: s.mtu ?? 1280)

        var v4Addr: [String] = [], v4Mask: [String] = []
        var v6Addr: [String] = [], v6Len: [NSNumber] = []
        for cidr in s.addresses {
            guard let (ip, bits) = Self.split(cidr) else { continue }
            if ip.contains(":") { v6Addr.append(ip); v6Len.append(NSNumber(value: bits)) }
            else { v4Addr.append(ip); v4Mask.append(Self.mask(bits)) }
        }
        var v4Routes: [NEIPv4Route] = [], v6Routes: [NEIPv6Route] = []
        for cidr in s.routes {
            guard let (ip, bits) = Self.split(cidr) else { continue }
            if ip.contains(":") { v6Routes.append(NEIPv6Route(destinationAddress: ip, networkPrefixLength: NSNumber(value: bits))) }
            else { v4Routes.append(NEIPv4Route(destinationAddress: ip, subnetMask: Self.mask(bits))) }
        }
        var v4Excl: [NEIPv4Route] = [], v6Excl: [NEIPv6Route] = []
        for cidr in s.excludeRoutes {
            guard let (ip, bits) = Self.split(cidr) else { continue }
            if ip.contains(":") { v6Excl.append(NEIPv6Route(destinationAddress: ip, networkPrefixLength: NSNumber(value: bits))) }
            else { v4Excl.append(NEIPv4Route(destinationAddress: ip, subnetMask: Self.mask(bits))) }
        }
        if !v4Addr.isEmpty {
            let v4 = NEIPv4Settings(addresses: v4Addr, subnetMasks: v4Mask)
            v4.includedRoutes = v4Routes
            v4.excludedRoutes = v4Excl
            ns.ipv4Settings = v4
        }
        if !v6Addr.isEmpty {
            let v6 = NEIPv6Settings(addresses: v6Addr, networkPrefixLengths: v6Len)
            v6.includedRoutes = v6Routes
            v6.excludedRoutes = v6Excl
            ns.ipv6Settings = v6
        }
        if !s.dnsServers.isEmpty {
            let dns = NEDNSSettings(servers: s.dnsServers)
            // Empty match domains: every query goes to these servers (MagicDNS as the resolver).
            dns.matchDomains = s.matchDomains.isEmpty ? [""] : s.matchDomains
            dns.searchDomains = s.searchDomains
            ns.dnsSettings = dns
        }

        let done = DispatchSemaphore(value: 0)
        var ok = false
        setTunnelNetworkSettings(ns) { [log] error in
            if let error { log.error("settings: \(error.localizedDescription, privacy: .public)") }
            ok = error == nil
            done.signal()
        }
        if done.wait(timeout: .now() + 20) == .timedOut {
            log.error("settings: timed out")
            return false
        }
        return ok
    }

    // MARK: - Network path

    private func startPathMonitor() {
        let m = NWPathMonitor()
        m.pathUpdateHandler = { [weak self] path in
            guard let self else { return }
            // Our own utun and other VPN interfaces are not a way out.
            let out = path.availableInterfaces.first { iface in
                !(iface.name.hasPrefix("utun") || iface.name.hasPrefix("ipsec") || iface.type == .loopback || iface.type == .other)
            }
            self.core.pathChanged(CoreStatus.Path(
                satisfied: path.status == .satisfied && out != nil,
                interface: out?.name ?? "",
                ipv4: path.supportsIPv4,
                ipv6: path.supportsIPv6,
                expensive: path.isExpensive))
        }
        m.start(queue: pathQueue)
        pathMonitor = m
    }

    // MARK: - Helpers

    /// Finds the utun socket iOS opened for this provider (the same lookup WireGuard's iOS client uses).
    static func utunDescriptor() -> Int32? {
        var info = ctl_info()
        withUnsafeMutablePointer(to: &info.ctl_name) {
            $0.withMemoryRebound(to: CChar.self, capacity: MemoryLayout.size(ofValue: $0.pointee)) {
                _ = strcpy($0, "com.apple.net.utun_control")
            }
        }
        for fd: Int32 in 0...1024 {
            var addr = sockaddr_ctl()
            var len = socklen_t(MemoryLayout.size(ofValue: addr))
            let ret = withUnsafeMutablePointer(to: &addr) {
                $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getpeername(fd, $0, &len) }
            }
            if ret != 0 || addr.sc_family != UInt8(AF_SYSTEM) { continue }
            if info.ctl_id == 0, ioctl(fd, CTLIOCGINFO, &info) != 0 { continue }
            if addr.sc_id == info.ctl_id { return fd }
        }
        return nil
    }

    static func split(_ cidr: String) -> (String, Int)? {
        let parts = cidr.split(separator: "/")
        guard parts.count == 2, let bits = Int(parts[1]) else { return nil }
        return (String(parts[0]), bits)
    }

    static func mask(_ bits: Int) -> String {
        let m: UInt32 = bits == 0 ? 0 : ~UInt32(0) << (32 - UInt32(bits))
        return "\(m >> 24 & 255).\(m >> 16 & 255).\(m >> 8 & 255).\(m & 255)"
    }
}
