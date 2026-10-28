#!/bin/bash
set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
bash -n scripts/install.sh scripts/uninstall.sh

for script in scripts/install.sh scripts/uninstall.sh; do
    if output="$(bash "$script" --invalid 2>&1)"; then
        printf 'FAIL: %s accepted unexpected arguments\n' "$script" >&2
        exit 1
    fi
    [[ $output == *usage:* ]]
    if [[ $EUID != 0 ]]; then
        if output="$(bash "$script" 2>&1)"; then
            printf 'FAIL: %s accepted an unprivileged invocation\n' "$script" >&2
            exit 1
        fi
        [[ $output == *'run as root'* ]]
    fi
done

if ! command -v bwrap >/dev/null; then
    printf 'PASS: installer syntax/argument/privilege guards; SKIP: isolated install scenarios (bwrap unavailable)\n'
    exit 0
fi

work="$(mktemp -d /tmp/easytv-install-test.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/mock" "$work/state" "$work/project/bin" "$work/project/scripts" "$work/project/systemd"
cp scripts/install.sh scripts/uninstall.sh scripts/easytv-remote.exs "$work/project/scripts/"
cp systemd/easytv.service systemd/easytv-remote.service systemd/70-easytv-remote.rules "$work/project/systemd/"
printf '#!/bin/sh\nexit 0\n' > "$work/project/bin/easytv"
chmod 0755 "$work/project/bin/easytv"

cat > "$work/mock/command" <<'MOCK'
#!/bin/bash
set -euo pipefail
command="${0##*/}"
printf '%s %s\n' "$command" "$*" >> /test/commands
case "$command" in
    apt-get)
        [[ ! -e /test/state/apt-fails ]]
        ;;
    getent)
        case "$1:$2" in
            passwd:easytv)
                if [[ -e /test/state/foreign-user ]]; then
                    printf 'easytv:x:1000:1000::/home/someone:/bin/bash\n'
                elif [[ -e /test/state/user ]]; then
                    printf 'easytv:x:999:999::/var/lib/easytv:/usr/sbin/nologin\n'
                else
                    exit 2
                fi
                ;;
            group:easytv)
                [[ -e /test/state/group ]] || exit 2
                printf 'easytv:x:999:\n'
                ;;
            group:video|group:render|group:audio)
                printf '%s:x:44:\n' "$2"
                ;;
            *) exit 2 ;;
        esac
        ;;
    groupadd) touch /test/state/group ;;
    useradd) touch /test/state/user ;;
    usermod) ;;
    runuser)
        [[ $* == '-u easytv -- env HOME=/var/lib/easytv /usr/local/lib/easytv/easytv-remote.exs --prepare' ||
           $* == '-u easytv -- env HOME=/var/lib/easytv mix local.hex --force' ]]
        [[ ! -e /test/state/prepare-fails ]]
        ;;
    udevadm) ;;
    install)
        args=()
        while [[ $# != 0 ]]; do
            case "$1" in
                -o|-g)
                    args+=("$1" 0)
                    shift 2
                    ;;
                *)
                    args+=("$1")
                    shift
                    ;;
            esac
        done
        /usr/bin/install "${args[@]}"
        ;;
    id)
        [[ $* == '-gn easytv' ]]
        printf 'easytv\n'
        ;;
    systemctl)
        case "$1" in
            daemon-reload) ;;
            enable)
                mkdir -p /etc/systemd/system/multi-user.target.wants
                ln -sf /etc/systemd/system/easytv.service \
                    /etc/systemd/system/multi-user.target.wants/easytv.service
                ;;
            disable)
                [[ $* == 'disable --now easytv.service' ]]
                rm -f /etc/systemd/system/multi-user.target.wants/easytv.service
                ;;
            stop) [[ $* == 'stop easytv-remote.service' ]] ;;
            *) printf 'Unexpected systemctl command: %s\n' "$*" >&2; exit 1 ;;
        esac
        ;;
    *) printf 'Unexpected mock command: %s\n' "$command" >&2; exit 1 ;;
esac
MOCK
chmod 0755 "$work/mock/command"
for command in apt-get getent groupadd useradd usermod install id systemctl runuser udevadm; do
    ln -s command "$work/mock/$command"
done

cat > "$work/scenarios.sh" <<'SCENARIOS'
#!/bin/bash
set -euo pipefail
cd /test/project
expect_failure() {
    local expected="$1"
    shift
    local output
    if output="$("$@" 2>&1)"; then
        printf 'Expected failure: %s\n' "$*" >&2
        exit 1
    fi
    [[ $output == *"$expected"* ]]
}

mv bin/easytv bin/saved
expect_failure 'build first' bash scripts/install.sh
[[ ! -e /test/commands ]]
mv bin/saved bin/easytv

