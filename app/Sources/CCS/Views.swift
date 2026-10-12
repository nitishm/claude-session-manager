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
    var selected = false

    var body: some View {
        HStack(spacing: 8) {
            StatusGlyph(session: session, busy: busy)
            if session.fork {
                Image(systemName: "arrow.triangle.branch").font(.system(size: 10)).foregroundStyle(.secondary).help("a fork")
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(session.displayLabel).lineLimit(1)
                HStack(spacing: 6) {
                    Text(session.dir).foregroundStyle(session.dirGone ? Color.red : (selected ? Color.white.opacity(0.85) : Color.accentColor))
                    if !session.repo.isEmpty { Text(session.repo).foregroundStyle(.secondary) }
                    if let why { Text(why).foregroundStyle(.secondary).italic() }
                }
                .font(.caption).lineLimit(1)
            }
            Spacer(minLength: 8)
            ForEach(session.tags.prefix(2), id: \.self) { t in
                Text("#" + t).font(.caption2.weight(.medium)).foregroundStyle(selected ? Color.white : Theme.tag(t)).padding(.horizontal, 6).padding(.vertical, 1)
                    .background(selected ? Color.white.opacity(0.25) : Theme.tag(t).opacity(0.14), in: Capsule())
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
                if let r = store.repos[session.id], r.kind != "" || r.pr != nil { RepoStrip(store: store, session: session, repo: r) }
                if brief != nil, store.briefStale { staleBanner }
                if let t = brief {
                    if let d = store.briefData { BriefView(store: store, session: session, data: d, facts: store.facts[session.id]) } else { section("Brief") { Text(t).textSelection(.enabled) } }
                }
                else if store.briefBusy, store.briefID == session.id { ProgressView("asking claude… (5-15s)").controlSize(.small) }
                if let f = store.facts[session.id], !(brief != nil && store.briefData != nil) { FactsView(store: store, session: session, facts: f) }
                if brief == nil, !store.briefBusy {
                    Text("No brief for this session.").font(.callout).foregroundStyle(.secondary)
                }
                if let t = store.timelines[session.id], !t.events.isEmpty, !(brief != nil && store.briefData != nil) { HistoryView(store: store, events: t.events) }
                section("Resume") {
                    Text(store.resumes[session.id] ?? "…").font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                }
            }
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task(id: session.id) {
            await store.loadResume(session.id)
            await store.loadFacts(session.id)
            await store.loadRepo(session.id)
            await store.loadTimeline(session.id)
            await store.ensureBrief(session.id, canMake: !session.stub)
        }
    }

    private var staleBanner: some View {
        HStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            Text("Stale: this brief is from \(ago(store.briefAt)) ago and the session has changed since.").font(.callout)
            Spacer()
            Button { Task { await store.loadBrief(session.id, force: true) } } label: { Label("Regenerate", systemImage: "arrow.clockwise") }
                .buttonStyle(.borderedProminent).tint(.orange).controlSize(.small).disabled(store.briefBusy).clickable("stale:regen", enabled: !store.briefBusy)
        }
        .padding(10)
        .background(Color.orange.opacity(0.14), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).stroke(Color.orange.opacity(0.4)))
    }

    private var brief: String? {
        store.briefID == session.id && !store.briefText.isEmpty ? store.briefText : nil
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(session.displayLabel).font(.system(.title, design: .rounded).weight(.bold)).textSelection(.enabled)
            HStack(spacing: 10) {
                Button { store.reveal(URL(fileURLWithPath: session.cwd)) } label: {
                    Label(session.dir, systemImage: "folder").foregroundStyle(session.dirGone ? Color.red : (store.hover == "dir" ? Color.accentColor : Color.secondary))
                }
                .buttonStyle(.plain).onHover { store.setHover("dir", $0) }.disabled(session.dirGone).clickable(enabled: !session.dirGone).help("Reveal in Finder")
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
            if !session.pr.isEmpty, let url = URL(string: session.pr) { Link(session.pr, destination: url).font(.caption).clickable() }
            if session.dirGone { Text("The directory is gone, so this cannot be resumed.").font(.caption).foregroundStyle(.red) }
            HStack {
                ForEach(session.tags, id: \.self) { t in
                    Button { store.tagFilter = t } label: {
                        Text("#" + t).font(.caption.weight(.medium)).foregroundStyle(Theme.tag(t)).padding(.horizontal, 8).padding(.vertical, 2)
                            .background(Theme.tag(t).opacity(store.hover == "tag:" + t ? 0.28 : 0.14), in: Capsule())
                    }
                    .buttonStyle(.plain).onHover { store.setHover("tag:" + t, $0) }.clickable().help("Filter the list by #\(t)")
                }
                Button(session.tags.isEmpty ? "Add tags" : "Edit tags") { store.editingTags = true; store.tagDraft = session.tags.joined(separator: " ") }.buttonStyle(.link).font(.caption).clickable("edittags")
            }
        }
    }

    private var actions: some View {
        HStack {
            Button { Task { await store.open([session.id], fork: false) } } label: { Label("Resume", systemImage: "play.fill") }
                .buttonStyle(.borderedProminent).disabled(session.dirGone || session.stub).clickable("act:resume", enabled: !(session.dirGone || session.stub))
            Button { Task { await store.open([session.id], fork: true) } } label: { Label("Fork", systemImage: "arrow.triangle.branch") }
                .disabled(session.dirGone || session.stub).clickable("act:fork", enabled: !(session.dirGone || session.stub))
            Button { Task { await store.loadBrief(session.id, force: true) } } label: { Label(brief != nil ? "Regenerate" : "Brief", systemImage: brief != nil ? "arrow.clockwise" : "doc.text.magnifyingglass") }
                .disabled(session.stub || store.briefBusy).clickable("act:brief", enabled: !(session.stub || store.briefBusy))
            Spacer()
            HStack(spacing: 4) {
                iconButton("chevron.left.forwardslash.chevron.right", "Open folder in Zed", "act:zed", off: session.dirGone) { store.openFolderInEditor(session.cwd) }
                iconButton("terminal", "New terminal in this folder", "act:term", off: session.dirGone) { store.openTerminalHere(session.cwd) }
                iconButton("doc.on.doc", "Copy resume command", "act:copy") { store.copy("cd \(session.cwd) && claude --resume \(session.id)", note: "copied resume command") }
                if let u = URL(string: session.pr), !session.pr.isEmpty {
                    iconButton("arrow.up.right.square", "Open PR in browser", "act:pr") { NSWorkspace.shared.open(u) }
                }
            }
        }
    }

    private func iconButton(_ symbol: String, _ tip: String, _ key: String, off: Bool = false, _ action: @escaping () -> Void) -> some View {
        Button(action: action) { Image(systemName: symbol).frame(width: 18, height: 18) }
            .disabled(off).help(tip).clickable(key, enabled: !off)
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
                Button("#" + t) { tap(t) }.buttonStyle(.bordered).controlSize(.small).clickable()
            }
        }
    }
}

