#!/usr/bin/env bash
#
# build.sh - Build the upload2dir binary and assemble a distribution zip.
#
# The resulting dist/upload2dir/ directory contains:
#   - upload2dir            (statically-linked binary; template.html is embedded)
#   - upload2dir.service    (systemd unit)
#   - upload2dir.env        (sample environment file)
#   - install.sh            (installer script)
#
# Usage:
#   ./build.sh                          # build for host OS/arch
#   GOOS=linux GOARCH=amd64 ./build.sh  # cross-compile for Linux/amd64
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PACKAGE_NAME="upload2dir"
STAGING_DIR="$SCRIPT_DIR/dist"
DIST_DIR="$STAGING_DIR/$PACKAGE_NAME"
ARCHIVE_PATH="$SCRIPT_DIR/${PACKAGE_NAME}.zip"

echo ">> Building $PACKAGE_NAME (GOOS=${GOOS:-$(go env GOOS)} GOARCH=${GOARCH:-$(go env GOARCH)}) ..."
# CGO disabled so the binary is fully static and portable across distros.
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$SCRIPT_DIR/$PACKAGE_NAME" ./...

echo ">> Assembling distribution in $DIST_DIR ..."
rm -rf "$STAGING_DIR"
mkdir -p "$DIST_DIR"

cp "$SCRIPT_DIR/$PACKAGE_NAME"          "$DIST_DIR/$PACKAGE_NAME"
cp "$SCRIPT_DIR/upload2dir.service"     "$DIST_DIR/upload2dir.service"
cp "$SCRIPT_DIR/upload2dir.env"         "$DIST_DIR/upload2dir.env"
cp "$SCRIPT_DIR/install.sh"             "$DIST_DIR/install.sh"

chmod +x "$DIST_DIR/$PACKAGE_NAME" "$DIST_DIR/install.sh"

rm -f "$ARCHIVE_PATH"
(cd "$STAGING_DIR" && zip -r "$ARCHIVE_PATH" "$PACKAGE_NAME")

echo ">> Build complete."
echo "   Archive : $ARCHIVE_PATH"
echo "   Unpacked: $DIST_DIR"