touch /test/state/foreign-user
expect_failure 'existing easytv account' bash scripts/install.sh
! grep -q '^apt-get' /test/commands
rm /test/state/foreign-user

touch /test/state/apt-fails
expect_failure '' bash scripts/install.sh
[[ ! -e /test/state/user && ! -e /usr/local/bin/easytv ]]
rm /test/state/apt-fails

mkdir -p /mnt/media/Channel
printf 'media\n' > /mnt/media/Channel/keep
bash scripts/install.sh
cmp bin/easytv /usr/local/bin/easytv
cmp systemd/easytv.service /etc/systemd/system/easytv.service
cmp systemd/easytv-remote.service /etc/systemd/system/easytv-remote.service
cmp scripts/easytv-remote.exs /etc/easytv/easytv-remote.exs
cmp scripts/easytv-remote.exs /usr/local/lib/easytv/easytv-remote.exs
cmp systemd/70-easytv-remote.rules /etc/udev/rules.d/70-easytv-remote.rules
[[ $(stat -c %a /etc/easytv/easytv-remote.exs) == 755 ]]
[[ $(stat -c %a /usr/local/bin/easytv) == 755 ]]
[[ $(stat -c %a /etc/systemd/system/easytv.service) == 644 ]]
[[ $(stat -c %a /var/lib/easytv) == 700 ]]
grep -q '^install -d -o easytv -g easytv -m 0700 /var/lib/easytv$' /test/commands
[[ -L /etc/systemd/system/multi-user.target.wants/easytv.service ]]
grep -q '^usermod --append --groups video,render,audio easytv$' /test/commands
! grep -Eq '^systemctl (start|restart)|^apt-get .* (pulseaudio|pipewire|alsa-utils)( |$)' /test/commands
printf 'saved\n' > /var/lib/easytv/state.json
mkdir -p /etc/systemd/system/easytv.service.d
printf '[Service]\n' > /etc/systemd/system/easytv.service.d/local.conf
printf '# user customization\n' >> /etc/easytv/easytv-remote.exs
mkdir -p /etc/systemd/system/easytv-remote.service.d
printf '[Service]\n' > /etc/systemd/system/easytv-remote.service.d/local.conf

bash scripts/install.sh
[[ $(grep -c '^useradd ' /test/commands) == 1 ]]
[[ $(grep -c '^groupadd ' /test/commands) == 1 ]]
[[ $(cat /var/lib/easytv/state.json) == saved ]]
[[ -f /etc/systemd/system/easytv.service.d/local.conf ]]
grep -q '# user customization' /etc/easytv/easytv-remote.exs
[[ -f /etc/systemd/system/easytv-remote.service.d/local.conf ]]

touch /test/state/prepare-fails
expect_failure '' bash scripts/install.sh
rm /test/state/prepare-fails

bash scripts/uninstall.sh
[[ ! -e /usr/local/bin/easytv && ! -e /etc/systemd/system/easytv.service ]]
[[ ! -e /etc/systemd/system/easytv-remote.service && ! -e /usr/local/lib/easytv/easytv-remote.exs ]]
[[ ! -e /etc/udev/rules.d/70-easytv-remote.rules ]]
grep -q '# user customization' /etc/easytv/easytv-remote.exs
[[ -f /etc/systemd/system/easytv-remote.service.d/local.conf ]]
[[ ! -e /etc/systemd/system/multi-user.target.wants/easytv.service ]]
[[ -f /test/state/user && -f /test/state/group ]]
[[ $(cat /var/lib/easytv/state.json) == saved ]]
[[ $(cat /mnt/media/Channel/keep) == media ]]
[[ -f /etc/systemd/system/easytv.service.d/local.conf ]]
bash scripts/uninstall.sh
printf 'PASS: isolated install, reinstall, uninstall, preservation, and failure scenarios\n'
SCENARIOS

bwrap --unshare-user --uid 0 --gid 0 --tmpfs / \
    --ro-bind /usr /usr --ro-bind /lib /lib --ro-bind-try /lib64 /lib64 \
    --symlink usr/bin /bin --symlink usr/sbin /sbin --proc /proc --dev /dev \
    --tmpfs /etc --tmpfs /var/lib --tmpfs /usr/local --tmpfs /run --tmpfs /mnt \
    --dir /run/systemd/system --dir /etc/systemd/system --dir /usr/local/bin \
    --bind "$work" /test \
    --ro-bind /etc/passwd /etc/passwd --ro-bind /etc/group /etc/group \
    --setenv PATH /test/mock:/usr/bin:/bin \
    /bin/bash /test/scenarios.sh
