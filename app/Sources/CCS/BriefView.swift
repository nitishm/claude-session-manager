import SwiftUI
import AppKit

/// A structured brief: a status banner, stat tiles, a timeline of icon + outcome rows, step columns and files.
/// State lives in the Store because @State needs Xcode's macro plugin.
struct BriefView: View {
    @Bindable var store: Store
    let session: Session
    let data: BriefData

    private var canResume: Bool { !session.dirGone && !session.stub }
    private var status: (symbol: String, color: Color) { Theme.status(data.status) }
    private func hovered(_ key: String) -> Bool { store.hover == key }

    private var daysActive: Int {
        max(1, (Calendar.current.dateComponents([.day], from: session.started, to: session.mtime).day ?? 0) + 1)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            hero
            tiles
            if !data.timeline.isEmpty { fold("timeline", "Timeline", "\(data.timeline.count) milestones") { timeline } }
            if !data.steps.isEmpty { fold("steps", "Steps", "\(data.progress.done)/\(data.steps.count) done") { kanban } }
            if !data.files.isEmpty { fold("files", "Files", "\(data.files.count)") { files } }
        }
    }

    // MARK: hero banner

    private var hero: some View {
        HStack(alignment: .top, spacing: 16) {
            if !data.steps.isEmpty {
                let p = data.progress
                Ring(value: Double(p.done) / Double(max(p.total, 1)), color: status.color, label: "\(p.done)/\(p.total)", size: 70)
            } else {
                Image(systemName: status.symbol).font(.system(size: 34)).foregroundStyle(status.color).frame(width: 70, height: 70)
            }
            VStack(alignment: .leading, spacing: 8) {
                Text(data.goal).font(.system(.title3, design: .rounded).weight(.bold)).textSelection(.enabled)
                HStack(spacing: 8) {
                    Label(data.status.capitalized, systemImage: status.symbol)
                        .font(.caption.bold()).foregroundStyle(status.color)
                        .padding(.horizontal, 9).padding(.vertical, 3).background(status.color.opacity(0.14), in: Capsule())
                    if !data.statusNote.isEmpty { Text(data.statusNote).font(.callout).foregroundStyle(.secondary) }
                }
                if !data.next.isEmpty {
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Image(systemName: "arrow.right.circle.fill").foregroundStyle(Theme.violet)
                        Text(data.next).font(.callout.weight(.medium)).textSelection(.enabled)
                    }
                    .padding(10).frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.violet.opacity(0.10), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                }
                Button { Task { await store.open([session.id], fork: false) } } label: { Label("Resume session", systemImage: "play.fill") }
                    .buttonStyle(.borderedProminent).tint(Theme.violet).controlSize(.regular).disabled(!canResume)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(16)
        .background {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(LinearGradient(colors: [status.color.opacity(0.22), status.color.opacity(0.05)], startPoint: .topLeading, endPoint: .bottomTrailing))
                .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).stroke(status.color.opacity(0.35)))
        }
    }

    // MARK: stat tiles

    private var tiles: some View {
        HStack(spacing: 10) {
            StatTile(symbol: "text.bubble.fill", value: "\(session.prompts)", label: "prompts", color: .blue)
            StatTile(symbol: "calendar", value: "\(daysActive)", label: daysActive == 1 ? "day" : "days", color: .purple)
            StatTile(symbol: "checklist", value: "\(data.progress.done)/\(data.steps.count)", label: "steps done", color: .green)
            StatTile(symbol: "doc.text.fill", value: "\(data.files.count)", label: data.files.count == 1 ? "file" : "files", color: .orange)
        }
    }

    // MARK: collapsible sections

    private func fold<C: View>(_ id: String, _ title: String, _ note: String, @ViewBuilder _ content: () -> C) -> some View {
        let open = !store.collapsed.contains(id)
        return VStack(alignment: .leading, spacing: 10) {
            Button { withAnimation(.easeInOut(duration: 0.15)) { store.toggle(id) } } label: {
                HStack(spacing: 8) {
                    Image(systemName: "chevron.right").font(.caption.bold()).rotationEffect(.degrees(open ? 90 : 0)).frame(width: 12)
                    Text(title).font(.system(.headline, design: .rounded))
                    Text(note).font(.caption).foregroundStyle(.secondary)
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain).help(open ? "Collapse" : "Expand")
            if open { content() }
        }
    }

    // MARK: timeline

    private var timeline: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(data.timeline.enumerated()), id: \.offset) { i, ev in
                let last = i == data.timeline.count - 1
                event(ev, line: !last || (!data.next.isEmpty && data.status != "done"), here: last && data.status != "done")
            }
            if !data.next.isEmpty, data.status != "done" {
                HStack(alignment: .center, spacing: 12) {
                    Text("next").font(.caption).foregroundStyle(.secondary).frame(width: 52, alignment: .trailing)
                    Image(systemName: "arrow.right").font(.caption.bold()).foregroundStyle(Theme.violet)
                        .frame(width: 30, height: 30).background(Theme.violet.opacity(0.12), in: Circle())
                    Text(data.next).foregroundStyle(.secondary)
                }
            }
        }
        .card()
    }

    private func event(_ ev: BriefData.Event, line: Bool, here: Bool) -> some View {
        let k = Theme.kind(ev.kind)
        return HStack(alignment: .top, spacing: 12) {
            Text(ev.when).font(.caption).foregroundStyle(.secondary).frame(width: 52, alignment: .trailing).padding(.top, 7)
            VStack(spacing: 0) {
                Image(systemName: k.symbol).font(.system(size: 13, weight: .semibold)).foregroundStyle(k.color)
                    .frame(width: 30, height: 30).background(k.color.opacity(0.16), in: Circle())
                    .overlay(Circle().stroke(here ? k.color : .clear, lineWidth: 2))
                if line { Rectangle().fill(k.color.opacity(0.25)).frame(width: 2).frame(maxHeight: .infinity) }
            }
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 8) {
                    Text(ev.text).font(.callout.weight(.semibold)).textSelection(.enabled)
                    if !k.label.isEmpty {
                        Text(k.label).font(.caption2.bold()).foregroundStyle(k.color)
                            .padding(.horizontal, 6).padding(.vertical, 1).background(k.color.opacity(0.14), in: Capsule())
                    }
                }
                if let o = ev.outcome, !o.isEmpty {
                    Label(o, systemImage: "arrow.turn.down.right").font(.callout).foregroundStyle(.secondary)
                        .labelStyle(OutcomeLabel())
                }
                if here {
                    HStack(spacing: 8) {
                        Text("you are here").font(.caption.bold()).foregroundStyle(status.color)
                        Button { Task { await store.open([session.id], fork: false) } } label: { Label("Resume here", systemImage: "play.fill").font(.caption) }
                            .buttonStyle(.link).disabled(!canResume)
                    }
                    .padding(.top, 2)
                }
            }
            .padding(.top, 5).padding(.bottom, 14)
            Spacer(minLength: 0)
        }
    }

    // MARK: steps

    private var kanban: some View {
        HStack(alignment: .top, spacing: 10) {
            column("Done", "done", "checkmark.circle.fill", .green)
            column("Doing", "doing", "circle.lefthalf.filled", Theme.violet)
            column("Next", "todo", "circle.dashed", .secondary)
        }
    }

    private func column(_ title: String, _ state: String, _ icon: String, _ color: Color) -> some View {
        let items = data.steps.filter { $0.state == state }
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Circle().fill(color).frame(width: 8, height: 8)
                Text(title).font(.system(.caption, design: .rounded).weight(.bold)).foregroundStyle(color)
                Spacer()
                Text("\(items.count)").font(.caption.bold()).foregroundStyle(.secondary)
            }
            if items.isEmpty { Text("Nothing here").font(.caption).foregroundStyle(.tertiary).padding(.vertical, 6) }
            ForEach(items, id: \.self) { s in stepCard(s, color: color) }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background(color.opacity(0.07), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private func stepCard(_ s: BriefData.Step, color: Color) -> some View {
        let k = Theme.kind(s.kind)
        return VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Image(systemName: k.symbol).font(.caption).foregroundStyle(k.color)
                Text(s.text).font(.callout.weight(.medium)).fixedSize(horizontal: false, vertical: true)
            }
            if let o = s.outcome, !o.isEmpty {
                Text(o).font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true).padding(.leading, 20)
            }
        }
        .padding(9).frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 9, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 9, style: .continuous).stroke(color.opacity(0.25)))
    }

    // MARK: files

    private var files: some View {
        FlowLayout(spacing: 8) {
            ForEach(data.files, id: \.self) { f in file(f) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    /// Real files open on click; ones that no longer exist are dimmed.
    @ViewBuilder private func file(_ f: String) -> some View {
        let url = store.resolve(f, cwd: session.cwd)
        let key = "file:" + f
        Button { if let url { NSWorkspace.shared.open(url) } } label: {
            Label {
                Text(f).lineLimit(1).truncationMode(.middle)
            } icon: {
                Image(systemName: url == nil ? "doc.badge.ellipsis" : "doc.text.fill").foregroundStyle(url == nil ? Color.secondary : Color.orange)
            }
            .font(.system(.caption, design: .monospaced))
            .foregroundStyle(url == nil ? Color.secondary.opacity(0.6) : (hovered(key) ? Color.accentColor : Color.primary))
            .padding(.horizontal, 10).padding(.vertical, 5)
            .frame(maxWidth: 300, alignment: .leading)
            .background(hovered(key) && url != nil ? Color.accentColor.opacity(0.16) : Color.primary.opacity(0.05), in: Capsule())
        }
        .buttonStyle(.plain).disabled(url == nil).onHover { store.setHover(key, $0) }
        .help(url.map { "Open " + $0.path } ?? "Not found under \(session.dir)")
        .contextMenu {
            if let url {
                Button("Open") { NSWorkspace.shared.open(url) }
                Button("Reveal in Finder") { store.reveal(url) }
            }
        }
    }
}

/// Puts the icon first and lets long text wrap under the text, not the icon.
struct OutcomeLabel: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            configuration.icon.font(.caption2).foregroundStyle(.tertiary)
            configuration.title.fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// Wraps its children onto rows, each at its natural width.
struct FlowLayout: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, rowH: CGFloat = 0, widest: CGFloat = 0
        for s in subviews {
            let sz = s.sizeThatFits(.unspecified)
            if x > 0, x + sz.width > width { x = 0; y += rowH + spacing; rowH = 0 }
            x += sz.width + spacing
            rowH = max(rowH, sz.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, width), height: y + rowH)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowH: CGFloat = 0
        for s in subviews {
            let sz = s.sizeThatFits(.unspecified)
            if x > bounds.minX, x + sz.width > bounds.maxX { x = bounds.minX; y += rowH + spacing; rowH = 0 }
            s.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(sz))
            x += sz.width + spacing
            rowH = max(rowH, sz.height)
        }
    }
}
