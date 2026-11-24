import SwiftUI

/// The numbers the drops are debugged with.
struct DiagnosticsView: View {
    @EnvironmentObject var model: TunnelModel

    var body: some View {
        let s = model.status
        List {
            Section("Engine") {
                row("State", s.state)
                if let up = s.uptimeSeconds { row("Up for", Self.duration(up)) }
                if let m = s.memory {
                    row("Memory", String(format: "%.1f MB heap, %.1f MB total (limit %d)", m.heapMB, m.sysMB, m.limitMB))
                }
            }
            if let c = s.counters {
                Section {
                    row("Path updates from iOS", "\(c.pathUpdates)")
                    row("Passed on to the engine", "\(c.pathChanges)")
                    row("Network settings applied", "\(c.settingsApplied)")
                    row("Identical, not re-applied", "\(c.settingsSkipped)")
                } header: {
                    Text("Network changes")
                } footer: {
                    Text("Every change passed on makes the engine re-check its connections; updates that change nothing are dropped.")
                }
            }
            if let p = s.lastPath {
                Section("Current network") {
                    row("Interface", p.satisfied ? p.interface : "none")
                    row("IPv4 / IPv6", "\(p.ipv4 ? "yes" : "no") / \(p.ipv6 ? "yes" : "no")")
                    row("Expensive", p.expensive ? "yes" : "no")
                }
            }
            if !s.health.isEmpty {
                Section("Warnings") {
                    ForEach(s.health, id: \.self) { Text($0).font(.footnote) }
                }
            }
        }
        .navigationTitle("Diagnostics")
    }

    private func row(_ k: String, _ v: String) -> some View {
        HStack { Text(k); Spacer(); Text(v).foregroundStyle(.secondary).multilineTextAlignment(.trailing) }
    }

    static func duration(_ s: Double) -> String {
        let f = DateComponentsFormatter()
        f.allowedUnits = [.day, .hour, .minute, .second]
        f.unitsStyle = .abbreviated
        f.maximumUnitCount = 2
        return f.string(from: s) ?? "\(Int(s)) s"
    }
}
