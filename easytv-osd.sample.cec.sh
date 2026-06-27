# #!/usr/bin/env bash
# set -u

# # Configure the tv_addr using the device # from:
# #   $ echo 'scan' | cec-client -s -d 1 -o easytv | grep device
# #   device #0: TV
# #   device #1: Recorder 1
# #   device #4: Playback 1
# tv_addr="0"

# die() {
#   printf '%s\n' "$*" >&2
#   exit 1
# }


# cec_client_bin="$(command -v cec-client || true)"
# if [ -z "$cec_client_bin" ]; then
#   die "could not find executable: cec-client"
# fi

# message="${*:- }"
# message="$(printf '%s' "$message" | LC_ALL=C tr -cd '[:print:]' | cut -c1-13)"
# message="${message:- }"

# printf 'osd %s %s\n' "$tv_addr" "$message" | "$cec_client_bin" -s -d 1