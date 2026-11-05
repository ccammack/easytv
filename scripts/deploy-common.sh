#!/bin/bash

export PATH=/usr/sbin:/usr/bin:/sbin:/bin
export LC_ALL=C

fail() { printf 'easytv deployment: %s\n' "$*" >&2; exit 1; }
require_root() { [[ $EUID == 0 ]] || fail 'run installation/uninstallation with sudo'; }
require_command() { command -v "$1" >/dev/null || fail "required command missing: $1"; }

check_system() {
    [[ $(uname -s) == Linux && -d /run/systemd/system ]] ||
        fail 'a running Linux systemd host is required'
    for command in systemctl getent stat install cp mv rm rmdir mktemp sha256sum find sort xargs wc flock udevadm; do
        require_command "$command"
    done
}

lock_deployment() {
    install -d -m 0755 /run/lock
    exec 9>/run/lock/easytv-deploy.lock
    flock -n 9 || fail 'another install/uninstall is running'
}

check_path() {
    local path=$1 mode
    if [[ -e $path || -L $path ]]; then
        [[ ! -L $path ]] || fail "refusing symbolic link: $path"
        [[ $(stat -c %u "$path") == 0 ]] || fail "not root-owned: $path"
        mode=$(stat -c %a "$path")
        (( (8#$mode & 0022) == 0 )) || fail "group/other-writable path: $path"
    fi
}

check_destinations() {
    local path
    for path in /opt /opt/easytv /etc/easytv /etc/easytv/config.exs \
        /etc/systemd/system /etc/systemd/system/easytv.service \
        /etc/udev/rules.d /etc/udev/rules.d/70-easytv-remote.rules \
        /usr/local/share /usr/local/share/easytv /usr/local/share/easytv/install-manifest; do
        check_path "$path"
    done
    [[ ! -L /var/lib/easytv ]] || fail '/var/lib/easytv must not be a symbolic link'
    for path in /usr/local/bin/easytv /etc/systemd/system/easytv-remote.service \
        /usr/lib/systemd/system/easytv.service /run/systemd/system/easytv.service; do
        [[ ! -e $path && ! -L $path ]] ||
            fail "conflicting installation: $path; uninstall the other deployment first"
    done
}

check_account() {
    local record uid gid home shell
    record=$(getent passwd easytv || true)
    if [[ -n $record ]]; then
        IFS=: read -r _ _ uid gid _ home shell <<< "$record"
        [[ $uid != 0 && $home == /var/lib/easytv && $shell == /usr/sbin/nologin ]] ||
            fail 'existing easytv account is not the expected non-login service account'
        [[ $(id -gn easytv) == easytv ]] || fail 'easytv must have primary group easytv'
    fi
}

write_manifest() {
    local root=$1 output=$2
    (
        cd "$root"
        find opt/easytv -type f -print0
        printf '%s\0' etc/easytv/config.exs etc/systemd/system/easytv.service \
            etc/udev/rules.d/70-easytv-remote.rules
    ) | (
        cd "$root"
        sort -z | xargs -0 sha256sum
    ) > "$output"
}

check_managed_files() {
    local manifest=/usr/local/share/easytv/install-manifest path
    if [[ ! -f $manifest ]]; then
        for path in /opt/easytv /etc/easytv/config.exs /etc/systemd/system/easytv.service \
            /etc/udev/rules.d/70-easytv-remote.rules; do
            [[ ! -e $path ]] || fail "unmanaged destination already exists: $path"
        done
        return
    fi
    [[ -s $manifest && -d /opt/easytv ]] || fail 'incomplete installation; resolve before redeployment'
    [[ -z $(find /opt/easytv -type l -print -quit) ]] || fail 'installed release contains a symlink'
    local entry
    while IFS= read -r entry; do
        [[ $entry =~ ^[0-9a-f]{64}\ \ (opt/easytv/[^[:space:]]+|etc/easytv/config.exs|etc/systemd/system/easytv.service|etc/udev/rules.d/70-easytv-remote.rules)$ ]] ||
            fail 'invalid installation manifest entry'
    done < "$manifest"
    (
        cd /
        sha256sum --strict --check --status "$manifest"
    ) || fail 'managed files changed or missing; edit the checkout, not installed files'
    local expected actual
    expected=$(wc -l < "$manifest")
    actual=$(find /opt/easytv -type f | wc -l)
    [[ $expected == $((actual + 3)) ]] || fail 'unexpected files in installed release'
}

stop_service() {
    local load
    load=$(systemctl show --property=LoadState --value easytv.service) ||
        fail 'cannot inspect easytv.service'
    case $load in
        not-found) ;;
        loaded) systemctl stop easytv.service || fail 'could not stop service; files kept' ;;
        *) fail "unexpected service state: $load" ;;
    esac
}
