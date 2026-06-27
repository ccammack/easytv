# #!/usr/bin/env bash
# set -eu

# step="3%"  # volume step
# script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# osd_script="$script_dir/easytv-osd.sh"

# die() {
#   printf '%s\n' "$*" >&2
#   exit 1
# }

# pactl_bin="$(command -v pactl || true)"
# [ -n "$pactl_bin" ] || die "pactl not found (install pulseaudio-utils)"

# [ -x "$osd_script" ] || die "could not find executable: $osd_script"

# case "${1-}" in
#   "")
#     "$pactl_bin" set-sink-mute @DEFAULT_SINK@ toggle
#     ;;
#   "+")
#     "$pactl_bin" set-sink-volume @DEFAULT_SINK@ "+$step"
#     ;;
#   "-")
#     "$pactl_bin" set-sink-volume @DEFAULT_SINK@ "-$step"
#     ;;
#   *)
#     die "usage: $(basename -- "$0") [ + | - ]"
#     ;;
# esac

# # Minimal readback for OSD
# volume="$("$pactl_bin" get-sink-volume @DEFAULT_SINK@ | awk 'match($0, /[0-9]+%/) { print substr($0, RSTART, RLENGTH); exit }')" || volume="?"
# mute="$("$pactl_bin" get-sink-mute @DEFAULT_SINK@ | awk '{ print $2 }')" || mute="no"

# percent="${volume%%%}"
# if [ "$mute" = "yes" ]; then
#   text="Volume ${percent}% (muted)"
# else
#   text="Volume ${percent}%"
# fi

# "$osd_script" "$text"