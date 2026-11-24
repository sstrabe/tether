import SwiftUI

struct SettingsView: View {
    @EnvironmentObject var model: TunnelModel
    @Environment(\.dismiss) private var dismiss
    @State private var draft = TetherSettings()
    @State private var confirmLogout = false

    var exitNodes: [CoreStatus.Node] { model.status.peers.filter { $0.exitNodeOption == true } }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("https://…", text: $draft.controlURL)
                        .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                        .accessibilityIdentifier("controlURL")
                    TextField("Device name (optional)", text: $draft.hostname)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                } header: {
                    Text("Control server")
                }

                Section {
                    Toggle("Always on", isOn: $draft.alwaysOn)
                    Toggle("Use subnet routes", isOn: $draft.acceptRoutes)
                    Toggle("Use tailnet DNS", isOn: $draft.acceptDNS)
                    Picker("Exit node", selection: $draft.exitNode) {
                        Text("None").tag("")
                        ForEach(exitNodes) { n in Text(n.shortName).tag(n.ips.first ?? "") }
                        if !draft.exitNode.isEmpty && !exitNodes.contains(where: { $0.ips.first == draft.exitNode }) {
                            Text(draft.exitNode).tag(draft.exitNode)
                        }
                    }
                } footer: {
                    Text("Always on brings the tunnel back by itself whenever it is down. Changes apply the next time it connects.")
                }

                Section {
                    Picker("Engine", selection: Binding(get: { model.mode }, set: { m in Task { await model.setMode(m) } })) {
                        Text("Tunnel").tag(TunnelModel.Mode.tunnel)
                        Text("In the app (test)").tag(TunnelModel.Mode.inApp)
                    }
                } header: {
                    Text("Testing")
                } footer: {
                    Text("In the app runs the node without a tunnel, for the simulator. It is a separate device in the tailnet.")
                }

                if model.status.me != nil {
                    Section {
                        Button("Sign out", role: .destructive) { confirmLogout = true }
                    }
                }
            }
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        model.settings = draft
                        Task { await model.applySettings() }
                        dismiss()
                    }
                    .disabled(!draft.controlURL.isEmpty && !draft.controlURL.hasPrefix("https://"))
                    .accessibilityIdentifier("save")
                }
            }
            .confirmationDialog("Sign this device out of the tailnet?", isPresented: $confirmLogout, titleVisibility: .visible) {
                Button("Sign out", role: .destructive) { Task { await model.logout() } }
            }
            .onAppear { draft = model.settings }
        }
    }
}
