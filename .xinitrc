#!/bin/sh

#exec xterm -fa Monospace -fs 12
exec $HOME/easytv.exs /mnt/media/ 2>&1 | systemd-cat -t easytv
