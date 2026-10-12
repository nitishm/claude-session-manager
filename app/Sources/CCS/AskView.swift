import SwiftUI

/// Ask a question about the selected session and get a short answer. Each send is one model call.
struct AskView: View {
    @Bindable var store: Store
    let session: Session

    private static let ideas = ["What is left to do?", "Which files changed and why?", "What did we decide?", "Where did it get stuck?"]

    private var busy: Bool { store.qBusy.contains(session.id) }
    private var history: [Store.QA] { store.qa[session.id] ?? [] }
    private var draft: Binding<String> {
        Binding(get: { store.qDraft[session.id] ?? "" }, set: { store.qDraft[session.id] = $0 })
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Ask this session").font(Theme.heading(16))
                Spacer()
                if !history.isEmpty {
                    Button("Clear") { store.clearQA(session.id) }.buttonStyle(.link).font(.caption).clickable("ask:clear")
                }
            }
            ForEach(history) { item in answer(item) }
            if busy {
                HStack(spacing: 8) { ProgressView().controlSize(.small); Text("thinking… (5-15s)").font(.callout).foregroundStyle(.secondary) }
            }
            HStack(spacing: 8) {
                TextField("Ask anything about what happened here", text: draft)
                    .textFieldStyle(.roundedBorder).onSubmit(send).disabled(busy)
                Button(action: send) { Image(systemName: "arrow.up.circle.fill").font(.title3) }
                    .buttonStyle(.plain).foregroundStyle(canSend ? Color.accentColor : Color.secondary)
                    .disabled(!canSend).clickable("ask:send", enabled: canSend).help("Ask (one model call)")
            }
            if history.isEmpty, !busy {
                HStack(spacing: 6) {
                    ForEach(Self.ideas, id: \.self) { idea in
                        Button { Task { await store.askSession(session.id, idea) } } label: {
                            Text(idea).font(.caption)
                                .padding(.horizontal, 8).padding(.vertical, 3)
                                .background(Color.accentColor.opacity(store.hover == "idea:" + idea ? 0.25 : 0.12), in: Capsule())
                        }
                        .buttonStyle(.plain).onHover { store.setHover("idea:" + idea, $0) }.clickable("idea:" + idea)
                    }
                }
            }
        }
    }

    private var canSend: Bool { !busy && !(store.qDraft[session.id] ?? "").trimmingCharacters(in: .whitespaces).isEmpty }

    private func send() {
        let q = store.qDraft[session.id] ?? ""
        Task { await store.askSession(session.id, q) }
    }

    private func answer(_ item: Store.QA) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(item.question).font(.callout.weight(.semibold)).textSelection(.enabled)
            Text(item.answer).font(.callout).foregroundStyle(item.failed ? Color.red : Color.primary).textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(10).frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }
}
