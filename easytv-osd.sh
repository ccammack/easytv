#!/usr/bin/env bash
set -u

pidfile="/tmp/easytv-osd.pid"

stop_managed_process() {
  if [ ! -f "$pidfile" ]; then
    return 0
  fi

  pid="$(cat "$pidfile" 2>/dev/null || true)"

  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    pkill -TERM -P "$pid" 2>/dev/null || true
    kill -TERM "$pid" 2>/dev/null || true
    sleep 0.02
    pkill -KILL -P "$pid" 2>/dev/null || true
    kill -KILL "$pid" 2>/dev/null || true
  fi

  rm -f "$pidfile"
}

aosd_cat="$(command -v aosd_cat || true)"
if [ -z "$aosd_cat" ]; then
  echo "could not find executable: aosd_cat" >&2
  exit 1
fi

stop_managed_process

if [ "$#" -eq 0 ]; then
  exit 0
fi

exec "$aosd_cat" \
  --position 0 \
  --x-offset 32 \
  --y-offset 32 \
  --padding 16 \
  --transparency 0 \
  --font "monospace 24" \
  --back-color black \
  --fore-color white \
  --fade-in 0 \
  --fade-full 5000 \
  --fade-out 0 \
  --lines 1 <<< "$1" >/dev/null 2>&1 &

echo "$!" > "$pidfile"
exit 0