/// Branch, change count and PR state as chips. Click the PR chip to open it.
struct RepoStrip: View {
    @Bindable var store: Store
    let session: Session
    let repo: RepoState

    var body: some View {
        HStack(spacing: 8) {
            if repo.kind != "" {
                chip("arrow.triangle.branch", repo.branch.isEmpty ? "no \(repo.kind == "jj" ? "bookmark" : "branch")" : repo.branch, .accentColor)
                if repo.dirty > 0 { chip("pencil", "\(repo.dirty) changed", .orange) } else { chip("checkmark", "clean", .secondary) }
                if repo.ahead > 0 || repo.behind > 0 { chip("arrow.up.arrow.down", "\(repo.ahead) ahead, \(repo.behind) behind", .secondary) }
            }
            if let p = repo.pr { prChip(p) }
            Spacer(minLength: 0)
        }
    }

    private func prChip(_ p: PRState) -> some View {
        let color: Color = p.state == "MERGED" ? .purple : p.state == "CLOSED" || p.checks == "failing" || p.review == "CHANGES_REQUESTED" ? .red : .green
        var bits = [p.draft ? "draft" : p.state.lowercased()]
        switch p.checks {
        case "failing": bits.append("\(p.failed) checks failing")
        case "pending": bits.append("\(p.pending) checks running")
        case "passing": bits.append("checks pass")
        default: break
        }
        switch p.review {
        case "APPROVED": bits.append("approved")
        case "CHANGES_REQUESTED": bits.append("changes requested")
        case "REVIEW_REQUIRED": bits.append("needs review")
        default: break
        }
        return Button {
            if let u = URL(string: session.pr) { NSWorkspace.shared.open(u) }
        } label: {
            label("arrow.triangle.pull", "PR " + bits.joined(separator: ", "), color, hot: store.hover == "repo:pr")
        }
        .buttonStyle(.plain).onHover { store.setHover("repo:pr", $0) }.clickable("repo:pr").help(p.title)
    }

    private func chip(_ symbol: String, _ text: String, _ color: Color) -> some View { label(symbol, text, color, hot: false) }

    private func label(_ symbol: String, _ text: String, _ color: Color, hot: Bool) -> some View {
        Label(text, systemImage: symbol).font(.caption.weight(.medium)).foregroundStyle(color)
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background(color.opacity(hot ? 0.28 : 0.14), in: Capsule())
    }
}
