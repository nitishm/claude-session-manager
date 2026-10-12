import AppKit
import Foundation
import Observation

/// All app state. Views read it; every ccsd call happens in a detached task so the UI never blocks.
@MainActor @Observable
final class Store {
    static let shared = Store()
    enum Conn: Equatable { case starting, ok, down(String) }

    static let sorts = ["recent", "dir", "prompts", "oldest", "tags"]

    var conn: Conn = .starting
    private(set) var fetched: [Session] = []
    private(set) var tagCounts: [TagCount] = []
    var selection = Set<String>()
    var flash = ""
    var busy = Set<String>() // ids with an AI call running

    // Search and filters. Changing any of them refetches.
    var query = "" { didSet { if query != oldValue { refreshSoon(after: 0.15) } } }
    var sort = "recent" { didSet { if sort != oldValue { refreshSoon() } } }
    var reverse = false { didSet { if reverse != oldValue { refreshSoon() } } }
    var tagFilter = "" { didSet { if tagFilter != oldValue { refreshSoon() } } }
    var staleOnly = false { didSet { if staleOnly != oldValue { refreshSoon() } } }
    var preOnly = false { didSet { if preOnly != oldValue { refreshSoon() } } }
    var liveOnly = false { didSet { if liveOnly != oldValue { refreshSoon() } } }
    var forksOnly = false { didSet { if forksOnly != oldValue { refreshSoon() } } }

    // Ask / deep search results replace the list until cleared.
    private(set) var hits: [Hit]?
    private(set) var hitKind = ""
    private(set) var hitQuery = ""
    var searching = false

    // Sheets and drafts live here rather than in @State: that macro's plugin ships only with Xcode.
    var editingTags = false
    var confirmDelete = false
    var asking: AskMode?
    var tagDraft = ""
    var askDraft = ""

    enum AskMode: String, Identifiable {
        case ask, deep
        var id: String { rawValue }
        var title: String { self == .ask ? "Ask Claude" : "Deep search" }
        var hint: String {
            self == .ask ? "Describe the session you are looking for" : "Words that appear anywhere in the transcripts"
        }
    }

    // Interaction state for the brief. Kept here because @State is unavailable without Xcode.
    var hover: String?
    var collapsed = Set<String>()

    func setHover(_ key: String, _ on: Bool) {
        if on { hover = key } else if hover == key { hover = nil }
    }

    func toggle(_ section: String) {
        if collapsed.contains(section) { collapsed.remove(section) } else { collapsed.insert(section) }
    }

    func copy(_ text: String, note: String = "copied") {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        say("\(note): \(text.count > 60 ? String(text.prefix(60)) + "…" : text)")
    }

