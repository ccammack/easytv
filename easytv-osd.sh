# #!/usr/bin/env bash
# set -eu -o pipefail

# script_name="$(basename -- "$0")"
# pidfile="/tmp/${script_name}.pid"

# say() {
#   printf '%s\n' "$*"
# }

# die() {
#   printf '%s\n' "$*" >&2
#   exit 1
# }

# stop_managed_process() {
#   local pidfile_path="$1"
#   local pid=""

#   if [ ! -f "$pidfile_path" ]; then
#     return 0
#   fi

#   if ! read -r pid < "$pidfile_path"; then
#     rm -f "$pidfile_path"
#     return 0
#   fi

#   if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
#     kill -TERM "$pid" 2>/dev/null || true
#     sleep 0.05
#     kill -KILL "$pid" 2>/dev/null || true
#   fi

#   rm -f "$pidfile_path"
# }

# aosd_cat_bin="$(command -v aosd_cat || true)"
# if [ -z "$aosd_cat_bin" ]; then
#   die "could not find executable: aosd_cat"
# fi

# stop_managed_process "$pidfile"

# if [ "$#" -eq 0 ]; then
#   exit 0
# fi

# message="$*"

# _aosd_cat_args=(
#   --position 0
#   --x-offset 32
#   --y-offset 32
#   --padding 16
#   --transparency 0
#   --font "monospace 24"
#   --back-color black
#   --fore-color white
#   --fade-in 0
#   --fade-full 5000
#   --fade-out 0
#   --lines 1
# )

# "$aosd_cat_bin" "${_aosd_cat_args[@]}" <<< "$message" >/dev/null 2>&1 &
# echo "$!" > "$pidfile"
# exit 0
