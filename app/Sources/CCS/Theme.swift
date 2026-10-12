import SwiftUI

/// One place for the app's look: palette, icons per kind of work, and the card style.
enum Theme {
    static let violet = Color(red: 0.486, green: 0.227, blue: 0.929) // same hue as the TUI title bar

    static func kind(_ k: String?) -> (symbol: String, color: Color, label: String) {
        switch k {
        case "research": return ("magnifyingglass", .cyan, "Research")
        case "diagnose": return ("ladybug.fill", .orange, "Diagnose")
        case "build": return ("hammer.fill", .blue, "Build")
        case "fix": return ("wrench.and.screwdriver.fill", .teal, "Fix")
        case "test": return ("checkmark.seal.fill", .green, "Test")
        case "decide": return ("arrow.triangle.branch", .purple, "Decision")
        case "blocked": return ("exclamationmark.octagon.fill", .red, "Blocked")
        case "ship": return ("paperplane.fill", .indigo, "Ship")
        case "setup": return ("gearshape.fill", .gray, "Setup")
        default: return ("circle.fill", .secondary, "")
        }
    }

    static func status(_ s: String) -> (symbol: String, color: Color) {
        switch s {
        case "blocked": return ("exclamationmark.triangle.fill", .orange)
        case "done": return ("checkmark.circle.fill", .green)
        case "idle": return ("moon.zzz.fill", .secondary)
        default: return ("bolt.circle.fill", violet)
        }
    }

    /// A stable colour per tag, so the same tag always looks the same.
    static func tag(_ name: String) -> Color {
        let palette: [Color] = [.blue, .purple, .pink, .orange, .teal, .indigo, .green, .mint]
        let h = name.unicodeScalars.reduce(5381) { ($0 &* 33) &+ Int($1.value) }
        return palette[abs(h) % palette.count]
    }
}

extension View {
    /// A raised, rounded surface. A tint colours its border and wash.
    func card(tint: Color? = nil, padding: CGFloat = 14) -> some View {
        self.padding(padding)
            .background {
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .fill(Color(nsColor: .controlBackgroundColor))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).fill((tint ?? .clear).opacity(0.07)))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).stroke((tint ?? Color.primary).opacity(tint == nil ? 0.08 : 0.3)))
                    .shadow(color: .black.opacity(0.06), radius: 6, y: 2)
            }
    }
}

/// A circular progress ring with a label in the middle.
struct Ring: View {
    let value: Double
    let color: Color
    let label: String
    var size: CGFloat = 64

    var body: some View {
        ZStack {
            Circle().stroke(color.opacity(0.18), lineWidth: 7)
            Circle().trim(from: 0, to: max(0.001, min(1, value)))
                .stroke(color, style: StrokeStyle(lineWidth: 7, lineCap: .round))
                .rotationEffect(.degrees(-90))
            Text(label).font(.system(.callout, design: .rounded).weight(.bold)).foregroundStyle(color)
        }
        .frame(width: size, height: size)
    }
}

/// A number with a label and an icon.
struct StatTile: View {
    let symbol: String
    let value: String
    let label: String
    let color: Color

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: symbol).font(.title3).foregroundStyle(color)
                .frame(width: 34, height: 34).background(color.opacity(0.14), in: RoundedRectangle(cornerRadius: 9, style: .continuous))
            VStack(alignment: .leading, spacing: 0) {
                Text(value).font(.system(.title3, design: .rounded).weight(.bold))
                Text(label).font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .card(padding: 10)
    }
}
