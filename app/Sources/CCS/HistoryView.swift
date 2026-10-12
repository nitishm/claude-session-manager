import SwiftUI

/// Everything that happened in a session, oldest first: prompts with their turn stats, commits, pushes, PRs,
/// compactions and pauses. It shows the latest few and expands to all. No model call is involved.
struct HistoryView: View {
    @Bindable var store: Store
    let events: [TLEvent]
    var embedded = false // inside the brief's Timeline fold, which already has a title

    private static let recent = 12
    private var all: Bool { store.collapsed.contains("history:all") }
    private var shown: [TLEvent] { all || events.count <= Self.recent ? events : Array(events.suffix(Self.recent)) }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                if !embedded {
                    Text("History").font(.headline)
                    Text("\(events.count) events").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                if events.count > Self.recent {
                    Button(all ? "Show latest" : "Show all \(events.count)") { store.toggle("history:all") }
                        .buttonStyle(.link).font(.caption).clickable("history:toggle")
                }
            }
            if !all, events.count > Self.recent {
                Text("\(events.count - Self.recent) earlier events hidden").font(.caption).foregroundStyle(.secondary)
            }
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(shown.enumerated()), id: \.element.id) { i, e in
                    if i == 0 || !Calendar.current.isDate(e.at, inSameDayAs: shown[i - 1].at) {
                        Text(e.at.formatted(.dateTime.weekday(.wide).day().month(.abbreviated)))
                            .font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                            .padding(.top, i == 0 ? 0 : 10).padding(.bottom, 4)
                    }
                    row(e)
                }
            }
        }
    }

    private func row(_ e: TLEvent) -> some View {
        let (symbol, color) = Self.style(e.kind)
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(e.at.formatted(date: .omitted, time: .shortened)).font(.caption.monospacedDigit()).foregroundStyle(.secondary).frame(width: 62, alignment: .trailing)
            Image(systemName: symbol).font(.system(size: 10)).foregroundStyle(color).frame(width: 14)
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 6) {
                    Text(e.text).font(.callout).foregroundStyle(e.kind == "prompt" ? Color.primary : Color.secondary)
                        .fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                    if let n = e.n, e.kind == "prompt" { Text("#\(n)").font(.caption2).foregroundStyle(.tertiary) }
                }
                if let line = stats(e) { Text(line).font(.caption).foregroundStyle(.secondary) }
                if let d = e.detail, !d.isEmpty { detail(e, d) }
            }
        }
        .padding(.vertical, 3)
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
