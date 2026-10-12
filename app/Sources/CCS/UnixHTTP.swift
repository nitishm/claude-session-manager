import Foundation
import Darwin

// URLSession cannot talk to a unix socket, so this is a tiny HTTP/1.0 client over one.
// HTTP/1.0 matters: Go's server then sends no chunked encoding and closes the connection at the
// end of the body, so a response is "read until EOF" and an event stream is just lines.

enum CCSError: Error, LocalizedError {
    case connect(String)
    case io(String)
    case status(Int, String)
    case decode(String)

    var errorDescription: String? {
        switch self {
        case .connect(let m): return "cannot reach ccsd: \(m)"
        case .io(let m): return "ccsd I/O error: \(m)"
        case .status(_, let m): return m
        case .decode(let m): return "unexpected reply: \(m)"
        }
    }
}

struct UnixHTTP: Sendable {
    let path: String

    private func open(timeout: Int) throws -> Int32 {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw CCSError.connect("socket: \(String(cString: strerror(errno)))") }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(path.utf8) + [0]
        let cap = MemoryLayout.size(ofValue: addr.sun_path)
        guard bytes.count <= cap else {
            close(fd)
            throw CCSError.connect("socket path too long (\(bytes.count) > \(cap))")
        }
        withUnsafeMutableBytes(of: &addr.sun_path) { $0.copyBytes(from: bytes) }
        let len = socklen_t(MemoryLayout<sockaddr_un>.size)
        let rc = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(fd, $0, len) }
        }
        guard rc == 0 else {
            let msg = String(cString: strerror(errno))
            close(fd)
            throw CCSError.connect(msg)
        }
        var tv = timeval(tv_sec: timeout, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        var one: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &one, socklen_t(MemoryLayout<Int32>.size))
        return fd
    }

    private func send(_ fd: Int32, _ method: String, _ target: String, _ body: Data?) throws {
        var head = "\(method) \(target) HTTP/1.0\r\nHost: ccsd\r\nAccept: application/json\r\n"
        if let body { head += "Content-Type: application/json\r\nContent-Length: \(body.count)\r\n" }
        head += "\r\n"
        var out = Data(head.utf8)
        if let body { out.append(body) }
        try out.withUnsafeBytes { raw in
            var off = 0
            while off < raw.count {
                let n = write(fd, raw.baseAddress! + off, raw.count - off)
                if n <= 0 { throw CCSError.io("write: \(String(cString: strerror(errno)))") }
                off += n
            }
        }
    }

    /// One request. AI calls can take a minute or more, hence the long default timeout.
    func request(_ method: String, _ target: String, body: Data? = nil, timeout: Int = 150) throws -> (status: Int, body: Data) {
        let fd = try open(timeout: timeout)
        defer { close(fd) }
        try send(fd, method, target, body)
        var all = Data()
        var buf = [UInt8](repeating: 0, count: 64 * 1024)
        while true {
            let n = read(fd, &buf, buf.count)
            if n < 0 { throw CCSError.io("read: \(String(cString: strerror(errno)))") }
            if n == 0 { break }
            all.append(contentsOf: buf[0..<n])
        }
        return try Self.split(all)
    }

    static func split(_ all: Data) throws -> (status: Int, body: Data) {
        let sep = Data("\r\n\r\n".utf8)
        guard let r = all.range(of: sep) else { throw CCSError.io("truncated response") }
        let head = String(decoding: all[..<r.lowerBound], as: UTF8.self)
        let parts = head.split(separator: " ", maxSplits: 2)
        guard parts.count >= 2, let code = Int(parts[1]) else { throw CCSError.io("bad status line") }
        return (code, Data(all[r.upperBound...]))
    }

    /// Reads a server-sent event stream and calls onEvent with each event name ("sessions", "live").
    /// Returns when the connection drops or the read times out (the server pings every 25s).
    func events(_ target: String, onEvent: (String) -> Void) throws {
        let fd = try open(timeout: 70)
        defer { close(fd) }
        try send(fd, "GET", target, nil)
        var pending = Data()
        var buf = [UInt8](repeating: 0, count: 4096)
        var inBody = false
        while !Task.isCancelled {
            let n = read(fd, &buf, buf.count)
            if n <= 0 { return }
            pending.append(contentsOf: buf[0..<n])
            while let nl = pending.firstIndex(of: 0x0A) {
                let line = String(decoding: pending[..<nl], as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
                pending.removeSubrange(...nl)
                if !inBody {
                    if line.isEmpty { inBody = true }
                    continue
                }
                if line.hasPrefix("event: ") { onEvent(String(line.dropFirst(7))) }
            }
        }
    }
}

