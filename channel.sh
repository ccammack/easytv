#!/bin/sh
set -eu

usage() {
  cat <<'EOF'
Usage:
  channel.sh +   # next channel hook
  channel.sh -   # previous channel hook
EOF
}

if [ "$#" -ne 1 ]; then
  usage >&2
  exit 1
fi

case "$1" in
  +)
    echo "channel.sh: next"
    ;;
  -)
    echo "channel.sh: prev"
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    echo "channel.sh: invalid argument: $1" >&2
    usage >&2
    exit 2
    ;;
esac

# Make easytv the active TV source on every channel change
printf '%s\n' 'as' | cec-client -s -d 1 -o easytv
# cec-ctl -d /dev/cec0 --to 0 --active-source phys-addr=1.0.0.0
