import SwiftUI

struct ContentView: View {
    @Bindable var store: Store
    var body: some View {
        NavigationSplitView {
            sidebar.navigationSplitViewColumnWidth(min: 420, ideal: 520)
        } detail: {
            detail
        }
        .searchable(text: $store.query, placement: .toolbar, prompt: "Search title, dir, tags, prompts")
        .toolbar { toolbar }
        .safeAreaInset(edge: .bottom) { statusBar }
        .sheet(isPresented: $store.editingTags) {
            if let s = store.single { TagEditor(store: store, session: s) }
        }
        .sheet(item: $store.asking) { mode in AskSheet(store: store, mode: mode) }
        .confirmationDialog("Move \(store.selection.count) session(s) to the Trash?", isPresented: $store.confirmDelete) {
            Button("Move to Trash", role: .destructive) {
                let ids = Array(store.selection)
                Task { await store.delete(ids) }
            }
        } message: { Text("Running sessions are skipped. Undo in the toolbar restores the last batch.") }
        .frame(minWidth: 900, minHeight: 520)
    }

    // MARK: sidebar

    private var sidebar: some View {
        VStack(spacing: 0) {
            sidebarHeader
            Divider()
            if store.hits != nil {
                HStack {
                    Image(systemName: store.hitKind == "deep" ? "doc.text.magnifyingglass" : "sparkles")
                    Text("\(store.hitKind == "deep" ? "Deep search" : "Ask"): \(store.hitQuery)").lineLimit(1)
                    Spacer()
                    Button("Clear") { store.clearHits() }.buttonStyle(.link).clickable("clearhits")
                }
                .font(.callout).padding(.horizontal, 12).padding(.vertical, 6).background(.quaternary)
            }
            if case .down(let why) = store.conn { downBanner(why) }
            List(selection: $store.selection) {
                ForEach(store.rows) { s in
                    SessionRow(session: s, busy: store.busy.contains(s.id), why: store.why(s.id), selected: store.selection.contains(s.id))
                        .tag(s.id)
                        .contextMenu { rowMenu(s) }
                }
            }
            .listStyle(.inset)
            .overlay { if store.rows.isEmpty, store.conn == .ok { empty } }
            .onKeyPress(.return) {
                guard !store.selection.isEmpty else { return .ignored }
                Task { await store.resume(Array(store.selection)) }
                return .handled
            }
        }
    }

    private var sidebarHeader: some View {
        HStack(spacing: 8) {
            Text("Sessions").font(.system(.title3, design: .rounded).weight(.bold))
            Text("\(store.rows.count)").font(.caption.weight(.semibold)).monospacedDigit().foregroundStyle(.secondary)
                .padding(.horizontal, 7).padding(.vertical, 1).background(Color.primary.opacity(0.08), in: Capsule())
            Spacer(minLength: 4)
            filterChip("Running", store.live.count, $store.liveOnly, .green, "chip:live")
            filterChip("Stale", nil, $store.staleOnly, .orange, "chip:stale")
            filterChip("Forks", nil, $store.forksOnly, .purple, "chip:forks")
            Menu {
                Picker("Sort", selection: $store.sort) { ForEach(Store.sorts, id: \.self) { Text($0.capitalized).tag($0) } }
                Toggle("Reverse", isOn: $store.reverse)
            } label: { Label(store.sort.capitalized + (store.reverse ? " ↑" : ""), systemImage: "arrow.up.arrow.down").font(.caption) }
                .menuStyle(.borderlessButton).fixedSize()
        }
        .padding(.horizontal, 12).padding(.vertical, 8).background(.bar)
    }

    private func filterChip(_ title: String, _ n: Int?, _ on: Binding<Bool>, _ color: Color, _ key: String) -> some View {
        Button { on.wrappedValue.toggle() } label: {
            HStack(spacing: 4) {
                Text(title)
                if let n, n > 0 { Text("\(n)").monospacedDigit() }
            }
            .font(.caption.weight(.medium)).foregroundStyle(on.wrappedValue ? Color.white : color)
            .padding(.horizontal, 9).padding(.vertical, 3)
            .background(on.wrappedValue ? color : color.opacity(store.hover == key ? 0.25 : 0.13), in: Capsule())
        }
        .buttonStyle(.plain).onHover { store.setHover(key, $0) }.clickable()
    }

    private var empty: some View {
        ContentUnavailableView {
            Label("No sessions", systemImage: "tray")
        } description: {
            Text(store.query.isEmpty && !store.filtersActive ? "Nothing found in ~/.claude/projects." : "Nothing matches this search and these filters.")
        } actions: {
            if !store.query.isEmpty || store.filtersActive { Button("Clear filters") { store.clearFilters() }.clickable("clearf") }
        }
    }

    private func downBanner(_ why: String) -> some View {
        HStack(alignment: .top) {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            Text(why).font(.callout).textSelection(.enabled)
            Spacer()
        }
        .padding(10).background(.orange.opacity(0.12))
    }

