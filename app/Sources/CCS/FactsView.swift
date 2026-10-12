import SwiftUI

/// What the transcript says happened: size, files touched, commands run. All of it is free to compute.
struct FactsView: View {
    @Bindable var store: Store
    let session: Session
    let facts: Facts

    private var showAll: Bool { store.collapsed.contains("files:all") }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Activity").font(Theme.heading(16))
            HStack(spacing: 10) {
                chip("bubble.left.and.bubble.right.fill", "\(facts.turns)", "turns", .blue)
                if facts.activeMin > 0 { chip("clock.fill", duration(facts.activeMin), "active", .purple) }
                chip("arrow.down.circle.fill", compact(facts.in + facts.cacheRead + facts.cacheWrite), "in", .teal)
                chip("arrow.up.circle.fill", compact(facts.out), "out", .green)
                if facts.cacheHit > 0 { chip("bolt.fill", "\(facts.cacheHit)%", "cached", .orange) }
                Spacer(minLength: 0)
            }
            if !facts.model.isEmpty {
                Text(facts.model.replacingOccurrences(of: "claude-", with: "")).font(.caption).foregroundStyle(.secondary)
            }
            if !facts.files.isEmpty { files }
            if !facts.commands.isEmpty { commands }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card()
    }

    private func chip(_ symbol: String, _ value: String, _ label: String, _ color: Color) -> some View {
        HStack(spacing: 6) {
            Image(systemName: symbol).foregroundStyle(color)
            Text(value).font(.system(.callout, design: .rounded).weight(.bold)).monospacedDigit()
            Text(label).font(.caption).foregroundStyle(.secondary)
        }
        .padding(.horizontal, 10).padding(.vertical, 5)
        .background(color.opacity(0.10), in: Capsule())
    }

    private var files: some View {
        let shown = showAll ? facts.files : Array(facts.files.prefix(4))
        return VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("Files touched").font(.subheadline.weight(.semibold))
                Text("click to open in Zed").font(.caption).foregroundStyle(.secondary)
            }
            ForEach(shown, id: \.path) { f in
                let key = "touch:" + f.path
                let gone = store.resolve(f.path, cwd: session.cwd) == nil
                Button { store.openFile(f.path, cwd: session.cwd) } label: {
                    HStack(spacing: 8) {
                        Image(systemName: f.edits > 0 ? "pencil.circle.fill" : "eye.circle")
                            .foregroundStyle(f.edits > 0 ? Color.green : Color.secondary)
                        Text(f.rel).font(.system(.caption, design: .monospaced)).lineLimit(1).truncationMode(.head)
                        Spacer(minLength: 8)
                        if f.edits > 0 { Text("edited ×\(f.edits)").font(.caption2).foregroundStyle(.green) }
                        if f.reads > 0 { Text("read ×\(f.reads)").font(.caption2).foregroundStyle(.secondary) }
                    }
                    .foregroundStyle(gone ? Color.secondary.opacity(0.6) : Color.primary)
                    .padding(.horizontal, 8).padding(.vertical, 4)
                    .background(store.hover == key && !gone ? Color.accentColor.opacity(0.14) : Color.clear, in: RoundedRectangle(cornerRadius: 7, style: .continuous))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain).disabled(gone).onHover { store.setHover(key, $0) }.clickable(enabled: !gone)
                .help(gone ? "No longer on disk" : "Open " + f.path)
                .contextMenu {
                    Button("Open in Zed") { store.openFile(f.path, cwd: session.cwd) }
                    Button("Reveal in Finder") { store.reveal(URL(fileURLWithPath: f.path)) }
                    Button("Copy path") { store.copy(f.path) }
                }
            }
            if facts.files.count > 4 {
                Button(showAll ? "Show fewer" : "Show all \(facts.files.count)") { store.toggle("files:all") }
                    .buttonStyle(.link).font(.caption).clickable()
            }
        }
    }

    private var commands: some View {
        let open = store.collapsed.contains("cmds:open")
        return VStack(alignment: .leading, spacing: 4) {
            Button { store.toggle("cmds:open") } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right").font(.caption2.bold()).rotationEffect(.degrees(open ? 90 : 0)).frame(width: 10)
                    Text("Commands").font(.subheadline.weight(.semibold))
                    Text("last \(facts.commands.count) of \(facts.commandCount)").font(.caption).foregroundStyle(.secondary)
                    if open { Text("click one to copy").font(.caption).foregroundStyle(.secondary) }
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain).clickable("cmds:head")
            if open {
                ForEach(Array(facts.commands.enumerated()), id: \.offset) { i, c in
                    let key = "cmd:\(i)"
                    Button { store.copy(c, note: "copied command") } label: {
                        HStack(spacing: 6) {
                            Text("$").foregroundStyle(.secondary)
                            Text(c).lineLimit(1).truncationMode(.tail)
                            Spacer(minLength: 0)
                        }
                        .font(.system(.caption, design: .monospaced))
                        .padding(.horizontal, 8).padding(.vertical, 4)
                        .background(store.hover == key ? Color.accentColor.opacity(0.14) : Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 7, style: .continuous))
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain).onHover { store.setHover(key, $0) }.clickable()
                }
            }
        }
    }
}
