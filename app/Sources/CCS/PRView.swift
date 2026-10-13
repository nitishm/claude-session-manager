import SwiftUI

/// Every pull request the session linked, newest first, each with its live state.
struct PRStrip: View {
    @Bindable var store: Store
    let prs: [PRInfo]

    private var summary: String {
        let merged = prs.filter { $0.pr?.state == "MERGED" }.count
        let open = prs.filter { $0.pr?.state == "OPEN" }.count
        return [open > 0 ? "\(open) open" : nil, merged > 0 ? "\(merged) merged" : nil].compactMap { $0 }.joined(separator: ", ")
    }

    var body: some View {
        FoldSection(store: store, id: "prs", symbol: "arrow.triangle.pull", title: "Pull requests", count: prs.count, summary: summary) {
            VStack(alignment: .leading, spacing: 8) { ForEach(prs) { p in row(p) } }
        }
    }

    private func stateColor(_ p: PRState) -> Color {
        p.state == "MERGED" ? .purple : (p.state == "CLOSED" || p.checks == "failing" || p.review == "CHANGES_REQUESTED") ? .red : .green
    }

    private func row(_ i: PRInfo) -> some View {
        let color = i.pr.map(stateColor) ?? .secondary
        return HStack(spacing: 10) {
            Image(systemName: i.pr?.state == "MERGED" ? "arrow.triangle.merge" : "arrow.triangle.pull").foregroundStyle(color).frame(width: 18)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text("#\(i.number)").font(.callout.weight(.semibold)).foregroundStyle(color)
                    if let t = i.pr?.title, !t.isEmpty { Text(t).font(.callout).lineLimit(1) }
                }
                HStack(spacing: 6) {
                    if let p = i.pr {
                        pill(p.draft ? "draft" : p.state.lowercased(), color)
                        switch p.checks {
                        case "failing": pill("\(p.failed) checks failing", .red)
                        case "pending": pill("\(p.pending) checks running", .orange)
                        case "passing": pill("checks pass", .green)
                        default: EmptyView()
                        }
                        switch p.review {
                        case "APPROVED": pill("approved", .green)
                        case "CHANGES_REQUESTED": pill("changes requested", .red)
                        case "REVIEW_REQUIRED": pill("needs review", .orange)
                        default: EmptyView()
                        }
                    } else if let e = i.err, !e.isEmpty {
                        Text("state unavailable").font(.caption).foregroundStyle(.secondary).help(e)
                    }
                    Text("linked \(i.at.formatted(date: .abbreviated, time: .shortened))").font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            if !i.url.isEmpty, let u = URL(string: i.url) {
                Button { store.copy(i.url, note: "copied the PR link") } label: { Image(systemName: "link").frame(width: 18, height: 18) }
                    .buttonStyle(.plain).help("Copy the link").clickable()
                Button { NSWorkspace.shared.open(u) } label: { Image(systemName: "arrow.up.right.square").frame(width: 18, height: 18) }
                    .buttonStyle(.plain).help("Open in the browser").clickable()
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 8)
        .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    private func pill(_ text: String, _ color: Color) -> some View {
        Text(text).font(.caption2.weight(.semibold)).foregroundStyle(color)
            .padding(.horizontal, 7).padding(.vertical, 1).background(color.opacity(0.14), in: Capsule())
    }
}
