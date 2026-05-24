#!/bin/bash
# Build modernpath CLI for all platforms

set -e

VERSION="${1:-dev}"
OUTPUT_DIR="dist"
BINARY_NAME="modernpath"

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

# Clean and create output directory
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

echo "Building modernpath CLI v${VERSION}..."
echo ""

# Define platforms: "GOOS/GOARCH/suffix"
platforms=(
    "darwin/amd64/"
    "darwin/arm64/"
    "linux/amd64/"
    "linux/arm64/"
    "windows/amd64/.exe"
)

for platform in "${platforms[@]}"; do
    IFS='/' read -r os arch suffix <<< "$platform"
    
    # Skip Windows builds if zip is not available (e.g., in Docker)
    if [ "$os" = "windows" ] && ! command -v zip &> /dev/null; then
        echo "Skipping ${os}/${arch} (zip not available)..."
        continue
    fi
    
    binary_name="${BINARY_NAME}${suffix}"
    archive_name="${BINARY_NAME}-${os}-${arch}"
    
    echo "Building ${os}/${arch}..."
    
    # Build binary
    GOOS=$os GOARCH=$arch go build -ldflags="-s -w -X github.com/modernpath/cli/cmd.Version=${VERSION}" -o "${OUTPUT_DIR}/${binary_name}" .
    
    # Create archive
    if [ "$os" = "windows" ]; then
        (cd "$OUTPUT_DIR" && zip "${archive_name}.zip" "${binary_name}")
        rm "${OUTPUT_DIR}/${binary_name}"
        archive_path="${OUTPUT_DIR}/${archive_name}.zip"
    else
        (cd "$OUTPUT_DIR" && tar -czf "${archive_name}.tar.gz" "${binary_name}")
        rm "${OUTPUT_DIR}/${binary_name}"
        archive_path="${OUTPUT_DIR}/${archive_name}.tar.gz"
    fi
    
    # Create checksum
    if command -v sha256sum &> /dev/null; then
        sha256sum "$archive_path" > "${archive_path}.sha256"
    elif command -v shasum &> /dev/null; then
        shasum -a 256 "$archive_path" > "${archive_path}.sha256"
    fi
    
    echo "  -> $archive_path"
done

echo ""
echo "Build complete! Binaries in ${OUTPUT_DIR}/"
ls -lh "$OUTPUT_DIR"
