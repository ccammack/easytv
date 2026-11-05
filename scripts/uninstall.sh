#!/bin/bash
set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
source "$project_dir/scripts/deploy-common.sh"
check=false
case ${1-} in
    --check) check=true ;;
    --help|-h)
        printf '%s\n' 'Usage: bash scripts/uninstall.sh [--check]' \
            'Remove managed release/config/unit/input rule. Keep state, account,' \
            'media, and prerequisites. --check makes no changes.'
        exit 0 ;;
    '') ;;
    *) fail 'usage: bash scripts/uninstall.sh [--check]' ;;
esac
[[ $# -le 1 ]] || fail 'too many arguments'
$check || require_root
check_system
$check || lock_deployment
check_destinations
check_managed_files
if [[ ! -f /usr/local/share/easytv/install-manifest ]]; then
    printf 'No managed EasyTV installation found.\n'
    exit 0
fi
if $check; then
    printf 'PASS uninstall preflight; no changes made.\n'
    exit 0
fi
stop_service
systemctl disable easytv.service
rm -r /opt/easytv
rm -f /etc/easytv/config.exs /etc/systemd/system/easytv.service \
    /etc/udev/rules.d/70-easytv-remote.rules /usr/local/share/easytv/install-manifest
systemctl daemon-reload
udevadm control --reload-rules
udevadm trigger --subsystem-match=input
systemctl start getty@tty1.service || fail 'removed files, but console login could not be restored'
printf '%s\n' 'EasyTV uninstalled. Media, state, service account, and packages were kept.'
