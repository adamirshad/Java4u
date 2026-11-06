#!/bin/sh
# Builds dist/java4u-macos.dmg: a Java4U.app (one universal binary for Intel and Apple
# Silicon) next to a link to Applications, so it installs by drag and drop.
# Runs on macOS after the darwin builds are in dist/ and `go run ./tools/mkicon` has run:
#   sh packaging/macos/build-dmg.sh 1.2.3
set -eu

version="$1"
root="$(mktemp -d)"
app="$root/Java4U.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

for bin in java4u-gui java4u; do
  lipo -create "dist/java4u-darwin-amd64/$bin" "dist/java4u-darwin-arm64/$bin" \
    -output "$app/Contents/MacOS/$bin"
  chmod 755 "$app/Contents/MacOS/$bin"
done
iconutil -c icns build/java4u.iconset -o "$app/Contents/Resources/java4u.icns"
sed "s/@VERSION@/$version/g" packaging/macos/Info.plist > "$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist"

# Ad-hoc signature: required to run on Apple Silicon. It is not a Developer ID, so
# Gatekeeper asks the player to confirm the first launch.
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict "$app"

ln -s /Applications "$root/Applications"
rm -f dist/java4u-macos.dmg
hdiutil create -volname "Java4U" -srcfolder "$root" -fs HFS+ -format UDZO -ov dist/java4u-macos.dmg
rm -rf "$root"
echo "wrote dist/java4u-macos.dmg"
