import SwiftUI

/// Pages Claude published during the session, each with a link. Read from the transcript, no model call.
struct ArtifactStrip: View {
    @Bindable var store: Store
    let artifacts: [Artifact]

    var body: some View {
        FoldSection(store: store, id: "artifacts", symbol: "square.on.square", title: "Artifacts", count: artifacts.count, summary: artifacts.last?.title ?? "") {
            VStack(alignment: .leading, spacing: 8) { ForEach(artifacts) { a in row(a) } }
        }
    }

    private func row(_ a: Artifact) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "doc.richtext").foregroundStyle(Theme.slate).frame(width: 18)
            VStack(alignment: .leading, spacing: 1) {
                if let u = URL(string: a.url) {
                    Link(a.title, destination: u).font(.callout.weight(.semibold)).lineLimit(1).clickable()
                } else {
                    Text(a.title).font(.callout.weight(.semibold)).lineLimit(1)
                }
                Text("published \(a.at.formatted(date: .abbreviated, time: .shortened))").font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            iconButton("link", "Copy the link", "art:copy:" + a.id) { store.copy(a.url, note: "copied the artifact link") }
            if let f = a.file {
                iconButton("folder", "Show the source file in Finder", "art:file:" + a.id) { store.reveal(URL(fileURLWithPath: f)) }
            }
            iconButton("arrow.up.right.square", "Open in the browser", "art:open:" + a.id) {
                if let u = URL(string: a.url) { NSWorkspace.shared.open(u) }
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 8)
        .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    private func iconButton(_ symbol: String, _ tip: String, _ key: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) { Image(systemName: symbol).frame(width: 18, height: 18) }
            .buttonStyle(.plain).help(tip).clickable()
    }
}
