#!/bin/sh
# Builds build/CCS.app with plain swiftc (no Xcode needed) and bundles ccsd inside it.
#   ./build.sh          build the app
#   ./build.sh run      build, then open it
#   ./build.sh install  build, then copy it to ~/Applications (quits a running copy first) and open it
set -eu
cd "$(dirname "$0")"
APP=build/CCS.app
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp AppIcon.icns "$APP/Contents/Resources/AppIcon.icns" # regenerate with make-icon.swift

# the daemon the app launches; built from the Go module next door
(cd ../go && go build -o "../app/$APP/Contents/MacOS/ccsd" ./cmd/ccsd)

# the TUI, so "In TUI" can open it when none is running
(cd ../go && go build -o "../app/$APP/Contents/MacOS/ccs-go" .)

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

# A stable identity (see make-signing-cert.sh) keeps macOS permission grants across rebuilds; ad-hoc does not.
ID=-
security find-identity -p codesigning 2>/dev/null | grep -q "CCS Local Signing" && ID="CCS Local Signing"
codesign --force --deep -s "$ID" "$APP" >/dev/null 2>&1 || echo "warning: codesign with '$ID' failed"
[ "$ID" = - ] && echo "note: ad-hoc signed, so macOS will re-ask for folder access after each rebuild (run make-signing-cert.sh once)"
echo "built $APP"
[ "${1:-}" = run ] && open "$APP"
if [ "${1:-}" = install ]; then
  DEST="$HOME/Applications"
  mkdir -p "$DEST"
  pkill -x CCS 2>/dev/null || true
  pkill -f "CCS.app/Contents/.*ccsd" 2>/dev/null || true
  sleep 1
  rm -rf "$DEST/CCS.app"
  cp -R "$APP" "$DEST/CCS.app"
  echo "installed $DEST/CCS.app"
  open "$DEST/CCS.app"
fi
exit 0
