#!/bin/bash
# Build script for koha-rfid-go
# Cross-compiles for Linux (amd64) and Windows (amd64)
#
# Usage:
#   ./build.sh              # builds both linux and windows
#   ./build.sh linux        # linux only
#   ./build.sh windows      # windows only
#   ./build.sh clean        # remove all build artifacts
#
# Output in ./build/<os>/ subdirectories

set -e

PROJECT="koha-rfid"
VERSION="${VERSION:-1.0.0}"
BUILD_DIR="./build"
CMDS=("cmd/program" "cmd/scan")

# Source the project root
cd "$(dirname "$0")"

die() { echo "ERROR: $*" >&2; exit 1; }

# ─── clean ───────────────────────────────────────────
if [ "$1" = "clean" ]; then
    echo "Cleaning build artifacts ..."
    rm -rf "$BUILD_DIR"
    echo "Done."
    exit 0
fi

# ─── detect target platforms ─────────────────────────
TARGETS=()
if [ "$#" -eq 0 ] || [ "$1" = "all" ]; then
    TARGETS=("linux" "windows")
else
    for arg; do TARGETS+=("$arg"); done
fi

echo "Building $PROJECT version $VERSION"
echo "Targets: ${TARGETS[*]}"
echo ""

# ─── build ────────────────────────────────────────────
for os in "${TARGETS[@]}"; do
    case "$os" in
        linux)   GOOS="linux"   GOARCH="amd64" EXT=""   ;;
        windows) GOOS="windows" GOARCH="amd64" EXT=".exe" ;;
        *)       die "unsupported target: $os (use linux or windows)" ;;
    esac

    OUTDIR="$BUILD_DIR/$os"
    mkdir -p "$OUTDIR"

    for cmd in "${CMDS[@]}"; do
        name=$(basename "$cmd")
        output="$OUTDIR/${name}${EXT}"

        echo "  $os/$GOARCH: $cmd → $output"
        GOOS="$GOOS" GOARCH="$GOARCH" go build \
            -ldflags "-X main.version=$VERSION" \
            -o "$output" \
            "./$cmd"
    done

    # Also build the main koha-rfid binary (HTTP server)
    output="$OUTDIR/${PROJECT}${EXT}"
    echo "  $os/$GOARCH: . → $output"
    GOOS="$GOOS" GOARCH="$GOARCH" go build \
        -ldflags "-X main.version=$VERSION" \
        -o "$output" \
        "."
done

echo ""
echo "All builds finished. Artifacts:"
find "$BUILD_DIR" -type f -exec ls -lh {} \;
