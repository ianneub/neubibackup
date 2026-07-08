#!/bin/bash
# Download restic binaries for embedding into the application.
# Usage: ./download-restic.sh [OS] [ARCH]
#   If OS and ARCH are provided, only download that specific binary.
#   If not provided, download all supported platforms.
# Examples:
#   ./download-restic.sh                  # Download all platforms
#   ./download-restic.sh darwin arm64     # Download only macOS arm64
#   ./download-restic.sh windows amd64    # Download only Windows amd64

set -e

RESTIC_VERSION="0.19.1"
BINARIES_DIR="$(dirname "$0")/../internal/restic"

# Create binaries directory
mkdir -p "$BINARIES_DIR"

# Function to download a single platform
download_platform() {
    local OS="$1"
    local ARCH="$2"

    # Construct download URL
    if [ "$OS" = "windows" ]; then
        FILENAME="restic_${RESTIC_VERSION}_${OS}_${ARCH}.zip"
    else
        FILENAME="restic_${RESTIC_VERSION}_${OS}_${ARCH}.bz2"
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
        return 0
    fi

    echo "  Downloading ${OS}_${ARCH}..."

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
}

# Function to create a dummy file for embed directives
create_dummy() {
    local OS="$1"
    local ARCH="$2"
    local OUTPUT_NAME="restic_${OS}_${ARCH}"

    if [ "$OS" = "windows" ]; then
        OUTPUT_NAME="${OUTPUT_NAME}.exe"
    fi

    local OUTPUT_PATH="${BINARIES_DIR}/${OUTPUT_NAME}"

    if [ ! -f "$OUTPUT_PATH" ]; then
        touch "$OUTPUT_PATH"
        echo "  Created dummy: ${OUTPUT_NAME}"
    fi
}

echo "Downloading restic $RESTIC_VERSION..."

if [ -n "$1" ] && [ -n "$2" ]; then
    # Specific platform requested
    TARGET_OS="$1"
    TARGET_ARCH="$2"

    download_platform "$TARGET_OS" "$TARGET_ARCH"

    # Create dummy files for other platforms to satisfy embed directives
    echo "Creating dummy files for other platforms..."
    if [ "$TARGET_OS" != "darwin" ] || [ "$TARGET_ARCH" != "arm64" ]; then
        create_dummy "darwin" "arm64"
    fi
    if [ "$TARGET_OS" != "darwin" ] || [ "$TARGET_ARCH" != "amd64" ]; then
        create_dummy "darwin" "amd64"
    fi
    if [ "$TARGET_OS" != "windows" ] || [ "$TARGET_ARCH" != "amd64" ]; then
        create_dummy "windows" "amd64"
    fi
else
    # Download all platforms
    PLATFORMS=(
        "darwin_arm64"
        "darwin_amd64"
        "windows_amd64"
    )

    for PLATFORM in "${PLATFORMS[@]}"; do
        OS="${PLATFORM%_*}"
        ARCH="${PLATFORM#*_}"
        download_platform "$OS" "$ARCH"
    done
fi

echo ""
echo "Restic binaries in ${BINARIES_DIR}/"
ls -la "$BINARIES_DIR/"