    @ViewBuilder private func rowMenu(_ s: Session) -> some View {
        let ids = store.selection.contains(s.id) ? Array(store.selection) : [s.id]
        Button("Resume") { Task { await store.resume(ids) } }
        Button("Fork") { Task { await store.open(ids, fork: true) } }
        Divider()
        Button(s.pinned ? "Unpin" : "Pin") { Task { await store.pin(ids) } }
        Button("Auto-tag with Claude") { Task { await store.autoTag(ids) } }
        Divider()
        Button("Move to Trash…", role: .destructive) { store.selection = Set(ids); store.confirmDelete = true }
    }

    // MARK: detail

    @ViewBuilder private var detail: some View {
        if let s = store.single {
            DetailView(store: store, session: s)
        } else if store.selection.count > 1 {
            ContentUnavailableView("\(store.selection.count) sessions selected", systemImage: "checklist",
                                   description: Text("Return resumes them all. Right-click for more."))
        } else {
            ContentUnavailableView("Select a session", systemImage: "text.bubble",
                                   description: Text("Search above, or ask Claude to find one."))
        }
    }

    // MARK: toolbar and status

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItemGroup {
            Menu {
                Picker("Sort", selection: $store.sort) {
                    ForEach(Store.sorts, id: \.self) { Text($0.capitalized).tag($0) }
                }
                Toggle("Reverse", isOn: $store.reverse)
            } label: { Label("Sort: \(store.sort)\(store.reverse ? " ↑" : "")", systemImage: "arrow.up.arrow.down") }

            Menu {
                Toggle("Running", isOn: $store.liveOnly)
                Toggle("Before last reboot", isOn: $store.preOnly)
                Toggle("Forks", isOn: $store.forksOnly)
                Toggle("Stale", isOn: $store.staleOnly)
                if !store.tagCounts.isEmpty {
                    Divider()
                    Picker("Tag", selection: $store.tagFilter) {
                        Text("Any tag").tag("")
                        ForEach(store.tagCounts, id: \.name) { Text("#\($0.name) (\($0.n))").tag($0.name) }
                    }
                }
                Divider()
                Button("Clear filters") { store.clearFilters() }
            } label: {
                Label("Filter", systemImage: store.filtersActive ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
            }

            Button { store.asking = .ask } label: { Label("Ask Claude", systemImage: "sparkles") }
                .keyboardShortcut("k", modifiers: .command).help("Find a session by describing it (⌘K)")
            Button { store.asking = .deep } label: { Label("Deep search", systemImage: "doc.text.magnifyingglass") }
                .keyboardShortcut("f", modifiers: [.command, .shift]).help("Search inside every transcript (⇧⌘F)")
        }
        ToolbarItemGroup {
            Button { Task { await store.pin(Array(store.selection)) } } label: { Label("Pin", systemImage: "star") }
                .keyboardShortcut("p", modifiers: .command).disabled(store.selection.isEmpty).help("Pin or unpin (⌘P)")
            Button { store.confirmDelete = true } label: { Label("Trash", systemImage: "trash") }
                .keyboardShortcut(.delete, modifiers: .command).disabled(store.selection.isEmpty).help("Move to Trash (⌘⌫)")
            Button { Task { await store.undo() } } label: { Label("Undo delete", systemImage: "arrow.uturn.backward") }
                .help("Restore the last deleted batch")
        }
    }

    private var statusBar: some View {
        HStack(spacing: 12) {
            switch store.conn {
            case .starting: ProgressView().controlSize(.small); Text("starting ccsd…")
            case .ok: Text("\(store.rows.count) sessions · \(store.live.count) running\(store.busyCount > 0 ? " · \(store.busyCount) working" : "")")
            case .down: Text("disconnected").foregroundStyle(.orange)
            }
            Spacer()
            if !store.flash.isEmpty { Text(store.flash).lineLimit(1).transition(.opacity) }
        }
        .font(.caption).foregroundStyle(.secondary)
        .padding(.horizontal, 12).padding(.vertical, 6).background(.bar)
        .animation(.default, value: store.flash)
    }
}

struct AskSheet: View {
    @Bindable var store: Store
    let mode: Store.AskMode

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(mode.title).font(.headline)
            TextField(mode.hint, text: $store.askDraft).textFieldStyle(.roundedBorder).onSubmit(go)
            if mode == .ask {
                Text("Sends a compact index of your sessions to Claude (Haiku). Takes 5-15 seconds.").font(.caption).foregroundStyle(.secondary)
            } else {
                Text("Reads every transcript on disk. Every word must appear.").font(.caption).foregroundStyle(.secondary)
            }
            HStack {
                if store.searching { ProgressView().controlSize(.small) }
                Spacer()
                Button("Cancel") { store.asking = nil }.keyboardShortcut(.cancelAction)
                Button("Search", action: go).keyboardShortcut(.defaultAction).disabled(store.askDraft.trimmingCharacters(in: .whitespaces).isEmpty || store.searching)
            }
        }
        .padding(18).frame(width: 460)
    }

    private func go() {
        let q = store.askDraft
        let deep = mode == .deep
        Task {
            await store.ask(q, deep: deep)
            store.asking = nil
        }
    }
}
