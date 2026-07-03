#!/bin/sh
set -eu

usage() {
  cat <<'EOF'
Usage:
  volume.sh      # toggle mute
  volume.sh +    # increase volume
  volume.sh -    # decrease volume
EOF
}

arg=${1-__NOARG__}

case "$arg" in
  __NOARG__)
    echo "volume.sh: toggle_mute"
    ;;
  +)
    echo "volume.sh: increase"
    ;;
  -)
    echo "volume.sh: decrease"
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    echo "volume.sh: invalid argument: $1" >&2
    usage >&2
    exit 2
    ;;
esac