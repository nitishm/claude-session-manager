import Foundation

/// Finds or starts ccsd. The app never reads transcripts itself; everything goes through the daemon.
enum Daemon {
    static let sock = NSHomeDirectory() + "/.cache/ccs/ccsd.sock"
    nonisolated(unsafe) private static var proc: Process?

    static var api: API { API(http: UnixHTTP(path: sock)) }

    /// ccsd next to the app binary (the bundle layout), then the dev checkout, then PATH.
    static func binary() -> String? {
        var c: [String] = []
        if let exe = Bundle.main.executableURL {
            c.append(exe.deletingLastPathComponent().appendingPathComponent("ccsd").path)
        }
        c.append(NSHomeDirectory() + "/scripts/ccs/ccsd")
        for dir in (ProcessInfo.processInfo.environment["PATH"] ?? "").split(separator: ":") {
            c.append(dir + "/ccsd")
        }
        return c.first { FileManager.default.isExecutableFile(atPath: $0) }
    }

    /// Returns nil when ccsd answers, else a message saying why it does not.
    /// Blocking: call off the main actor.
    static func ensureRunning() -> String? {
        if (try? api.health()) != nil { return nil }
        guard let bin = binary() else { return "ccsd not found. Build it: cd go && go build -o ../ccsd ./cmd/ccsd" }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.standardOutput = FileHandle.nullDevice
        p.standardError = FileHandle.nullDevice
        do { try p.run() } catch { return "cannot start ccsd: \(error.localizedDescription)" }
        proc = p
        for _ in 0..<50 { // up to 5s: ccsd scans every transcript once before it listens
            Thread.sleep(forTimeInterval: 0.1)
            if (try? api.health()) != nil { return nil }
            if !p.isRunning { return "ccsd exited right away (is another copy starting?)" }
        }
        return "ccsd did not start in time"
    }

    /// Stops the daemon only if this app started it.
    static func stopIfOwned() {
        proc?.terminate()
        proc = nil
    }
}
