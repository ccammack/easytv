#!/bin/sh

# exec xterm
exec systemd-cat -t easytv "$HOME"/easytv.exs --scan /mnt/media/ 2>&1
