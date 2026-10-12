import SwiftUI
import AppKit

@main
struct CCSApp: App {
    private let store = Store.shared
    @NSApplicationDelegateAdaptor private var delegate: AppDelegate

    var body: some Scene {
        WindowGroup("Claude Sessions", id: "main") {
            ContentView(store: store)
                .task { store.start() }
        }
        .defaultSize(width: 1100, height: 680)

        MenuBarExtra {
            MenuContent(store: store)
        } label: {
            Image(systemName: store.busyCount > 0 ? "terminal.fill" : "terminal")
            if store.live.count > 0 { Text("\(store.live.count)") }
        }
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ n: Notification) {
        NSApp.setActivationPolicy(.regular) // also lets a bare binary run from a terminal show a window
        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationWillTerminate(_ n: Notification) { Daemon.stopIfOwned() }
}

/// The menu bar list: running sessions with their state. Picking one opens the app on it.
struct MenuContent: View {
    @Bindable var store: Store
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        if store.live.isEmpty {
            Text("No running sessions")
        } else {
            ForEach(store.live) { s in
                Button {
                    store.clearFilters()
                    store.selection = [s.id]
                    show()
                } label: {
                    Text("\(glyph(s)) \(s.displayLabel) · \(s.dir)")
                }
            }
        }
        Divider()
        Button("Open Claude Sessions") { show() }.keyboardShortcut("o")
        Button("Quit") { NSApp.terminate(nil) }.keyboardShortcut("q")
    }

    private func glyph(_ s: Session) -> String {
        switch s.live?.status {
        case "idle": return "●"
        case "busy": return "◐"
        default: return "?"
        }
    }

    private func show() {
        openWindow(id: "main")
        NSApp.activate(ignoringOtherApps: true)
    }
}
