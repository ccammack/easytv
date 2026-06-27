#!/bin/sh

xrandr --output HDMI-1 --mode 1280x720 --primary

exec xterm -maximized
# exec systemd-cat -t easytv "$HOME"/easytv/easytv.exs --scan /mnt/media/ 2>&1
