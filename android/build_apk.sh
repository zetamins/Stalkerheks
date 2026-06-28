#!/bin/bash
# Build stalkerhek Android APK using Go cross-compilation.
# No gomobile required — produces a native ARM64 binary bundled as asset.

set -e
cd "$(dirname "$0")/.."

echo "=== Building stalkerhek APK ==="

# 1. Locate the Android NDK (needed for the cgo c-shared JNI engine).
#    The in-process engine (libstalkerhek_engine.so) is the primary path used by
#    EngineBridge.loadLibrary; it MUST exist for every ABI in abiFilters, or
#    32-bit / x86 devices fall back to exec and fail. The exec binary
#    (libstalkerhek.so) is built alongside as a fallback.
NDK="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-}}"
if [ -z "$NDK" ]; then
  SDK="${ANDROID_HOME:-$HOME/Android/Sdk}"
  NDK=$(find "$SDK/ndk" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | sort -r | head -1)
fi
if [ -z "$NDK" ] || [ ! -d "$NDK" ]; then
  echo "ERROR: Android NDK not found. Set ANDROID_NDK_HOME (or install via sdkmanager 'ndk;<version>')."
  exit 1
fi
HOST_TAG="linux-x86_64"
[ "$(uname)" = "Darwin" ] && HOST_TAG="darwin-x86_64"
TOOLCHAIN="$NDK/toolchains/llvm/prebuilt/$HOST_TAG/bin"
API=26  # must match minSdk in app/build.gradle.kts
JNILIBS="android/app/src/main/jniLibs"

# ABI table: "abi goarch clang-target [GOARM]"
ABIS=(
  "arm64-v8a   arm64 aarch64-linux-android"
  "armeabi-v7a arm   armv7a-linux-androideabi 7"
  "x86         386   i686-linux-android"
  "x86_64      amd64 x86_64-linux-android"
)

echo "Step 1: Cross-compiling engine (c-shared JNI) + exec binary per ABI..."
echo "  NDK: $NDK  (API $API)"
for entry in "${ABIS[@]}"; do
  read -r ABI GOARCH TARGET GOARM <<< "$entry"
  CC="$TOOLCHAIN/${TARGET}${API}-clang"
  if [ ! -x "$CC" ]; then
    echo "  [$ABI] SKIP — compiler not found: $CC"
    continue
  fi
  mkdir -p "$JNILIBS/$ABI"

  # Primary: in-process JNI engine (cgo, c-shared).
  CGO_ENABLED=1 CC="$CC" GOOS=android GOARCH="$GOARCH" GOARM="$GOARM" \
    go build -buildmode=c-shared \
      -o "$JNILIBS/$ABI/libstalkerhek_engine.so" ./cmd/stalkerhek/ \
    && echo "  [$ABI] engine: $(file -b "$JNILIBS/$ABI/libstalkerhek_engine.so" | cut -d, -f1-2)" \
    || { echo "  [$ABI] ENGINE BUILD FAILED"; exit 1; }

  # Fallback: standalone exec binary, packaged as lib*.so so it survives install.
  CGO_ENABLED=1 CC="$CC" GOOS=android GOARCH="$GOARCH" GOARM="$GOARM" \
    go build -o "$JNILIBS/$ABI/libstalkerhek.so" ./cmd/stalkerhek/ \
    && echo "  [$ABI] binary: $(file -b "$JNILIBS/$ABI/libstalkerhek.so" | cut -d, -f1-2)" \
    || echo "  [$ABI] exec binary skipped (optional)"
done

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
