import SwiftUI

struct ContentView: View {
    @EnvironmentObject var model: TunnelModel
    @Environment(\.openURL) private var openURL
    @State private var showSettings = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(model.stateText).font(.headline)
                            if let me = model.status.me, let ip = me.ips.first {
                                Text("\(me.shortName) · \(ip)")
                                    .font(.subheadline).foregroundStyle(.secondary)
                                    .accessibilityIdentifier("self")
                            } else if let tn = model.status.tailnetName {
                                Text(tn).font(.subheadline).foregroundStyle(.secondary)
                            }
                        }
                        Spacer()
                        Toggle("Connected", isOn: Binding(
                            get: { model.isOn },
                            set: { on in Task { on ? await model.turnOn() : await model.turnOff() } }
                        ))
                        .labelsHidden()
                        .accessibilityIdentifier("power")
                    }
                    if model.mode == .inApp {
                        Label("Test mode: engine in the app, no tunnel", systemImage: "testtube.2")
                            .font(.footnote).foregroundStyle(.orange)
                    }
                    if let err = model.lastError {
                        Text(err).font(.footnote).foregroundStyle(.red)
                    }
                    if let url = model.authURL, model.status.state == "NeedsLogin" {
                        Button("Sign in") { openURL(url) }
                            .accessibilityIdentifier("signin")
                    } else if model.status.state == "NeedsLogin" {
                        Button("Get sign-in link") { Task { await model.login() } }
                    }
                }

                if !model.status.peers.isEmpty {
                    Section("Devices") {
                        ForEach(model.status.peers) { PeerRow(peer: $0) }
                    }
                }

                Section {
                    NavigationLink("Diagnostics") { DiagnosticsView() }
                    NavigationLink("Log") { LogView() }
                }
            }
            .navigationTitle("Tether")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showSettings = true } label: { Image(systemName: "gearshape") }
                        .accessibilityLabel("Settings")
                }
            }
            .sheet(isPresented: $showSettings) { SettingsView() }
            .refreshable { await model.refresh() }
        }
    }
}

struct PeerRow: View {
    var peer: CoreStatus.Node

    var body: some View {
        HStack {
            Circle().fill(peer.online ? Color.green : Color.secondary.opacity(0.4)).frame(width: 8, height: 8)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(peer.shortName)
                    if peer.exitNode == true {
                        Text("exit").font(.caption2).padding(.horizontal, 4)
                            .background(Capsule().fill(Color.accentColor.opacity(0.15)))
                    }
                }
                Text(peer.ips.first ?? "").font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if peer.online {
                Text(peer.isDirect ? "direct" : "relay \(peer.relay ?? "")")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .contextMenu {
            if let ip = peer.ips.first { Button("Copy IP") { UIPasteboard.general.string = ip } }
            if let dns = peer.dnsName { Button("Copy name") { UIPasteboard.general.string = dns } }
        }
    }
}
