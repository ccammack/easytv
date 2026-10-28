#!/bin/bash
set -euo pipefail

fail() {
    printf 'easytv install: %s\n' "$*" >&2
    exit 1
}

[[ $# == 0 ]] || fail 'usage: sudo bash scripts/install.sh'
[[ $EUID == 0 ]] || fail 'run as root (sudo make install)'
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
[[ -x "$project_dir/bin/easytv" ]] || fail 'build first as your normal user: make'
[[ -f "$project_dir/systemd/easytv.service" ]] || fail 'systemd/easytv.service is missing'
[[ -f "$project_dir/systemd/easytv-remote.service" ]] || fail 'easytv-remote.service is missing'
[[ -f "$project_dir/scripts/easytv-remote.exs" ]] || fail 'easytv-remote.exs is missing'
[[ ! -L /etc/easytv && ! -L /etc/easytv/easytv-remote.exs ]] ||
    fail 'remote configuration must not be a symbolic link'
command -v apt-get >/dev/null || fail 'Raspberry Pi OS Lite (Debian apt) is required'
[[ -d /run/systemd/system ]] || fail 'a running systemd system is required'

if getent passwd easytv >/dev/null; then
    IFS=: read -r _ _ uid _ _ home shell < <(getent passwd easytv)
    [[ $uid != 0 && $home == /var/lib/easytv && $shell == /usr/sbin/nologin ]] ||
        fail 'existing easytv account is not the expected service account; resolve it before installing'
    [[ "$(id -gn easytv)" == easytv ]] || fail 'existing easytv account must have primary group easytv'
fi
[[ ! -L /var/lib/easytv ]] || fail '/var/lib/easytv must not be a symbolic link'

apt-get update
apt-get install -y --no-install-recommends \
    mpv libvips-tools aosd-cat \
    xserver-xorg-core xinit xauth xfonts-base fonts-dejavu-core libpam-systemd \
    elixir erlang-dev build-essential ca-certificates

if ! getent group easytv >/dev/null; then
    groupadd --system easytv
fi
if ! getent passwd easytv >/dev/null; then
    useradd --system --gid easytv --home-dir /var/lib/easytv \
        --no-create-home --shell /usr/sbin/nologin easytv
fi
for group in video render audio; do
    getent group "$group" >/dev/null || fail "required hardware group is missing: $group"
done
usermod --append --groups video,render,audio easytv

install -d -o easytv -g easytv -m 0700 /var/lib/easytv
install -d -o root -g root -m 0755 /usr/local/bin
install -o root -g root -m 0755 "$project_dir/bin/easytv" /usr/local/bin/easytv
install -o root -g root -m 0644 "$project_dir/systemd/easytv.service" \
    /etc/systemd/system/easytv.service
install -d -o root -g root -m 0755 /usr/local/lib/easytv /etc/easytv /etc/udev/rules.d
install -o root -g root -m 0755 "$project_dir/scripts/easytv-remote.exs" \
    /usr/local/lib/easytv/easytv-remote.exs
if [[ ! -e /etc/easytv/easytv-remote.exs ]]; then
    install -o root -g root -m 0755 "$project_dir/scripts/easytv-remote.exs" \
        /etc/easytv/easytv-remote.exs
fi
install -o root -g root -m 0644 "$project_dir/systemd/easytv-remote.service" \
    /etc/systemd/system/easytv-remote.service
install -o root -g root -m 0644 "$project_dir/systemd/70-easytv-remote.rules" \
    /etc/udev/rules.d/70-easytv-remote.rules
runuser -u easytv -- env HOME=/var/lib/easytv mix local.hex --force
runuser -u easytv -- env HOME=/var/lib/easytv /usr/local/lib/easytv/easytv-remote.exs --prepare
udevadm control --reload-rules
udevadm trigger --subsystem-match=input
systemctl daemon-reload
systemctl enable easytv.service

printf '%s\n' \
    'EasyTV installed and enabled for boot; it has not been started or restarted.' \
    'Review media/device paths with: sudo systemctl edit easytv.service' \
    'Remote device paths: sudo systemctl edit easytv-remote.service' \
    'Editable remote script: /etc/easytv/easytv-remote.exs (preserved on reinstall)' \
    'Then start (or restart an existing installation) with: sudo systemctl restart easytv.service' \
    'Logs: journalctl -u easytv.service -b'
