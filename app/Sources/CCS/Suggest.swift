import Foundation

/// Questions worth asking about this session, built from what is already known about it. No model call.
/// Specific ones come first (a failed PR check, an open step, a named file); generic ones fill the rest.
func suggestedQuestions(session: Session, brief: BriefData?, facts: Facts?, events: [TLEvent], repo: RepoState?) -> [String] {
    func short(_ s: String, _ n: Int) -> String {
        let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
        return t.count > n ? String(t.prefix(n - 1)) + "…" : t
    }
    var out: [String] = []
    func add(_ q: String) { if !out.contains(q) { out.append(q) } }

    if let pr = repo?.pr {
        if pr.failed > 0 { add("Why are the PR checks failing?") }
        else if pr.review == "CHANGES_REQUESTED" { add("What changes did the reviewer ask for?") }
    }
    if let b = brief {
        if b.status == "blocked" { add("What is blocking this, and what would unblock it?") }
        if let open = b.steps.first(where: { $0.state != "done" }) { add("What is left on: \(short(open.text, 48))?") }
        if let d = b.steps.first(where: { $0.kind == "decide" }) { add("What did we decide about \(short(d.text, 44))?") }
    }
    if events.contains(where: { ($0.errors ?? 0) > 0 || $0.kind == "api-error" }) { add("Which commands failed, and why?") }
    if let c = events.last(where: { $0.kind == "commit" }) { add("What did the commit \"\(short(c.text, 40))\" change?") }
    if let f = facts?.files.filter({ $0.edits > 0 }).max(by: { $0.edits < $1.edits }) {
        add("Why did we change \((f.rel as NSString).lastPathComponent)?")
    }
    if let r = repo, r.dirty > 0 { add("What is uncommitted in the working copy?") }
    if !session.pr.isEmpty || repo?.pr != nil { add("What is the PR for, and where does it stand?") }
    for g in ["What is left to do?", "What did we decide?", "Where did it get stuck?", "Which files changed and why?"] { add(g) }
    return Array(out.prefix(4))
}
