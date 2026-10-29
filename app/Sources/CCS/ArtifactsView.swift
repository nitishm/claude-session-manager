import SwiftUI

/// Sidebar list for the Artifacts mode: every page Claude published, newest first.
struct ArtifactList: View {
    @Bindable var store: Store

    var body: some View {
        List(selection: $store.artSelection) {
            ForEach(store.artifactShown) { a in
                ArtifactListRow(artifact: a, selected: store.artSelection == a.id)
                    .tag(a.id)
                    .contextMenu {
                        Button("Open in the browser") { open(a) }
                        Button("Copy link") { store.copy(a.url, note: "copied the artifact link") }
                        if let f = a.file { Button("Show the source file") { store.reveal(URL(fileURLWithPath: f)) } }
                        Divider()
                        Button("Go to session") { store.goToSession(a.sessionId) }
                    }
            }
        }
        .listStyle(.inset)
        .scrollContentBackground(.hidden)
        .overlay {
            if store.artifactShown.isEmpty, store.conn == .ok {
                ContentUnavailableView(store.query.isEmpty && !store.artFiltersActive ? "No artifacts yet" : "No match", systemImage: "square.on.square",
                                       description: Text(store.query.isEmpty && !store.artFiltersActive ? "Pages Claude publishes show up here." : "Try a different search or clear the filters."))
            }
        }
        .onKeyPress(.return) {
            guard let a = store.artifact else { return .ignored }
            open(a)
            return .handled
        }
    }

    private func open(_ a: ArtifactRow) {
        if let u = URL(string: a.url) { NSWorkspace.shared.open(u) }
    }
}

struct ArtifactListRow: View {
    let artifact: ArtifactRow
    var selected = false

