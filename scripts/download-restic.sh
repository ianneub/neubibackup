#!/bin/bash
# Download restic binaries for embedding into the application.
# This script downloads restic 0.18.1 for all supported platforms.

set -e

RESTIC_VERSION="0.18.1"
BINARIES_DIR="$(dirname "$0")/../internal/restic"

# Create binaries directory
mkdir -p "$BINARIES_DIR"

# Platforms to download
PLATFORMS=(
    "darwin_amd64"
    "darwin_arm64"
    "windows_amd64"
)

echo "Downloading restic $RESTIC_VERSION..."

for PLATFORM in "${PLATFORMS[@]}"; do
    OS="${PLATFORM%_*}"
    ARCH="${PLATFORM#*_}"

    # Construct download URL
    if [ "$OS" = "windows" ]; then
        FILENAME="restic_${RESTIC_VERSION}_${OS}_${ARCH}.zip"
        BINARY_NAME="restic.exe"
    else
        FILENAME="restic_${RESTIC_VERSION}_${OS}_${ARCH}.bz2"
        BINARY_NAME="restic"
    fi

    URL="https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/${FILENAME}"
    OUTPUT_NAME="restic_${OS}_${ARCH}"

    if [ "$OS" = "windows" ]; then
        OUTPUT_NAME="${OUTPUT_NAME}.exe"
    fi

    OUTPUT_PATH="${BINARIES_DIR}/${OUTPUT_NAME}"

    # Skip if already downloaded
    if [ -f "$OUTPUT_PATH" ]; then
        echo "  ✓ ${OUTPUT_NAME} already exists, skipping"
        continue
    fi

    echo "  Downloading ${PLATFORM}..."

    # Download and extract
    if [ "$OS" = "windows" ]; then
        # Windows: download zip and extract
        TEMP_ZIP=$(mktemp)
        TEMP_DIR=$(mktemp -d)
        curl -sL "$URL" -o "$TEMP_ZIP"
        unzip -q -o "$TEMP_ZIP" -d "$TEMP_DIR"
        # Find the exe file (handles versioned names like restic_0.18.1_windows_amd64.exe)
        EXTRACTED_EXE=$(find "$TEMP_DIR" -name "*.exe" | head -1)
        mv "$EXTRACTED_EXE" "$OUTPUT_PATH"
        rm -rf "$TEMP_ZIP" "$TEMP_DIR"
    else
        # macOS/Linux: download bz2 and decompress
        curl -sL "$URL" | bunzip2 > "$OUTPUT_PATH"
        chmod +x "$OUTPUT_PATH"
    fi

    echo "  ✓ ${OUTPUT_NAME}"
done

echo ""
echo "All restic binaries downloaded to ${BINARIES_DIR}/"
ls -la "$BINARIES_DIR/"
