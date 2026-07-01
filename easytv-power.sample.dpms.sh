# #!/usr/bin/env bash
# set -eu -o pipefail

# script_name="$(basename -- "$0")"

# say() {
#   printf '%s\n' "$*"
# }

# die() {
#   printf '%s\n' "$*" >&2
#   exit 1
# }

# xset_bin="$(command -v xset || true)"
# if [ -z "$xset_bin" ]; then
#   die "could not find executable: xset"
# fi

# case "${1-}" in
#   0)
#     say "dpms off"
#     "$xset_bin" dpms force off
#     ;;
#   1)
#     say "dpms on"
#     "$xset_bin" dpms force on
#     ;;
#   *)
#     die "usage: ${script_name} [0|1]"
#     ;;
# esac