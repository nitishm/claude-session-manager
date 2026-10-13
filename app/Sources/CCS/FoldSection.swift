import SwiftUI

/// A titled section with a chevron that collapses it. Lists of more than three rows start collapsed, so a long
/// list does not push the rest of the page down or slow the scroll. The choice is kept in the Store.
struct FoldSection<Content: View>: View {
    @Bindable var store: Store
    let id: String
    let symbol: String
    let title: String
    let count: Int
    let summary: String
    @ViewBuilder var content: () -> Content

    private var longList: Bool { count > 3 }
    private var open: Bool { longList ? store.collapsed.contains("open:" + id) : !store.collapsed.contains(id) }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button { withAnimation(.easeInOut(duration: 0.15)) { store.toggle(longList ? "open:" + id : id) } } label: {
                HStack(spacing: 8) {
                    Image(systemName: "chevron.right").font(.caption.bold()).rotationEffect(.degrees(open ? 90 : 0)).frame(width: 12)
                    Image(systemName: symbol).foregroundStyle(Theme.slate)
                    Text(title).font(Theme.heading(16))
                    Text("\(count)").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                        .padding(.horizontal, 7).padding(.vertical, 1).background(Color.primary.opacity(0.08), in: Capsule())
                    if !open { Text(summary).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain).help(open ? "Collapse" : "Expand")
            if open { content() }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
