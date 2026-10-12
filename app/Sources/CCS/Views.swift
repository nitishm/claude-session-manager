import SwiftUI

// MARK: list

struct StatusGlyph: View {
    let session: Session
    let busy: Bool

    var body: some View {
        Group {
            if busy {
                ProgressView().controlSize(.small)
            } else if let live = session.live {
                switch live.status {
                case "idle": Image(systemName: "circle.fill").foregroundStyle(.green).help("running, idle")
                case "busy": Image(systemName: "circle.lefthalf.filled").foregroundStyle(.orange).help("running, working")
                default: Image(systemName: "questionmark.circle.fill").foregroundStyle(.blue).help("running, needs you")
                }
            } else if session.pinned {
                Image(systemName: "star.fill").foregroundStyle(.yellow).help("pinned")
            } else if session.preBoot {
                Image(systemName: "arrow.counterclockwise").foregroundStyle(.secondary).help("was open before the last reboot")
            } else if session.stub {
                Image(systemName: "sparkle").foregroundStyle(.secondary).help("new, no prompts yet")
            } else {
                Color.clear
            }
        }
        .font(.system(size: 11))
        .frame(width: 16)
    }
}

struct SessionRow: View {
    let session: Session
    let busy: Bool
    let why: String?

    var body: some View {
        HStack(spacing: 8) {
            StatusGlyph(session: session, busy: busy)
            if session.fork {
                Image(systemName: "arrow.triangle.branch").font(.system(size: 10)).foregroundStyle(.secondary).help("a fork")
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(session.displayLabel).lineLimit(1)
                HStack(spacing: 6) {
                    Text(session.dir).foregroundStyle(session.dirGone ? Color.red : Color.accentColor)
                    if !session.repo.isEmpty { Text(session.repo).foregroundStyle(.secondary) }
                    if let why { Text(why).foregroundStyle(.secondary).italic() }
                }
                .font(.caption).lineLimit(1)
            }
            Spacer(minLength: 8)
            ForEach(session.tags.prefix(2), id: \.self) { t in
                Text("#" + t).font(.caption2).padding(.horizontal, 5).padding(.vertical, 1)
                    .background(.quaternary, in: Capsule())
            }
            TimelineView(.periodic(from: .now, by: 30)) { ctx in
                Text(ago(session.mtime, now: ctx.date)).foregroundStyle(.secondary).monospacedDigit()
            }
            .font(.caption).frame(width: 30, alignment: .trailing)
            Text("\(session.prompts)").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                .frame(width: 26, alignment: .trailing).help("prompts")
        }
        .padding(.vertical, 2)
    }
}

// MARK: detail

