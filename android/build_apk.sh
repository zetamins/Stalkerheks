#!/bin/bash
# Build stalkerhek Android APK using Go cross-compilation.
# No gomobile required — produces a native ARM64 binary bundled as asset.

set -e
cd "$(dirname "$0")/.."

echo "=== Building stalkerhek APK ==="

# 1. Cross-compile Go binary for Android ARM64
echo "Step 1: Cross-compiling stalkerhek for Android ARM64..."
GOOS=android GOARCH=arm64 go build -o android/app/src/main/assets/stalkerhek ./cmd/stalkerhek/
mkdir -p android/app/src/main/assets
cp android/app/src/main/assets/stalkerhek android/app/src/main/assets/
echo "  Binary: $(file android/app/src/main/assets/stalkerhek | cut -d, -f1)"

# Also build for ARM32 (older devices)
echo "Step 2: Cross-compiling stalkerhek for Android ARM32..."
GOOS=android GOARCH=arm go build -o android/app/src/main/assets/stalkerhek-arm ./cmd/stalkerhek/ 2>/dev/null && \
  echo "  ARM32 binary built" || echo "  ARM32 skipped (optional)"

# 3. Build Android APK
echo "Step 3: Building APK with Gradle..."
cd android
./gradlew assembleRelease

# 4. Sign APK (v1+v2+v3)
UNSIGNED="app/build/outputs/apk/release/app-release-unsigned.apk"
SIGNED="stalkerhek-release.apk"
if [ -f "$UNSIGNED" ]; then
  echo "Step 4: Signing APK..."
  SDK="${ANDROID_HOME:-$HOME/Android/Sdk}"
  APKSIGNER=$(find "$SDK/build-tools" -name apksigner -type f 2>/dev/null | sort -r | head -1)
  KEYSTORE="${STALKERHEK_KEYSTORE:-$HOME/stalkerhek.keystore}"
  KEYPASS="${STALKERHEK_KEYPASS:-stalkerhek}"
  if [ -n "$APKSIGNER" ] && [ -f "$KEYSTORE" ]; then
    "$APKSIGNER" sign --ks "$KEYSTORE" --ks-pass "pass:$KEYPASS" --ks-key-alias stalkerhek --out "$SIGNED" "$UNSIGNED"
    echo "Signed APK: android/$SIGNED"
  else
    cp "$UNSIGNED" "$SIGNED"
    echo "Keystore not found — unsigned APK: android/$SIGNED"
  fi
fi
if command -v gradlew &> /dev/null; then
    ./gradlew assembleRelease
elif command -v gradle &> /dev/null; then
    gradle assembleRelease
else
    echo "ERROR: Gradle not found."
    echo "Install Android Studio or run: sdkmanager 'build-tools;34.0.0'"
    echo ""
    echo "Manual build:"
    echo "  1. Open android/ in Android Studio"
    echo "  2. Build → Build APK"
    echo "  3. APK at: android/app/build/outputs/apk/release/"
    exit 1
fi

echo ""
echo "=== Done ==="
echo "APK: android/app/build/outputs/apk/release/app-release.apk"
echo ""
echo "Install: adb install android/app/build/outputs/apk/release/app-release.apk"
