#!/usr/bin/env bash
#
# install.sh - Install upload2dir as a systemd service.
#
# This script:
#   1. Copies the upload2dir binary to /usr/local/bin/
#   2. Installs the systemd service unit under /etc/systemd/system/
#   3. Installs (or preserves) /etc/upload2dir.env
#   4. Creates the served root directory if it does not exist
#   5. Reloads systemd, enables and (re)starts the service
#
# Run from the extracted dist/ directory:
#
#   sudo ./install.sh
#
set -euo pipefail

BIN_NAME="upload2dir"
BIN_DEST="/usr/local/bin/${BIN_NAME}"
SERVICE_NAME="upload2dir.service"
SERVICE_DEST="/etc/systemd/system/${SERVICE_NAME}"
ENV_DEST="/etc/upload2dir.env"

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: this installer must be run as root (try: sudo ./install.sh)" >&2
    exit 1
fi

for f in "$BIN_NAME" "$SERVICE_NAME" "upload2dir.env"; do
    if [ ! -f "$SRC_DIR/$f" ]; then
        echo "Error: required file '$f' not found in $SRC_DIR" >&2
        exit 1
    fi
done

echo ">> Installing binary -> $BIN_DEST"
install -m 0755 "$SRC_DIR/$BIN_NAME" "$BIN_DEST"

echo ">> Installing systemd unit -> $SERVICE_DEST"
install -m 0644 "$SRC_DIR/$SERVICE_NAME" "$SERVICE_DEST"

# Preserve an existing environment file to avoid overwriting a
# customised configuration; save the previous file as a timestamped
# backup and install the fresh copy so new keys become visible.
if [ -f "$ENV_DEST" ]; then
    BACKUP="${ENV_DEST}.bak.$(date +%Y%m%d%H%M%S)"
    echo ">> Backing up existing env file -> $BACKUP"
    cp -p "$ENV_DEST" "$BACKUP"
fi
echo ">> Installing env file  -> $ENV_DEST"
install -m 0644 "$SRC_DIR/upload2dir.env" "$ENV_DEST"

# Ensure the served root exists; if the operator changes HOME in
# upload2dir.env they are responsible for creating that directory.
DEFAULT_ROOT="/srv/upload2dir"
if [ ! -d "$DEFAULT_ROOT" ]; then
    echo ">> Creating default root -> $DEFAULT_ROOT"
    install -d -m 0755 "$DEFAULT_ROOT"
fi

echo ">> Reloading systemd daemon"
systemctl daemon-reload

echo ">> Enabling and starting $SERVICE_NAME"
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

echo ""
echo ">> Installation complete."
echo "   Edit configuration: $ENV_DEST"
echo "   Check status:       systemctl status $SERVICE_NAME"
echo "   Follow logs:        journalctl -u $SERVICE_NAME -f"
