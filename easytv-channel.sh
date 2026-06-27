#!/usr/bin/env bash
set -eu -o pipefail

script_name="$(basename -- "$0")"

_tv_logical_address="0"
_cec_osd_name="easytv"
_cec_client_args=("-o" "$_cec_osd_name" "-s" "-d" "1")

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

case "${1-}" in
  "-")
    # Prev Channel
    ;;
  "+")
    # Next Channel
    ;;
  *)
    die "usage: ${script_name} [+|-]"
    ;;
esac

# Assert active source
printf '%s\n' "as" | "$cec_client_bin" "${_cec_client_args[@]}"
