#!/usr/bin/env bash
set -u

step=3
control="PCM"

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
osd_script="$script_dir/easytv-osd.sh"

say() {
  printf '%s\n' "$*"
}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

clamp() {
  local n="$1"
  local lo="$2"
  local hi="$3"

  if [ "$n" -lt "$lo" ]; then
    printf '%s\n' "$lo"
  elif [ "$n" -gt "$hi" ]; then
    printf '%s\n' "$hi"
  else
    printf '%s\n' "$n"
  fi
}

read_current_output() {
  "$amixer_bin" sget "$control" 2>/dev/null
}

read_current_percent() {
  local output line percent
  output="$1"
  line="$(printf '%s\n' "$output" | awk '/Playback/ && /%/ { last = $0 } END { print last }')"
  [ -n "$line" ] || return 1
  percent="$(printf '%s\n' "$line" | sed -n 's/.*\[\([0-9]\{1,3\}\)%\].*/\1/p' | head -n1)"
  [ -n "$percent" ] || return 1
  printf '%s\n' "$percent"
}

build_osd_text() {
  local output line percent
  output="$1"
  line="$(printf '%s\n' "$output" | awk '/Playback/ && /%/ { last = $0 } END { print last }')"
  [ -n "$line" ] || return 1

  percent="$(printf '%s\n' "$line" | sed -n 's/.*\[\([0-9]\{1,3\}%\)\].*/\1/p' | head -n1)"
  [ -n "$percent" ] || return 1

  if printf '%s\n' "$line" | grep -q '\[off\]'; then
    printf 'Volume %s (muted)\n' "$percent"
  else
    printf 'Volume %s\n' "$percent"
  fi
}

amixer_bin="$(command -v amixer || true)"
if [ -z "$amixer_bin" ]; then
  die "could not find executable: amixer"
fi

if [ ! -f "$osd_script" ]; then
  die "could not find script: $osd_script"
fi

output="$(read_current_output)" || die "amixer read failed for control: $control"
current_percent="$(read_current_percent "$output")" || die "could not parse current percent for control: $control"

case "${1-}" in
  "")
    say "toggle mute on control: $control"
    "$amixer_bin" sset "$control" toggle >/dev/null 2>&1 \
      || die "amixer toggle failed for control: $control"
    ;;
  "+")
    target_percent="$(clamp "$((current_percent + step))" 0 100)"
    say "volume up on control: $control current=${current_percent}% target=${target_percent}%"
    "$amixer_bin" sset "$control" "${target_percent}%" >/dev/null 2>&1 \
      || die "amixer set failed for control: $control target=${target_percent}%"
    ;;
  "-")
    target_percent="$(clamp "$((current_percent - step))" 0 100)"
    say "volume down on control: $control current=${current_percent}% target=${target_percent}%"
    "$amixer_bin" sset "$control" "${target_percent}%" >/dev/null 2>&1 \
      || die "amixer set failed for control: $control target=${target_percent}%"
    ;;
  *)
    die "usage: $(basename "$0") [ + | - ]"
    ;;
esac

output="$(read_current_output)" || die "amixer readback failed for control: $control"
text="$(build_osd_text "$output")" || die "could not parse amixer output for control: $control"

say "osd text: $text"
"$osd_script" "$text"
exit $?