    /// A brief names files loosely ("client.go", "~/x/y", "make suites"). Only treat it as a path if it exists.
    func resolve(_ f: String, cwd: String) -> URL? {
        let p = (f as NSString).expandingTildeInPath
        let url = p.hasPrefix("/") ? URL(fileURLWithPath: p) : URL(fileURLWithPath: cwd).appendingPathComponent(p)
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    /// Code and config files open in Zed, not whatever app owns the extension. Folders keep the default (Finder).
    func openFile(_ f: String, cwd: String) {
        guard let u = resolve(f, cwd: cwd) else { copy(f); return }
        var isDir: ObjCBool = false
        FileManager.default.fileExists(atPath: u.path, isDirectory: &isDir)
        let zed = URL(fileURLWithPath: "/Applications/Zed.app")
        if !isDir.boolValue, FileManager.default.fileExists(atPath: zed.path) {
            NSWorkspace.shared.open([u], withApplicationAt: zed, configuration: NSWorkspace.OpenConfiguration())
        } else {
            NSWorkspace.shared.open(u)
        }
    }

    func reveal(_ u: URL) { NSWorkspace.shared.activateFileViewerSelecting([u]) }

    // Brief popup state for the selected session.
    var briefID: String?
    var briefText = ""
    var briefData: BriefData?
    var briefBusy = false
    private(set) var resumes: [String: String] = [:] // detail-only field, fetched per selection

    private var pending: Task<Void, Never>?
    private var flashTask: Task<Void, Never>?
    private var events: Task<Void, Never>?
    private let api = Daemon.api

    var rows: [Session] {
        guard let hits else { return fetched }
        let byID = Dictionary(fetched.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        return hits.compactMap { byID[$0.id] }
    }

    func why(_ id: String) -> String? { hits?.first { $0.id == id }?.why }

    var live: [Session] { fetched.filter { $0.live != nil } }
    var busyCount: Int { fetched.filter { $0.live?.status == "busy" }.count }

    var single: Session? {
        guard selection.count == 1, let id = selection.first else { return nil }
        return rows.first { $0.id == id }
    }

    var selected: [Session] { rows.filter { selection.contains($0.id) } }

    // MARK: lifecycle

    func start() {
        guard events == nil else { return }
        events = Task { [weak self] in
            while !Task.isCancelled {
                let err = await Task.detached { Daemon.ensureRunning() }.value
                guard let self else { return }
                if let err {
                    self.conn = .down(err)
                    try? await Task.sleep(for: .seconds(3))
                    continue
                }
                await self.refresh()
                let http = UnixHTTP(path: Daemon.sock)
                let me = self
                await Task.detached {
                    try? http.events("/events") { _ in Task { @MainActor in await me.refresh() } }
                }.value
                self.conn = .down("lost the connection to ccsd")
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }

    func refreshSoon(after delay: Double = 0) {
        pending?.cancel()
        pending = Task { [weak self] in
            if delay > 0 { try? await Task.sleep(for: .seconds(delay)) }
            if Task.isCancelled { return }
            await self?.refresh()
        }
    }

    private func queryItems() -> [URLQueryItem] {
        var q: [URLQueryItem] = [.init(name: "sort", value: sort)]
        func add(_ k: String, _ v: String) { if !v.isEmpty { q.append(.init(name: k, value: v)) } }
        // While ask/deep results are shown, fetch the whole list and let the hits pick from it.
        if hits == nil {
            add("q", query)
            add("tag", tagFilter)
            if staleOnly { add("stale", "1") }
            if preOnly { add("pre", "1") }
            if liveOnly { add("live", "1") }
            if forksOnly { add("forks", "1") }
        }
        if reverse { add("rev", "1") }
        return q
    }

    func refresh() async {
        let api = self.api
        let items = queryItems()
        do {
            let (s, t) = try await Task.detached { (try api.sessions(items), try api.tags()) }.value
            fetched = s
            tagCounts = t
            conn = .ok
            let ids = Set(rows.map(\.id))
            selection.formIntersection(ids)
        } catch {
            conn = .down(error.localizedDescription)
        }
    }

    // MARK: messages

    func say(_ text: String) {
        flash = text
        flashTask?.cancel()
        flashTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(4))
            if !Task.isCancelled { self?.flash = "" }
        }
    }

    /// Runs a ccsd call off the main actor, reports failures, and refetches when asked.
    private func run<T: Sendable>(_ what: String, refetch: Bool = true, _ call: @escaping @Sendable (API) throws -> T) async -> T? {
        let api = self.api
        do {
            let v = try await Task.detached { try call(api) }.value
            if refetch { await refresh() }
            return v
        } catch {
            say("\(what) failed: \(error.localizedDescription)")
            return nil
        }
    }

    // MARK: actions

    func pin(_ ids: [String]) async {
        for id in ids { _ = await run("pin", refetch: false) { try $0.pin(id) } }
        await refresh()
    }

    func setTags(_ id: String, _ tags: [String]) async {
        if await run("tags", { try $0.setTags(id, tags) }) != nil { say("tags saved") }
    }

    func open(_ ids: [String], fork: Bool, tab: Bool = false) async {
        guard let r = await run("open", refetch: false, { try $0.open(ids, tab: tab, fork: fork) }) else { return }
        if let e = r.error { say(e) } else if r.skipped > 0 { say("opened \(r.opened), skipped \(r.skipped) (directory gone or no transcript)") } else {
            say("opened \(r.opened) in your terminal")
        }
    }

    func delete(_ ids: [String]) async {
        guard let r = await run("delete", { try $0.delete(ids) }) else { return }
        say(r.error ?? "moved \(r.moved) to Trash" + (r.skipped > 0 ? " (skipped \(r.skipped) running)" : "") + ". Undo is in the toolbar")
    }

    func undo() async {
        guard let r = await run("undo", { try $0.undo() }) else { return }
        say(r.error ?? "restored \(r.restored)")
    }

    func summarize(_ id: String) async {
        busy.insert(id)
        defer { busy.remove(id) }
        if await run("summary", { try $0.summarize(id) }) != nil { say("summary saved") }
    }

    func autoTag(_ ids: [String]) async {
        let ids = Array(ids.prefix(20))
        for id in ids { busy.insert(id) }
        defer { for id in ids { busy.remove(id) } }
        if await run("auto-tag", { try $0.autoTag(ids) }) != nil { say("tagged \(ids.count) session(s)") }
    }

    func loadResume(_ id: String) async {
        if let r = await run("detail", refetch: false, { try $0.session(id) }), let cmd = r.resume { resumes[id] = cmd }
    }

    /// Shows a brief that is already cached for this session. Never calls the model.
    func showCachedBrief(_ id: String) async {
        guard briefID != id, !briefBusy else { return }
        if let r = await run("brief", refetch: false, { try $0.brief(id, force: false, cachedOnly: true) }), r.data != nil || !r.brief.isEmpty {
            briefID = id
            briefText = r.brief
            briefData = r.data
        }
    }

    func loadBrief(_ id: String, force: Bool = false) async {
        briefID = id
        briefBusy = true
        briefText = ""
        briefData = nil
        defer { briefBusy = false }
        if let r = await run("brief", refetch: false, { try $0.brief(id, force: force) }), briefID == id {
            briefText = r.brief
            briefData = r.data
        } else if briefID == id {
            briefID = nil
        }
    }

    func ask(_ text: String, deep: Bool) async {
        let q = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !q.isEmpty else { return }
        searching = true
        defer { searching = false }
        guard let h = await run(deep ? "deep search" : "ask", refetch: false, { deep ? try $0.deep(q) : try $0.ask(q) }) else { return }
        if h.isEmpty { say("no matching session found"); return }
        hits = h
        hitKind = deep ? "deep" : "ask"
        hitQuery = q
        selection = []
        await refresh()
        say("\(h.count) matches")
    }

    func clearHits() {
        guard hits != nil else { return }
        hits = nil
        refreshSoon()
    }

    func clearFilters() {
        query = ""; tagFilter = ""
        staleOnly = false; preOnly = false; liveOnly = false; forksOnly = false
        clearHits()
    }

    var filtersActive: Bool { !tagFilter.isEmpty || staleOnly || preOnly || liveOnly || forksOnly }
}
