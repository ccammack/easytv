#!/usr/bin/env bash
set -u

say() {
  printf '%s\n' "$*"
}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

xset_bin="$(command -v xset || true)"
if [ -z "$xset_bin" ]; then
  die "could not find executable: xset"
fi

run_dpms() {
  local mode="$1"
  local output status

  output="$("$xset_bin" dpms force "$mode" 2>&1)"
  status=$?

  if [ "$status" -ne 0 ]; then
    printf '%s\n' "$output" >&2
    return "$status"
  fi

  printf '%s\n' "$output"
}

case "${1-}" in
  0)
    say "dpms off"
    run_dpms "off" >/dev/null || die "dpms off failed"
    ;;
  1)
    say "dpms on"
    run_dpms "on" >/dev/null || die "dpms on failed"
    ;;
  *)
    die "usage: $(basename "$0") [0|1]"
    ;;
esac