    var body: some View {
        HStack(spacing: 8) {
            // No local file means the preview is the live claude.ai page in an embedded browser.
            let web = artifact.file == nil
            Image(systemName: web ? "globe" : "doc.richtext")
                .foregroundStyle(selected ? Color.white : (web ? Color.accentColor : Theme.slate))
                .help(web ? "Opens in an embedded browser (claude.ai)" : "Local page")
                .frame(width: 18)
            VStack(alignment: .leading, spacing: 2) {
                Text(artifact.title).lineLimit(1)
                if let ex = artifact.excerpt, !ex.isEmpty {
                    Text("⌕ " + ex).font(.caption).foregroundStyle(.orange).lineLimit(2)
                } else {
                    HStack(spacing: 6) {
                        Text(artifact.dir).foregroundStyle(selected ? Color.white.opacity(0.85) : Color.accentColor)
                        Text(artifact.label).foregroundStyle(.secondary)
                    }
                    .font(.caption).lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            TimelineView(.periodic(from: .now, by: 30)) { ctx in
                Text(ago(artifact.at, now: ctx.date)).foregroundStyle(.secondary).monospacedDigit()
            }
            .font(.caption).frame(width: 30, alignment: .trailing)
        }
        .padding(.vertical, 2)
    }
}

/// Detail pane for the selected artifact: the page fills the pane, and a drag handle at the bottom
/// pulls up a details panel (session, link, source) that takes room from the preview.
struct ArtifactDetail: View {
    @Bindable var store: Store

    var body: some View {
        if let a = store.artifact {
            VStack(alignment: .leading, spacing: 0) {
                header(a)
                preview(a).frame(minHeight: 0, maxHeight: .infinity)
                handle
                if store.artDetailsH > 0 { details(a).frame(height: store.artDetailsH) }
            }
        } else {
            ContentUnavailableView("Select an artifact", systemImage: "square.on.square",
                                   description: Text("Search above, or pick a page on the left."))
        }
    }

    private func header(_ a: ArtifactRow) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(a.title).font(Theme.heading(24)).lineLimit(2).textSelection(.enabled)
            HStack(spacing: 8) {
                Label(a.at.formatted(date: .abbreviated, time: .shortened), systemImage: "clock")
                Label(a.dir, systemImage: "folder")
                Label(a.label, systemImage: "text.bubble").lineLimit(1)
            }
            .font(.callout).foregroundStyle(.secondary)
            HStack(spacing: 8) {
                Button { if let u = URL(string: a.url) { NSWorkspace.shared.open(u) } } label: { Label("Open", systemImage: "arrow.up.right.square") }
                    .buttonStyle(.borderedProminent).clickable("art:open")
                Button { store.copy(a.url, note: "copied the artifact link") } label: { Label("Copy link", systemImage: "link") }
                    .clickable("art:copy")
                if let f = a.file {
                    Button { store.reveal(URL(fileURLWithPath: f)) } label: { Label("Show source", systemImage: "folder") }
                        .clickable("art:reveal")
                }
                Button { store.goToSession(a.sessionId) } label: { Label("Go to session", systemImage: "text.bubble") }
                    .clickable("art:session")
            }
        }
        .padding(.horizontal, 18).padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The page itself, rendered from its source file. Pages whose file is gone and cannot be rebuilt show a note.
    @ViewBuilder private func preview(_ a: ArtifactRow) -> some View {
        if let f = a.file, FileManager.default.fileExists(atPath: f) {
            ArtifactPreview(file: URL(fileURLWithPath: f), remote: nil)
                .frame(minWidth: 0, maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
                .background(Color.white)
                .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).stroke(Color.primary.opacity(0.12)))
                .padding(.horizontal, 18)
        } else if a.url.hasPrefix("https://claude.ai/"), let u = URL(string: a.url) {
            ArtifactPreview(file: nil, remote: u)
                .frame(minWidth: 0, maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
                .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).stroke(Color.primary.opacity(0.12)))
                .padding(.horizontal, 18)
        } else {
            VStack {
                HStack(spacing: 8) {
                    Image(systemName: "eye.slash").foregroundStyle(.secondary)
                    Text("No preview: the source file is gone. Open the page to see it.").font(.callout).foregroundStyle(.secondary)
                }
                .padding(12).frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .padding(.horizontal, 18)
                Spacer(minLength: 0)
            }
        }
    }

    /// Drag up to open the details, down to close them. The preview gives up or takes back the room.
    private var handle: some View {
        ZStack {
            Color.clear
            Capsule().fill(Color.primary.opacity(0.25)).frame(width: 44, height: 4)
        }
        .frame(height: 18).frame(maxWidth: .infinity).contentShape(Rectangle())
        .help("Drag to resize the preview")
        .onHover { inside in if inside { NSCursor.resizeUpDown.push() } else { NSCursor.pop() } }
        .gesture(
            DragGesture(minimumDistance: 2, coordinateSpace: .global)
                .onChanged { g in
                    let base = store.artDragBase ?? store.artDetailsH
                    store.artDragBase = base
                    store.artDetailsH = min(max(0, base - g.translation.height), 520)
                }
                .onEnded { _ in store.artDragBase = nil }
        )
    }

    private func details(_ a: ArtifactRow) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 4) {
                Text("From session").font(.caption).foregroundStyle(.secondary)
                Text(a.label).font(.callout.weight(.semibold)).textSelection(.enabled)
                Text("Link").font(.caption).foregroundStyle(.secondary).padding(.top, 6)
                Text(a.url).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                if let f = a.file {
                    Text("Source").font(.caption).foregroundStyle(.secondary).padding(.top, 6)
                    Text(f).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                }
            }
            .padding(.horizontal, 18).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// Sessions | Artifacts switch: a pill with a violet highlight that slides under the chosen side.
struct ModeSwitch: View {
    @Bindable var store: Store
    @Namespace private var pill

    private func icon(_ m: Store.Mode) -> String { m == .sessions ? "text.bubble.fill" : "square.stack.3d.up.fill" }

    var body: some View {
        HStack(spacing: 2) {
            ForEach(Store.Mode.allCases, id: \.self) { m in
                let on = store.mode == m
                Button {
                    withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) { store.mode = m }
                } label: {
                    HStack(spacing: 5) {
                        Image(systemName: icon(m)).font(.system(size: 11, weight: .semibold))
                        Text(m.rawValue).font(.system(size: 12, weight: .semibold, design: .rounded))
                    }
                    .foregroundStyle(on ? Color.white : Color.primary.opacity(store.hover == "mode:\(m.rawValue)" ? 0.85 : 0.6))
                    .padding(.horizontal, 12).padding(.vertical, 5)
                    .background {
                        if on {
                            Capsule()
                                .fill(LinearGradient(colors: [Theme.violet, Theme.violet.opacity(0.7)], startPoint: .topLeading, endPoint: .bottomTrailing))
                                .shadow(color: Theme.violet.opacity(0.35), radius: 4, y: 1)
                                .matchedGeometryEffect(id: "pill", in: pill)
                        }
                    }
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain).onHover { store.setHover("mode:\(m.rawValue)", $0) }.clickable()
            }
        }
        .padding(3)
        .background(Color.primary.opacity(0.07), in: Capsule())
        .overlay(Capsule().stroke(Color.primary.opacity(0.08)))
        .fixedSize()
    }
}