/// Typed calls over UnixHTTP. All blocking; call them off the main actor (the Store does).
struct API: Sendable {
    let http: UnixHTTP

    private func decode<T: Decodable>(_ r: (status: Int, body: Data), _ type: T.Type) throws -> T {
        guard (200..<300).contains(r.status) else {
            let msg = (try? JSONDecoder().decode([String: String].self, from: r.body))?["error"] ?? "HTTP \(r.status)"
            throw CCSError.status(r.status, msg)
        }
        // Go encodes an empty nil slice as null; a list endpoint with no results means [].
        if T.self is any RangeReplaceableCollection.Type, String(decoding: r.body, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines) == "null",
           let empty = try? JSONDecoder().decode(T.self, from: Data("[]".utf8)) { return empty }
        do { return try JSONDecoder.ccsd.decode(T.self, from: r.body) } catch { throw CCSError.decode("\(error)") }
    }

    private func json(_ v: some Encodable) -> Data { (try? JSONEncoder().encode(v)) ?? Data() }

    func health() throws -> Health { try decode(http.request("GET", "/health", timeout: 3), Health.self) }

    func sessions(_ q: [URLQueryItem]) throws -> [Session] {
        var c = URLComponents()
        c.queryItems = q.isEmpty ? nil : q
        return try decode(http.request("GET", "/sessions" + (c.percentEncodedQuery.map { "?" + $0 } ?? "")), [Session].self)
    }

    func session(_ id: String) throws -> Session { try decode(http.request("GET", "/sessions/\(id)"), Session.self) }
    func tags() throws -> [TagCount] { try decode(http.request("GET", "/tags"), [TagCount].self) }

    func pin(_ id: String) throws { _ = try decode(http.request("POST", "/sessions/\(id)/pin"), [String: Bool].self) }

    func setTags(_ id: String, _ tags: [String]) throws {
        _ = try decode(http.request("PUT", "/sessions/\(id)/tags", body: json(["tags": tags])), [String: [String]].self)
    }

    struct IDs: Encodable { let ids: [String] }
    struct OpenReq: Encodable { let ids: [String]; let tab: Bool; let fork: Bool }

    func delete(_ ids: [String]) throws -> Deleted { try decode(http.request("POST", "/delete", body: json(IDs(ids: ids))), Deleted.self) }
    func undo() throws -> Restored { try decode(http.request("POST", "/undo"), Restored.self) }

    func open(_ ids: [String], tab: Bool, fork: Bool) throws -> Opened {
        try decode(http.request("POST", "/open", body: json(OpenReq(ids: ids, tab: tab, fork: fork))), Opened.self)
    }

    struct BriefReply: Decodable { let brief: String; let cached: Bool; let data: BriefData? }
    func brief(_ id: String, force: Bool) throws -> BriefReply {
        try decode(http.request("POST", "/ai/brief", body: json(["id": AnyEncodable(id), "force": AnyEncodable(force)])), BriefReply.self)
    }

    func summarize(_ id: String) throws -> String {
        try decode(http.request("POST", "/ai/summarize", body: json(["id": id])), [String: String].self)["summary"] ?? ""
    }

    func ask(_ query: String) throws -> [Hit] { try decode(http.request("POST", "/ai/ask", body: json(["query": query])), [Hit].self) }
    func deep(_ query: String) throws -> [Hit] { try decode(http.request("POST", "/ai/deep", body: json(["query": query])), [Hit].self) }

    func autoTag(_ ids: [String]) throws { _ = try decode(http.request("POST", "/ai/autotag", body: json(IDs(ids: ids))), [String: [String]].self) }
}

struct AnyEncodable: Encodable {
    let value: any Encodable
    init(_ v: any Encodable) { value = v }
    func encode(to encoder: Encoder) throws { try value.encode(to: encoder) }
}
