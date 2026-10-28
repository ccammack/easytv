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
if [[ -f /etc/systemd/system/easytv-remote.service ]]; then
    systemctl stop easytv-remote.service
    rm -f /etc/systemd/system/easytv-remote.service
fi
rm -f /usr/local/lib/easytv/easytv-remote.exs /etc/udev/rules.d/70-easytv-remote.rules
rm -f /usr/local/bin/easytv
systemctl daemon-reload
udevadm control --reload-rules
udevadm trigger --subsystem-match=input

printf '%s\n' \
    'EasyTV and remote uninstalled. Media, state, /etc/easytv, the easytv account, packages, and local systemd overrides were preserved.' \
    'Reboot to return tty1 to the normal console login.'
