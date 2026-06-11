#!/usr/bin/env bash
set -u

_tv_logical_address="0"
_cec_client_args=("-s" "-d" "1")

say() {
  printf '%s\n' "$*"
}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

cec_client_bin="$(command -v cec-client || true)"
if [ -z "$cec_client_bin" ]; then
  die "could not find executable: cec-client"
fi

run_cec() {
  local cmd="$1"
  local output status

  output="$(printf '%s\n' "$cmd" | "$cec_client_bin" "${_cec_client_args[@]}" 2>&1)"
  status=$?

  if [ "$status" -ne 0 ]; then
    printf '%s\n' "$output" >&2
    return "$status"
  fi

  case "$(printf '%s' "$output" | tr '[:upper:]' '[:lower:]')" in
    *"cec_transmit failed"*|*"ioctl cec_transmit failed"*|*"errno=64"*|*"connection closed"*|*"failed to"*)
      printf '%s\n' "$output" >&2
      return 1
      ;;
  esac

  printf '%s\n' "$output"
}

activate_source_until_ok() {
  local attempt=1

  while true; do
    say "active source attempt: $attempt"

    if run_cec "as" >/dev/null; then
      return 0
    fi

    sleep 1.5
    attempt=$((attempt + 1))
  done
}

case "${1-}" in
  0)
    say "standby tv logical address: $_tv_logical_address"
    run_cec "standby $_tv_logical_address" >/dev/null || die "cec standby failed"
    ;;
  1)
    say "power on tv logical address: $_tv_logical_address"
    run_cec "on $_tv_logical_address" >/dev/null || die "cec power on failed"
    sleep 1.5
    say "set active source"
    activate_source_until_ok || die "cec active source failed"
    ;;
  *)
    die "usage: $(basename "$0") [0|1]"
    ;;
esac