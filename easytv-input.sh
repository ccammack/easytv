# #!/usr/bin/env bash
# set -eu

# # Find input addresses with:
# #   echo 'scan' | cec-client -s -d 1 -o easytv
# #
# # In the output, find the device you want and copy its:
# #   address: X.X.X.X
# #
# # Examples:
# #   1.0.0.0  -> device on TV HDMI 1
# #   2.0.0.0  -> device on TV HDMI 2
# #   2.1.0.0  -> device behind something connected to TV HDMI 2
# #
# # Put the addresses below in the order you want to cycle through them.
# # Index 0 is treated as the default starting point when no state file exists.
# # Do not include the TV's address (0.0.0.0).
# inputs=(
#   "1.0.0.0"
#   "2.0.0.0"
# )

# state_file="/tmp/$(basename "$0").state"
# cec_client_bin="$(command -v cec-client)"

# read_index() {
#   [[ -f "$state_file" ]] && cat "$state_file" || echo 0
# }

# write_index() {
#   echo "$1" > "$state_file"
# }

# phys_to_cec_bytes() {
#   local phys="$1"
#   local a b c d

#   IFS=. read -r a b c d <<< "$phys" || return 1

#   for part in "$a" "$b" "$c" "$d"; do
#     case "$part" in
#       ''|*[!0-9]*)
#         return 1
#         ;;
#     esac
#     (( part >= 0 && part <= 15 )) || return 1
#   done

#   printf '%x%x:%x%x\n' "$a" "$b" "$c" "$d"
# }

# case "${1-}" in
#   +) next_index=$(( $(read_index) + 1 )) ;;
#   -) next_index=$(( $(read_index) - 1 )) ;;
#   *) echo "usage: $(basename "$0") [ + | - ]" >&2; exit 2 ;;
# esac

# (( next_index >= 0 && next_index < ${#inputs[@]} )) || exit 1

# target_phys="${inputs[$next_index]}"
# target_bytes="$(phys_to_cec_bytes "$target_phys")" || {
#   echo "invalid physical address: $target_phys" >&2
#   exit 2
# }

# echo "tx 4F:82:${target_bytes}" | "$cec_client_bin" -s -d 1 -o easytv >/dev/null
# write_index "$next_index"
