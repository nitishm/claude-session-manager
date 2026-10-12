#!/bin/sh
# Builds build/CCS.app with plain swiftc (no Xcode needed) and bundles ccsd inside it.
#   ./build.sh          build the app
#   ./build.sh run      build, then open it
set -eu
cd "$(dirname "$0")"
APP=build/CCS.app
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp AppIcon.icns "$APP/Contents/Resources/AppIcon.icns" # regenerate with make-icon.swift

# the daemon the app launches; built from the Go module next door
(cd ../go && go build -o "../app/$APP/Contents/MacOS/ccsd" ./cmd/ccsd)

swiftc -parse-as-library -swift-version 5 -O \
  -framework SwiftUI -framework AppKit \
  Sources/CCS/*.swift -o "$APP/Contents/MacOS/CCS"

cat > "$APP/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>CCS</string>
<key>CFBundleDisplayName</key><string>Claude Sessions</string>
<key>CFBundleIdentifier</key><string>dev.nitishm.ccs</string>
<key>CFBundleExecutable</key><string>CCS</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.1.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>14.0</string>
<key>NSPrincipalClass</key><string>NSApplication</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
EOF

codesign --force --deep -s - "$APP" >/dev/null 2>&1 || echo "warning: ad-hoc codesign failed"
echo "built $APP"
[ "${1:-}" = run ] && open "$APP"
exit 0
