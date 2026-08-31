#!/usr/bin/env bash
#
# build.sh - Build the simple-file-browser binary and assemble a distribution zip.
#
# The resulting dist/simple-file-browser/ directory contains:
#   - simple-file-browser            (statically-linked binary; template.html is embedded)
#   - simple-file-browser.service    (systemd unit)
#   - simple-file-browser.env        (sample environment file)
#   - install.sh            (installer script)
#
# Usage:
#   ./build.sh                          # build for host OS/arch
#   GOOS=linux GOARCH=amd64 ./build.sh  # cross-compile for Linux/amd64
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PACKAGE_NAME="simple-file-browser"
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
cp "$SCRIPT_DIR/simple-file-browser.service"     "$DIST_DIR/simple-file-browser.service"
cp "$SCRIPT_DIR/simple-file-browser.env"         "$DIST_DIR/simple-file-browser.env"
cp "$SCRIPT_DIR/install.sh"             "$DIST_DIR/install.sh"

chmod +x "$DIST_DIR/$PACKAGE_NAME" "$DIST_DIR/install.sh"

rm -f "$ARCHIVE_PATH"
(cd "$STAGING_DIR" && zip -r "$ARCHIVE_PATH" "$PACKAGE_NAME")

echo ">> Build complete."
echo "   Archive : $ARCHIVE_PATH"
echo "   Unpacked: $DIST_DIR"
