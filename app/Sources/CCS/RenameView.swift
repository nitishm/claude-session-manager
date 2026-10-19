import SwiftUI

/// Gives a session a new title. It writes the same transcript line as /rename, so claude --resume shows it too.
struct RenameSheet: View {
    @Bindable var store: Store
    let session: Session

    private var trimmed: String { store.renameDraft.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Rename session").font(Theme.heading(20))
            TextField("New title", text: $store.renameDraft)
                .textFieldStyle(.roundedBorder).onSubmit(save).disabled(store.renameBusy)
            Text("Same as /rename in Claude Code. A running session cannot be renamed here.")
                .font(.caption).foregroundStyle(.secondary)
            HStack {
                if store.renameBusy { ProgressView().controlSize(.small) }
                Spacer()
                Button("Cancel") { store.renaming = nil }.keyboardShortcut(.cancelAction)
                Button("Rename", action: save).keyboardShortcut(.defaultAction)
                    .disabled(trimmed.isEmpty || trimmed == session.displayLabel || store.renameBusy)
            }
        }
        .padding(18).frame(width: 460)
    }

    private func save() {
        guard !trimmed.isEmpty, !store.renameBusy else { return }
        Task { await store.commitRename() }
    }
}
