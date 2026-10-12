import SwiftUI

/// Everything that happened in a session, oldest first, in a fixed-height terminal-style window that scrolls.
/// It opens at the newest event. No model call is involved.
struct HistoryView: View {
    @Bindable var store: Store
    let events: [TLEvent]
    var embedded = false // inside the brief's Timeline fold, which already has a title

    private static let bg = Color(red: 0.08, green: 0.09, blue: 0.11)
    private static let fg = Color(red: 0.85, green: 0.87, blue: 0.9)
    private static let dim = Color(red: 0.5, green: 0.53, blue: 0.58)

    var body: some View {
        VStack(spacing: 0) {
            chrome
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(events.enumerated()), id: \.element.id) { i, e in
                            if i == 0 || !Calendar.current.isDate(e.at, inSameDayAs: events[i - 1].at) {
                                Text("── " + e.at.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated)))
                                    .foregroundStyle(Self.dim).padding(.top, i == 0 ? 0 : 8).padding(.bottom, 2)
                            }
                            row(e).id(e.id)
                        }
                    }
                    .font(.system(size: 12, design: .monospaced))
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(height: 280)
                .onAppear { if let last = events.last { proxy.scrollTo(last.id, anchor: .bottom) } }
                .onChange(of: events.count) { _, _ in if let last = events.last { proxy.scrollTo(last.id, anchor: .bottom) } }
            }
        }
        .background(Self.bg)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(Color.white.opacity(0.12)))
        .environment(\.colorScheme, .dark)
    }

    private var chrome: some View {
        HStack(spacing: 6) {
            ForEach([Color.red, .yellow, .green], id: \.self) { Circle().fill($0.opacity(0.8)).frame(width: 10, height: 10) }
            Spacer()
            Text("timeline · \(events.count) events").font(.system(size: 11, design: .monospaced)).foregroundStyle(Self.dim)
            Spacer()
        }
        .padding(.horizontal, 10).padding(.vertical, 7)
        .background(Color.white.opacity(0.06))
    }

    private func row(_ e: TLEvent) -> some View {
        let (symbol, color) = Self.style(e.kind)
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(e.at.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute())).foregroundStyle(Self.dim)
            Image(systemName: symbol).font(.system(size: 10)).foregroundStyle(color).frame(width: 14)
            VStack(alignment: .leading, spacing: 1) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(e.text).foregroundStyle(e.kind == "prompt" ? Self.fg : Self.dim)
                        .fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                    if let n = e.n, e.kind == "prompt" { Text("#\(n)").font(.system(size: 10, design: .monospaced)).foregroundStyle(Self.dim.opacity(0.7)) }
                }
                if let line = stats(e) { Text(line).font(.system(size: 11, design: .monospaced)).foregroundStyle(Self.dim) }
                if let d = e.detail, !d.isEmpty { detail(e, d) }
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder private func detail(_ e: TLEvent, _ d: String) -> some View {
        if d.hasPrefix("https://"), let u = URL(string: d) {
            Link(d, destination: u).font(.caption).clickable()
        } else {
            Text(d).font(.caption).foregroundStyle(.secondary)
        }
    }

    private func stats(_ e: TLEvent) -> String? {
        guard e.kind == "prompt" else { return nil }
        var bits: [String] = []
        if let t = e.tools, t > 0 { bits.append("\(t) tools") }
        if let x = e.edits, x > 0 { bits.append("\(x) edits") }
        if let s = e.secs, s > 0 { bits.append(s < 60 ? "\(s)s" : duration(s / 60)) }
        if let r = e.errors, r > 0 { bits.append("\(r) failed") }
        return bits.isEmpty ? nil : bits.joined(separator: " · ")
    }

    static func style(_ kind: String) -> (String, Color) {
        switch kind {
        case "prompt": return ("text.bubble", .accentColor)
        case "commit": return ("checkmark.circle.fill", .green)
        case "push": return ("arrow.up.circle.fill", .green)
        case "pr": return ("arrow.triangle.pull", .purple)
        case "compact": return ("arrow.down.right.and.arrow.up.left", .orange)
        case "away": return ("moon.zzz", .indigo)
        case "title": return ("pencil", .secondary)
        case "agent": return ("person.2", .teal)
        case "ask": return ("questionmark.bubble", .blue)
        case "resume": return ("arrow.counterclockwise", .secondary)
        case "gap": return ("pause.circle", .secondary)
        case "schedule": return ("clock.arrow.circlepath", .secondary)
        case "artifact": return ("square.on.square", .secondary)
        case "api-error": return ("exclamationmark.triangle", .orange)
        case "interrupt": return ("hand.raised", .orange)
        default: return ("circle", .secondary)
        }
    }
}
