import SwiftUI

struct LogView: View {
    @EnvironmentObject var model: TunnelModel
    @State private var lines: [String] = []
    @State private var filter = ""

    var shown: [String] {
        let l = filter.isEmpty ? lines : lines.filter { $0.localizedCaseInsensitiveContains(filter) }
        return l.suffix(800).reversed()
    }

    var body: some View {
        List(Array(shown.enumerated()), id: \.offset) { _, line in
            Text(line).font(.system(.caption2, design: .monospaced)).textSelection(.enabled)
        }
        .listStyle(.plain)
        .searchable(text: $filter, prompt: "Filter")
        .navigationTitle("Log")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                ShareLink(item: lines.joined(separator: "\n")) { Image(systemName: "square.and.arrow.up") }
                    .disabled(lines.isEmpty)
            }
        }
        .task { lines = await model.logs() }
        .refreshable { lines = await model.logs() }
    }
}
