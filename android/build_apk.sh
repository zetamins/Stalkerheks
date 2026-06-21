#!/bin/bash
# Build stalkerhek Android APK using gomobile.
# Prerequisites:
#   go install golang.org/x/mobile/cmd/gomobile@latest
#   gomobile init
#   Android SDK + NDK installed

set -e

echo "=== Building stalkerhek APK ==="

# 1. Build Go mobile library (AAR)
echo "Step 1: Building Go mobile library..."
cd "$(dirname "$0")/.."
gomobile bind -target=android -androidapi 21 -o android/app/libs/stalkerhek.aar ./mobile

# 2. Build Android APK
echo "Step 2: Building Android APK..."
cd android
if command -v gradlew &> /dev/null; then
    ./gradlew assembleRelease
elif command -v gradle &> /dev/null; then
    gradle assembleRelease
else
    echo "ERROR: Gradle not found. Install Android Studio or gradle."
    echo "You can also open android/ in Android Studio and build from there."
    exit 1
fi

echo ""
echo "=== Done ==="
echo "APK: android/app/build/outputs/apk/release/app-release.apk"
echo ""
echo "Install on device:"
echo "  adb install android/app/build/outputs/apk/release/app-release.apk"
