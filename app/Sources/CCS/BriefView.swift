import SwiftUI
import AppKit

/// A structured brief: header card, kanban columns, timeline. Everything on it is clickable:
/// files open or reveal, steps and events copy, Next resumes, sections collapse.
/// State lives in the Store because @State needs Xcode's macro plugin.
struct BriefView: View {
    @Bindable var store: Store
    let session: Session
    let data: BriefData

    private var canResume: Bool { !session.dirGone && !session.stub }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            card
            if !data.steps.isEmpty { fold("steps", "Steps", "\(data.progress.done)/\(data.steps.count) done") { kanban } }
            if !data.timeline.isEmpty { fold("timeline", "Timeline", "\(data.timeline.count) events") { timeline } }
        }
    }

    // MARK: shared bits

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

    private func hovered(_ key: String) -> Bool { store.hover == key }

    /// A collapsible section. Click the header to fold it.
    private func fold<C: View>(_ id: String, _ title: String, _ note: String, @ViewBuilder _ content: () -> C) -> some View {
        let open = !store.collapsed.contains(id)
        return VStack(alignment: .leading, spacing: 8) {
            Button { withAnimation(.easeInOut(duration: 0.15)) { store.toggle(id) } } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right").font(.caption.bold()).rotationEffect(.degrees(open ? 90 : 0)).frame(width: 12)
                    Text(title).font(.headline)
                    Text(note).font(.caption).foregroundStyle(.secondary)
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain).help(open ? "Collapse" : "Expand")
            if open { content() }
        }
    }

    private func pill(_ icon: String, _ text: String, key: String, tint: Color = .secondary, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Label(text, systemImage: icon).font(.caption).foregroundStyle(hovered(key) ? Color.accentColor : tint)
                .padding(.horizontal, 8).padding(.vertical, 3)
                .background(hovered(key) ? Color.accentColor.opacity(0.18) : Color.clear, in: Capsule())
                .background(.quaternary, in: Capsule())
        }
        .buttonStyle(.plain).onHover { store.setHover(key, $0) }
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
                row("Next") {
                    VStack(alignment: .leading, spacing: 6) {
                        Label(data.next, systemImage: "arrow.right").foregroundStyle(Color.accentColor).textSelection(.enabled)
                        HStack(spacing: 8) {
                            Button { Task { await store.open([session.id], fork: false) } } label: { Label("Resume", systemImage: "play.fill") }
                                .controlSize(.small).disabled(!canResume).help("Resume this session in your terminal")
                            Button { store.copy(data.next, note: "copied next step") } label: { Label("Copy", systemImage: "doc.on.doc") }
                                .controlSize(.small).help("Copy the next step, to paste as your first prompt")
                        }
                    }
                }
            }
            if !data.files.isEmpty {
                row("Files") {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 130), spacing: 6, alignment: .leading)], alignment: .leading, spacing: 6) {
                        ForEach(data.files, id: \.self) { f in file(f) }
                    }
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 10))
    }

    @ViewBuilder private func file(_ f: String) -> some View {
        let url = store.resolve(f, cwd: session.cwd)
        pill(url == nil ? "doc.on.doc" : "doc.text", f, key: "file:" + f) { store.openFile(f, cwd: session.cwd) }
            .help(url == nil ? "Not a file on disk. Click to copy" : "Open \(url!.path)")
            .contextMenu {
                if let url {
                    Button("Open") { NSWorkspace.shared.open(url) }
                    Button("Reveal in Finder") { store.reveal(url) }
                    Button("Copy path") { store.copy(url.path, note: "copied path") }
                } else {
                    Button("Copy") { store.copy(f) }
                }
            }
    }

    private func row<C: View>(_ label: String, @ViewBuilder _ content: () -> C) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(label.uppercased()).font(.caption2.bold()).foregroundStyle(.secondary).frame(width: 44, alignment: .leading)
            content()
        }
    }

    // MARK: 2. kanban

    private var kanban: some View {
        HStack(alignment: .top, spacing: 10) {
            column("Done", "done", "checkmark.circle.fill", .green)
            column("Doing", "doing", "circle.lefthalf.filled", .blue)
            column("Next", "todo", "circle", .secondary)
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
            if items.isEmpty { Text("—").foregroundStyle(.tertiary) }
            ForEach(items, id: \.self) { s in
                let key = "step:" + s.text
                Button { store.copy(s.text, note: "copied step") } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Image(systemName: icon).foregroundStyle(color).font(.caption)
                        Text(s.text).font(.callout).multilineTextAlignment(.leading).fixedSize(horizontal: false, vertical: true)
                    }
                    .padding(8).frame(maxWidth: .infinity, alignment: .leading)
                    .background(hovered(key) ? color.opacity(0.2) : Color.clear, in: RoundedRectangle(cornerRadius: 6))
                    .background(.background, in: RoundedRectangle(cornerRadius: 6))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain).onHover { store.setHover(key, $0) }.help("Click to copy")
                .contextMenu { Button("Copy") { store.copy(s.text, note: "copied step") } }
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background(color.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(color.opacity(0.35)))
    }

    // MARK: 3. timeline

    private var timeline: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(data.timeline.enumerated()), id: \.offset) { i, ev in
                let last = i == data.timeline.count - 1
                event(ev, i: i, showLine: !last || (!data.next.isEmpty && data.status != "done"), here: last && data.status != "done")
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

    private func event(_ ev: BriefData.Event, i: Int, showLine: Bool, here: Bool) -> some View {
        let key = "ev:\(i)"
        return HStack(alignment: .top, spacing: 12) {
            Text(ev.when).font(.caption).foregroundStyle(.secondary).frame(width: 52, alignment: .trailing).padding(.top, 1)
            VStack(spacing: 0) {
                Image(systemName: here ? "circle.lefthalf.filled" : "circle.fill").font(.caption)
                    .foregroundStyle(here ? statusColor : Color.green).frame(width: 14)
                if showLine { Rectangle().fill(.quaternary).frame(width: 2).frame(maxHeight: .infinity) }
            }
            VStack(alignment: .leading, spacing: 4) {
                Button { store.copy(ev.text, note: "copied event") } label: {
                    Text(ev.text).multilineTextAlignment(.leading)
                        .foregroundStyle(hovered(key) ? Color.accentColor : Color.primary)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain).onHover { store.setHover(key, $0) }.help("Click to copy")
                if here {
                    HStack(spacing: 8) {
                        Text("you are here").font(.caption).foregroundStyle(statusColor)
                        Button { Task { await store.open([session.id], fork: false) } } label: { Label("Resume here", systemImage: "play.fill").font(.caption) }
                            .buttonStyle(.link).disabled(!canResume)
                    }
                }
            }
            .padding(.bottom, 12)
            Spacer(minLength: 0)
        }
    }
}
