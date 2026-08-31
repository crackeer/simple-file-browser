#!/usr/bin/env bash
#
# install.sh - Install simple-file-browser as a systemd service.
#
# This script:
#   1. Copies the simple-file-browser binary to /usr/local/bin/
#   2. Installs the systemd service unit under /etc/systemd/system/
#   3. Installs (or preserves) /etc/simple-file-browser.env
#   4. Creates the served root directory if it does not exist
#   5. Reloads systemd, enables and (re)starts the service
#
# Run from the extracted dist/ directory:
#
#   sudo ./install.sh
#
set -euo pipefail

BIN_NAME="simple-file-browser"
BIN_DEST="/usr/local/bin/${BIN_NAME}"
SERVICE_NAME="simple-file-browser.service"
SERVICE_DEST="/etc/systemd/system/${SERVICE_NAME}"
ENV_DEST="/etc/simple-file-browser.env"

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: this installer must be run as root (try: sudo ./install.sh)" >&2
    exit 1
fi

for f in "$BIN_NAME" "$SERVICE_NAME" "simple-file-browser.env"; do
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
install -m 0644 "$SRC_DIR/simple-file-browser.env" "$ENV_DEST"

# Ensure the served root exists; if the operator changes HOME in
# simple-file-browser.env they are responsible for creating that directory.
DEFAULT_ROOT="/srv/simple-file-browser"
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
