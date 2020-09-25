#!/bin/sh

echo "[*] Before remove (<%= pkg %>)"

DESKTOP_FILE=/usr/share/applications/IVPN.desktop
if [ -f $DESKTOP_FILE ]; then
    echo "[ ] Uninstalling .desktop file"
    rm $DESKTOP_FILE || echo "[-] Failed to remove file: '$DESKTOP_FILE'"
fi