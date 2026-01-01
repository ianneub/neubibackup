#!/bin/bash
# Build a development app bundle for testing Location Services permission
# Usage: ./scripts/build-dev-app.sh

set -e

echo "Building NeubiBackup..."
go build -o neubibackup .

echo "Creating app bundle structure..."
rm -rf NeubiBackup.app
mkdir -p NeubiBackup.app/Contents/MacOS
mkdir -p NeubiBackup.app/Contents/Resources

cp neubibackup NeubiBackup.app/Contents/MacOS/

echo "Creating Info.plist with Location permission..."
cat > NeubiBackup.app/Contents/Info.plist << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>neubibackup</string>
    <key>CFBundleIdentifier</key>
    <string>com.neubibackup.app</string>
    <key>CFBundleName</key>
    <string>NeubiBackup</string>
    <key>CFBundleVersion</key>
    <string>dev</string>
    <key>CFBundleShortVersionString</key>
    <string>dev</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSLocationWhenInUseUsageDescription</key>
    <string>NeubiBackup uses your location to detect the current WiFi network name (SSID) for the allowed_ssids feature. This allows backups to run only on specific networks you configure.</string>
</dict>
</plist>
EOF

echo "Signing app bundle (ad-hoc)..."
codesign --force --deep --sign - NeubiBackup.app

echo ""
echo "✅ App bundle created: NeubiBackup.app"
echo ""
echo "To test:"
echo "  1. Run: open NeubiBackup.app"
echo "  2. macOS should prompt for Location permission when SSID is checked"
echo "  3. Check .dev-data/app.log for SSID detection logs"
echo ""
echo "If you need to reset Location permission:"
echo "  tccutil reset Location com.neubibackup.app"
