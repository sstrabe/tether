import SwiftUI

@main
struct TetherApp: App {
    @StateObject private var model = TunnelModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
        }
    }
}
