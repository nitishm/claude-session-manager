import Foundation

// These mirror ccsd's JSON (see SessionView in go/cmd/ccsd/server.go). The keys are a contract:
// ccsd adds fields but never renames them, so an unknown field here is ignored.

struct Turn: Codable, Hashable {
    let user: Bool
    let text: String
}

struct Live: Codable, Hashable {
    let pid: Int
    let id: String
    let cwd: String
    let name: String
    let status: String   // idle, busy, or anything else = waiting on you
    let updatedAt: Int64 // unix millis
    let kind: String
}

/// Go encodes a nil slice as null. Treat null or a missing key as an empty array.
@propertyWrapper
struct NullAsEmpty<T: Codable & Hashable>: Codable, Hashable {
    var wrappedValue: [T]
    init(wrappedValue: [T]) { self.wrappedValue = wrappedValue }
    init(from d: Decoder) throws { wrappedValue = (try? [T](from: d)) ?? [] }
    func encode(to e: Encoder) throws { try wrappedValue.encode(to: e) }
}

extension KeyedDecodingContainer {
    func decode<T>(_ t: NullAsEmpty<T>.Type, forKey k: Key) throws -> NullAsEmpty<T> {
        try decodeIfPresent(t, forKey: k) ?? NullAsEmpty(wrappedValue: [])
    }
}

struct Session: Codable, Identifiable, Hashable {
    let id: String
    let path: String
    let cwd: String
    let dir: String
    let label: String
    let title: String
    let aiTitle: String
    let first: String
    let last: String
    let pr: String
    let summary: String
    let excerpt: String
    let prompts: Int
    let mtime: Date
    let started: Date
    @NullAsEmpty var turns: [Turn]
    @NullAsEmpty var tags: [String]
    let pinned: Bool
    let live: Live?
    let preBoot: Bool
    let stale: Bool
    let dirGone: Bool
    let repo: String
    let fork: Bool
    let forkOf: String?
    let forks: Int
    let stub: Bool
    let resume: String?

    /// The title without the "[fork] " prefix ccs adds, since the row already shows a fork icon.
    var displayLabel: String {
        var l = label
        for p in ["[fork] ", "⑂ "] where l.hasPrefix(p) { l.removeFirst(p.count) }
        return l
    }
}

struct TagCount: Codable, Hashable {
    let name: String
    let n: Int
}

struct Hit: Codable, Hashable {
    let id: String
    let why: String
}

struct Opened: Codable {
    let opened: Int
    let skipped: Int
    let error: String?
}

struct Deleted: Codable {
    let moved: Int
    let skipped: Int
    let error: String?
}

struct Restored: Codable {
    let restored: Int
    let error: String?
}

struct Health: Codable {
    let ok: Bool
    let sessions: Int
    let live: Int
}

extension JSONDecoder {
    /// Go writes RFC 3339 with up to nanosecond fractions; Foundation parses at most milliseconds.
    static let ccsd: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            if let date = parseGoTime(s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: "bad time \(s)"))
        }
        return d
    }()
}

func parseGoTime(_ raw: String) -> Date? {
    var s = raw
    if let dot = s.firstIndex(of: ".") {
        var end = s.index(after: dot)
        while end < s.endIndex, s[end].isNumber { end = s.index(after: end) }
        let frac = String(s[s.index(after: dot)..<end].prefix(3))
        s = String(s[..<dot]) + "." + frac + String(s[end...])
    }
    if let d = goTimeFrac.date(from: s) { return d }
    return goTimePlain.date(from: raw) ?? goTimePlain.date(from: String(s.prefix(19)) + "Z")
}

// Building an ISO8601DateFormatter is expensive; one per call made every
// list refresh stall the UI. Parsing with a shared instance is thread-safe.
private let goTimeFrac: ISO8601DateFormatter = {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return f
}()
private let goTimePlain: ISO8601DateFormatter = {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime]
    return f
}()

