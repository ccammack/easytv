#!/bin/bash
set -euo pipefail

fail() {
    printf 'easytv uninstall: %s\n' "$*" >&2
    exit 1
}

[[ $# == 0 ]] || fail 'usage: sudo bash scripts/uninstall.sh'
[[ $EUID == 0 ]] || fail 'run as root (sudo make uninstall)'
[[ -d /run/systemd/system ]] || fail 'a running systemd system is required'

if [[ -f /etc/systemd/system/easytv.service ]]; then
    systemctl disable --now easytv.service
    rm -f /etc/systemd/system/easytv.service
fi
rm -f /usr/local/bin/easytv
systemctl daemon-reload

printf '%s\n' \
    'EasyTV uninstalled. Media, state, the easytv account, packages, and local systemd overrides were preserved.' \
    'Reboot to return tty1 to the normal console login.'
