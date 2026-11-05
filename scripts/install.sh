#!/bin/bash
set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
source "$project_dir/scripts/deploy-common.sh"

check=false
case ${1-} in
    --check) check=true ;;
    --help|-h)
        printf '%s\n' 'Usage: bash scripts/install.sh [--check]' \
            'Build/test without sudo first. Installation requires root, installs runtime' \
            'packages with apt, stops playback, and replaces checkout configuration.' \
            'It does not enable or start the service. --check makes no changes.'
        exit 0 ;;
    '') ;;
    *) fail 'usage: bash scripts/install.sh [--check]' ;;
esac
[[ $# -le 1 ]] || fail 'too many arguments'
$check || require_root
check_system
$check || lock_deployment
require_command apt-get
for command in groupadd useradd usermod runuser chown chmod; do require_command "$command"; done
release=$project_dir/_build/prod/rel/easytv
[[ -x $release/bin/play && -x $release/bin/easytv && -d $release/releases ]] ||
    fail 'build first without sudo: MIX_ENV=prod mix release --overwrite'
[[ -z $(find "$release" -type l -print -quit) ]] || fail 'release must not contain symbolic links'
for path in priv/easytv.config.exs systemd/easytv.service systemd/70-easytv-remote.rules; do
    [[ -s $project_dir/$path && ! -L $project_dir/$path ]] || fail "missing/invalid source: $path"
done
check_destinations
check_account
check_managed_files
if $check; then
    printf '%s\n' 'PASS deployment preflight; no files, packages, accounts, or service states changed.' \
        'Runtime architecture, display, audio, media, and input access still need validation on the Pi.'
    exit 0
fi

apt-get update
apt-get install -y --no-install-recommends mpv libvips-tools aosd-cat \
    xserver-xorg-core xinit xauth xfonts-base fonts-dejavu-core libpam-systemd ca-certificates
getent group easytv >/dev/null || groupadd --system easytv
getent passwd easytv >/dev/null || useradd --system --gid easytv \
    --home-dir /var/lib/easytv --no-create-home --shell /usr/sbin/nologin easytv
check_account
for group in video render audio; do
    getent group "$group" >/dev/null || fail "missing hardware group: $group"
done
usermod --append --groups video,render,audio easytv
install -d -o easytv -g easytv -m 0700 /var/lib/easytv
install -d -o root -g root -m 0755 /opt /etc/easytv /usr/local/share/easytv

stage=$(mktemp -d /opt/.easytv-stage.XXXXXX)
backup=
replacing=false
cleanup() {
    local status=$?
    trap - EXIT
    rm -r -- "$stage"
    if (( status != 0 )); then
        printf 'Installation failed; service is not restarted.\n' >&2
        if $replacing; then
            printf 'Files may be partially replaced. Keep playback stopped; inspect deployment.\n' >&2
            [[ -z $backup ]] || printf 'Previous release retained at %s\n' "$backup" >&2
        fi
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
install -d "$stage/opt/easytv" "$stage/etc/easytv" "$stage/etc/systemd/system" "$stage/etc/udev/rules.d"
cp -a "$release/." "$stage/opt/easytv/"
chown -R root:root "$stage"
chmod -R go-w "$stage"
install -m 0644 "$project_dir/priv/easytv.config.exs" "$stage/etc/easytv/config.exs"
install -m 0644 "$project_dir/systemd/easytv.service" "$stage/etc/systemd/system/easytv.service"
install -m 0644 "$project_dir/systemd/70-easytv-remote.rules" "$stage/etc/udev/rules.d/70-easytv-remote.rules"
# Execute user configuration only as the unprivileged service account.
chmod 0755 "$stage"
runuser -u easytv -- "$stage/opt/easytv/bin/easytv" eval \
    'EasyTV.UserConfig.Loader.load!(hd(System.argv())); IO.puts("PASS staged release/config")' \
    "$stage/etc/easytv/config.exs" || fail 'staged release/config validation failed'
write_manifest "$stage" "$stage/install-manifest"
check_destinations
check_managed_files
stop_service
replacing=true
if [[ -d /opt/easytv ]]; then
    backup=$(mktemp -d /opt/.easytv-previous.XXXXXX)
    rmdir "$backup"
    mv /opt/easytv "$backup"
fi
mv "$stage/opt/easytv" /opt/easytv
install -m 0644 "$stage/etc/easytv/config.exs" /etc/easytv/config.exs
install -m 0644 "$stage/etc/systemd/system/easytv.service" /etc/systemd/system/easytv.service
install -m 0644 "$stage/etc/udev/rules.d/70-easytv-remote.rules" /etc/udev/rules.d/70-easytv-remote.rules
install -m 0644 "$stage/install-manifest" /usr/local/share/easytv/install-manifest
udevadm control --reload-rules
udevadm trigger --subsystem-match=input
systemctl daemon-reload
if [[ -n $backup ]]; then rm -r -- "$backup"; backup=; fi
printf '%s\n' 'EasyTV installed; playback is stopped. Boot enablement was not changed.' \
    'Configure media/display/audio before starting; attached keyboards can control EasyTV.' \
    'Start: sudo systemctl start easytv.service' \
    'Logs: sudo journalctl _UID=$(id -u easytv) -b --no-pager' \
    'Playback state preserved.'
