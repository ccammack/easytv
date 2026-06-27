# #!/usr/bin/env bash
# set -eu

# log_err() {
#   systemd-cat -t easytv -p err printf '%s\n' "$*"
# }

# if ! pulseaudio --check 2>/dev/null; then
#   if ! pulseaudio --start 2> >(systemd-cat -t easytv -p err); then
#     log_err "failed to start PulseAudio"
#     exit 0
#   fi
# fi

# sleep 2

# hdmi_sink="$(
#   pactl list sinks short 2> >(systemd-cat -t easytv -p err) \
#     | grep -i hdmi \
#     | awk '{print $2}' \
#     | head -n1
# )"

# if [ -n "$hdmi_sink" ]; then
#   pactl set-default-sink "$hdmi_sink" 2> >(systemd-cat -t easytv -p err) \
#     || log_err "failed to set default sink: $hdmi_sink"
# else
#   log_err "no HDMI sink found"
# fi