struct DetailView: View {
    @Bindable var store: Store
    let session: Session

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                actions
                if let t = brief {
                    if let d = store.briefData { BriefView(data: d) } else { section("Brief") { Text(t).textSelection(.enabled) } }
                }
                else if store.briefBusy, store.briefID == session.id { ProgressView("asking claude… (5-15s)").controlSize(.small) }
                section(session.summary.isEmpty ? "First ask → last ask" : "Summary") {
                    Text(session.excerpt).textSelection(.enabled)
                }
                if !session.turns.isEmpty {
                    section("Recent conversation") {
                        VStack(alignment: .leading, spacing: 10) {
                            ForEach(Array(session.turns.enumerated()), id: \.offset) { _, t in
                                HStack(alignment: .top, spacing: 8) {
                                    Text(t.user ? "you" : "claude").font(.caption.bold())
                                        .foregroundStyle(t.user ? Color.accentColor : Color.green).frame(width: 44, alignment: .leading)
                                    Text(t.text).textSelection(.enabled)
                                }
                            }
                        }
                    }
                }
                section("Resume") {
                    Text(store.resumes[session.id] ?? "…").font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                }
            }
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task(id: session.id) { await store.loadResume(session.id) }
    }

    private var brief: String? {
        store.briefID == session.id && !store.briefText.isEmpty ? store.briefText : nil
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(session.displayLabel).font(.title2.bold()).textSelection(.enabled)
            HStack(spacing: 10) {
                Label(session.dir, systemImage: "folder").foregroundStyle(session.dirGone ? Color.red : Color.secondary)
                if !session.repo.isEmpty { Label(session.repo, systemImage: "arrow.triangle.branch") }
                if let live = session.live { Label(live.status, systemImage: "circle.fill").foregroundStyle(.green) }
            }
            .font(.callout).foregroundStyle(.secondary)
            HStack(spacing: 10) {
                Text("started \(session.started.formatted(date: .abbreviated, time: .shortened))")
                Text("active \(ago(session.mtime)) ago")
                Text("\(session.prompts) prompts")
                if session.forks > 0 { Text("\(session.forks) fork(s)") }
                if session.stale { Text("stale").foregroundStyle(.orange) }
            }
            .font(.caption).foregroundStyle(.secondary)
            if !session.pr.isEmpty, let url = URL(string: session.pr) { Link(session.pr, destination: url).font(.caption) }
            if session.dirGone { Text("The directory is gone, so this cannot be resumed.").font(.caption).foregroundStyle(.red) }
            HStack {
                ForEach(session.tags, id: \.self) { t in
                    Text("#" + t).font(.caption).padding(.horizontal, 7).padding(.vertical, 2).background(.quaternary, in: Capsule())
                }
                Button(session.tags.isEmpty ? "Add tags" : "Edit tags") { store.editingTags = true; store.tagDraft = session.tags.joined(separator: " ") }.buttonStyle(.link).font(.caption)
            }
        }
    }

    private var actions: some View {
        HStack {
            Button { Task { await store.open([session.id], fork: false) } } label: { Label("Resume", systemImage: "play.fill") }
                .buttonStyle(.borderedProminent).disabled(session.dirGone || session.stub)
            Button { Task { await store.open([session.id], fork: true) } } label: { Label("Fork", systemImage: "arrow.triangle.branch") }
                .disabled(session.dirGone || session.stub)
            Button { Task { await store.loadBrief(session.id, force: brief != nil) } } label: { Label("Brief", systemImage: "doc.text.magnifyingglass") }
                .disabled(session.stub || store.briefBusy)
            Button { Task { await store.summarize(session.id) } } label: { Label("Summarize", systemImage: "text.badge.star") }
                .disabled(session.stub || store.busy.contains(session.id))
            Spacer()
        }
    }

    private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.headline)
            content().font(.body)
        }
    }
}

struct TagEditor: View {
    @Bindable var store: Store
    let session: Session

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Tags for \(session.displayLabel)").font(.headline).lineLimit(2)
            TextField("space or comma separated, empty clears", text: $store.tagDraft)
                .textFieldStyle(.roundedBorder).onSubmit(save)
            if !store.tagCounts.isEmpty {
                Text("Existing").font(.caption).foregroundStyle(.secondary)
                FlowTags(tags: store.tagCounts.map(\.name)) { t in
                    if !store.tagDraft.split(whereSeparator: { " ,".contains($0) }).contains(where: { $0 == Substring(t) }) {
                        store.tagDraft = (store.tagDraft.isEmpty ? "" : store.tagDraft + " ") + t
                    }
                }
            }
            HStack {
                Spacer()
                Button("Cancel") { store.editingTags = false }.keyboardShortcut(.cancelAction)
                Button("Save", action: save).keyboardShortcut(.defaultAction)
            }
        }
        .padding(18).frame(width: 420)
    }

    private func save() {
        let tags = store.tagDraft.split(whereSeparator: { " ,".contains($0) }).map { String($0).trimmingCharacters(in: CharacterSet(charactersIn: "#")) }
        store.editingTags = false
        Task { await store.setTags(session.id, tags) }
    }
}

struct FlowTags: View {
    let tags: [String]
    let tap: (String) -> Void

    var body: some View {
        // Simple wrapping row: tags are few, so a grid of adaptive columns is enough.
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 70), spacing: 6)], alignment: .leading, spacing: 6) {
            ForEach(tags, id: \.self) { t in
                Button("#" + t) { tap(t) }.buttonStyle(.bordered).controlSize(.small)
            }
        }
    }
}