/// Short age like the TUI: now, 5m, 3h, 12d.
func ago(_ d: Date, now: Date = Date()) -> String {
    let s = now.timeIntervalSince(d)
    switch s {
    case 86400...: return "\(Int(s / 86400))d"
    case 3600...: return "\(Int(s / 3600))h"
    case 60...: return "\(Int(s / 60))m"
    default: return "now"
    }
}

/// The structured brief ccsd returns in `data`. Null when the cached brief predates the format.
struct BriefData: Codable, Hashable {
    /// kind is research, diagnose, build, fix, test, decide, blocked, ship or setup; outcome is the result in a few words.
    struct Step: Codable, Hashable { let text: String; let state: String; let kind: String?; let outcome: String? }
    struct Event: Codable, Hashable { let when: String; let text: String; let kind: String?; let outcome: String? }
    let goal: String
    let status: String
    let statusNote: String
    let next: String
    @NullAsEmpty var steps: [Step]
    @NullAsEmpty var timeline: [Event]
    @NullAsEmpty var files: [String]

    var progress: (done: Int, total: Int) { (steps.filter { $0.state == "done" }.count, steps.count) }
}

/// Numbers read from a transcript (GET /sessions/{id}/facts). No model call is behind them.
struct FileTouch: Codable, Hashable {
    let path: String
    let rel: String
    let edits: Int
    let reads: Int
}

struct Facts: Codable, Hashable {
    let model: String
    let turns: Int
    let `in`: Int64
    let out: Int64
    let cacheRead: Int64
    let cacheWrite: Int64
    let activeMin: Int
    @NullAsEmpty var files: [FileTouch]
    @NullAsEmpty var commands: [String]
    let commandCount: Int

    var cacheHit: Int {
        let tot = `in` + cacheRead + cacheWrite
        return tot > 0 ? Int(cacheRead * 100 / tot) : 0
    }
}

func compact(_ n: Int64) -> String {
    if n >= 1_000_000 { return String(format: "%.1fM", Double(n) / 1e6) }
    if n >= 1000 { return String(format: "%.1fk", Double(n) / 1e3) }
    return "\(n)"
}

func duration(_ min: Int) -> String {
    if min < 60 { return "\(min)m" }
    return min % 60 == 0 ? "\(min / 60)h" : "\(min / 60)h \(min % 60)m"
}

struct PRState: Codable, Hashable {
    let state: String
    let draft: Bool
    let review: String
    let checks: String
    let passed: Int
    let failed: Int
    let pending: Int
    let title: String
}

/// Live working-copy and PR state from `GET /sessions/{id}/repo`. Slow, so it loads apart from the session.
struct RepoState: Codable, Hashable {
    let kind: String
    let branch: String
    let dirty: Int
    let ahead: Int
    let behind: Int
    let pr: PRState?
    let err: String?
}

/// One thing that happened in a session, from `GET /sessions/{id}/timeline`.
struct TLEvent: Codable, Hashable, Identifiable {
    let at: Date
    let kind: String
    let text: String
    let detail: String?
    let n: Int?
    let secs: Int?
    let tools: Int?
    let edits: Int?
    let errors: Int?
    var id: String { "\(at.timeIntervalSince1970)|\(kind)|\(text)" }
}

/// A pull request the session linked, with its live state from gh (nil when gh could not read it).
struct PRInfo: Codable, Hashable, Identifiable {
    let number: Int
    let url: String
    let at: Date
    let pr: PRState?
    let err: String?
    var id: String { url.isEmpty ? "#\(number)" : url }
}

/// A page Claude published during the session.
struct Artifact: Codable, Hashable, Identifiable {
    let title: String
    let url: String
    let file: String?
    let at: Date
    var id: String { url }
}

struct Timeline: Codable, Hashable {
    @NullAsEmpty var events: [TLEvent]
    @NullAsEmpty var artifacts: [Artifact]
}
