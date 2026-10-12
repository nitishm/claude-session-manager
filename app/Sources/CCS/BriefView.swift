import SwiftUI

/// A structured brief: header card, kanban columns, timeline. Stateless, so it needs no @State.
struct BriefView: View {
    let data: BriefData

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            card
            if !data.steps.isEmpty { kanban }
            if !data.timeline.isEmpty { timeline }
        }
    }

    // MARK: colors

    private var statusColor: Color {
        switch data.status {
        case "blocked": return .orange
        case "done": return .green
        case "idle": return .secondary
        default: return .blue
        }
    }

    private var statusIcon: String {
        switch data.status {
        case "blocked": return "exclamationmark.triangle.fill"
        case "done": return "checkmark.circle.fill"
        case "idle": return "circle"
        default: return "circle.lefthalf.filled"
        }
    }

    // MARK: 1. card

    private var card: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(data.goal).font(.title3.bold()).textSelection(.enabled)
            if !data.steps.isEmpty {
                let p = data.progress
                HStack(spacing: 10) {
                    ProgressView(value: Double(p.done), total: Double(max(p.total, 1))).tint(.green).frame(maxWidth: 240)
                    Text("\(p.done) of \(p.total) steps").font(.caption).foregroundStyle(.secondary)
                }
            }
            row("State") {
                HStack(spacing: 6) {
                    Label(data.status, systemImage: statusIcon).foregroundStyle(statusColor).font(.callout.bold())
                    if !data.statusNote.isEmpty { Text(data.statusNote).foregroundStyle(.secondary) }
                }
            }
            if !data.next.isEmpty {
                row("Next") { Label(data.next, systemImage: "arrow.right").foregroundStyle(Color.accentColor) }
            }
            if !data.files.isEmpty {
                row("Files") {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 120), spacing: 6, alignment: .leading)], alignment: .leading, spacing: 6) {
                        ForEach(data.files, id: \.self) { f in
                            Text(f).font(.system(.caption, design: .monospaced)).lineLimit(1)
                                .padding(.horizontal, 7).padding(.vertical, 2).background(.quaternary, in: Capsule())
                        }
                    }
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 10))
    }

    private func row<C: View>(_ label: String, @ViewBuilder _ content: () -> C) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(label.uppercased()).font(.caption2.bold()).foregroundStyle(.secondary).frame(width: 44, alignment: .leading)
            content()
        }
    }

    // MARK: 2. kanban

    private var kanban: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Steps").font(.headline)
            HStack(alignment: .top, spacing: 10) {
                column("Done", "done", "checkmark.circle.fill", .green)
                column("Doing", "doing", "circle.lefthalf.filled", .blue)
                column("Next", "todo", "circle", .secondary)
            }
        }
    }

    private func column(_ title: String, _ state: String, _ icon: String, _ color: Color) -> some View {
        let items = data.steps.filter { $0.state == state }
        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(title).font(.caption.bold()).foregroundStyle(color)
                Spacer()
                Text("\(items.count)").font(.caption).foregroundStyle(.secondary)
            }
            if items.isEmpty {
                Text("—").foregroundStyle(.tertiary)
            }
            ForEach(items, id: \.self) { s in
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: icon).foregroundStyle(color).font(.caption)
                    Text(s.text).font(.callout).fixedSize(horizontal: false, vertical: true)
                }
                .padding(8).frame(maxWidth: .infinity, alignment: .leading)
                .background(.background, in: RoundedRectangle(cornerRadius: 6))
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background(color.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(color.opacity(0.35)))
    }

    // MARK: 3. timeline

    private var timeline: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Timeline").font(.headline)
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(data.timeline.enumerated()), id: \.offset) { i, ev in
                    let last = i == data.timeline.count - 1
                    event(ev, last: last, showLine: !last || (!data.next.isEmpty && data.status != "done"), here: last && data.status != "done")
                }
                if !data.next.isEmpty, data.status != "done" {
                    HStack(alignment: .top, spacing: 12) {
                        Text("next").font(.caption).foregroundStyle(.secondary).frame(width: 52, alignment: .trailing)
                        Image(systemName: "circle").foregroundStyle(.secondary).font(.caption).frame(width: 14)
                        Text(data.next).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private func event(_ ev: BriefData.Event, last: Bool, showLine: Bool, here: Bool) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Text(ev.when).font(.caption).foregroundStyle(.secondary).frame(width: 52, alignment: .trailing).padding(.top, 1)
            VStack(spacing: 0) {
                Image(systemName: here ? "circle.lefthalf.filled" : "circle.fill").font(.caption)
                    .foregroundStyle(here ? statusColor : Color.green).frame(width: 14)
                if showLine { Rectangle().fill(.quaternary).frame(width: 2).frame(maxHeight: .infinity) }
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(ev.text)
                if here { Text("you are here").font(.caption).foregroundStyle(statusColor) }
            }
            .padding(.bottom, 12)
            Spacer(minLength: 0)
        }
    }
}
