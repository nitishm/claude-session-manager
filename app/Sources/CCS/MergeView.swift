import SwiftUI

/// Merge two selected sessions into a new one. A model writes a handoff from both; a new session starts from it.
struct MergeSheet: View {
    @Bindable var store: Store
    let pair: Store.MergePair

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Merge sessions").font(Theme.heading(20))
            VStack(spacing: 6) {
                row(pair.a, "A")
                row(pair.b, "B")
            }
            Text("The new session starts in the lead session's folder. It gets a handoff of both, and the full transcripts stay on disk for detail.")
                .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            TextField("Extra guidance (optional), e.g. keep B's retry fix, drop its logging rewrite", text: $store.mergeGuidance, axis: .vertical)
                .textFieldStyle(.roundedBorder).lineLimit(1...3).disabled(store.mergeBusy)
            if store.mergeBusy {
                HStack(spacing: 8) { ProgressView().controlSize(.small); Text("reading both sessions… (20-60s)").font(.callout).foregroundStyle(.secondary) }
            }
            if !store.mergeError.isEmpty { Text(store.mergeError).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            if let r = store.mergeReply { preview(r) }
            HStack {
                Spacer()
                Button("Cancel") { store.merging = nil }.keyboardShortcut(.cancelAction)
                if store.mergeReply == nil {
                    Button("Write handoff") { Task { await store.runMerge() } }
                        .keyboardShortcut(.defaultAction).disabled(store.mergeBusy)
                } else {
                    Button("Redo") { Task { await store.runMerge() } }.disabled(store.mergeBusy)
                    Button("Open new session") { Task { await store.openMerge() } }
                        .keyboardShortcut(.defaultAction).buttonStyle(.borderedProminent)
                }
            }
        }
        .padding(20).frame(width: 640)
    }

    private func row(_ s: Session, _ tag: String) -> some View {
        let isLead = store.mergeLead == s.id
        return Button { store.mergeLead = s.id; store.mergeReply = nil } label: {
            HStack(spacing: 10) {
                Image(systemName: isLead ? "largecircle.fill.circle" : "circle").foregroundStyle(isLead ? Color.accentColor : Color.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(s.displayLabel).lineLimit(1).font(.callout.weight(.semibold))
                        if s.fork { Image(systemName: "arrow.triangle.branch").font(.caption).foregroundStyle(.secondary) }
                        if isLead { Text("lead").font(.caption2.weight(.semibold)).foregroundStyle(Color.accentColor).padding(.horizontal, 6).background(Color.accentColor.opacity(0.14), in: Capsule()) }
                    }
                    Text("\(s.dir) · \(s.prompts) prompts · active \(ago(s.mtime)) ago").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(10).background(Color.primary.opacity(isLead ? 0.08 : 0.04), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        }
        .buttonStyle(.plain).disabled(store.mergeBusy).clickable("merge:" + tag)
    }

    private func preview(_ r: API.MergeReply) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(r.common > 0 ? "They share their first \(r.common) turns, then diverge (\(r.leadN) and \(r.otherN) more)." : "These sessions share no history.")
                .font(.caption).foregroundStyle(.secondary)
            ScrollView {
                Text(r.handoff).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(10)
            }
            .frame(height: 280).background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            Text("Saved at \(r.file)").font(.caption2).foregroundStyle(.tertiary).textSelection(.enabled)
        }
    }
}
