import SwiftUI
import WebKit

/// Renders an artifact: its local source file (no persistent storage), or for pages that exist only on
/// claude.ai (Claude Docs) the live page, which keeps a web session so you sign in once. Links leave for the browser.
struct ArtifactPreview: NSViewRepresentable {
    let file: URL?
    let remote: URL?

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let cfg = WKWebViewConfiguration()
        cfg.websiteDataStore = remote == nil ? .nonPersistent() : .default()
        let v = WKWebView(frame: .zero, configuration: cfg)
        v.navigationDelegate = context.coordinator
        v.setValue(false, forKey: "drawsBackground")
        return v
    }

    func updateNSView(_ v: WKWebView, context: Context) {
        let target = remote ?? file
        guard context.coordinator.loaded != target else { return }
        context.coordinator.loaded = target
        context.coordinator.host = remote?.host
        if let remote { v.load(URLRequest(url: remote)) }
        else if let file { v.loadFileURL(file, allowingReadAccessTo: file.deletingLastPathComponent()) }
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var loaded: URL?
        var host: String?

        func webView(_ w: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            if action.navigationType == .linkActivated, let u = action.request.url, host == nil || u.host != host {
                NSWorkspace.shared.open(u)
                decisionHandler(.cancel)
            } else {
                decisionHandler(.allow)
            }
        }
    }
}
