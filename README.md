# Introduction

`easytv` wraps [mpv](https://github.com/mpv-player/mpv) to create a minimal TV appliance for Raspberry Pi.

I run it on a Raspberry Pi 4B v1.2 4GB with [FLIRC](https://flirc.tv/) and an EZclicker 6-button remote. It is not configurable and may not run on anything else without modification.

It accepts a list of *media root* directories passed on the command line and treats each child directory below a *media root* as a media *channel*.

The media files in each *channel* subtree are cached into a looping playlist. Pass the `--scan` command line option to rebuild the *channel* playlist cache.

Switching *channels* saves the current playhead position and later restores it when that *channel* is resumed.

Pausing playback saves the current playhead position and turns the screen off using `xset`.

# Installation

Start with a fresh install of Raspberry Pi OS Lite x64:

``` sh
$ cat /etc/os-release | grep PRETTY_NAME
PRETTY_NAME="Debian GNU/Linux 13 (trixie)"

$ uname -a
Linux player 6.12.75+rpt-rpi-v8 #1 SMP PREEMPT Debian 1:6.12.75-1+rpt1 (2026-03-11) aarch64 GNU/Linux
```

Configure the user to log into the console automatically.

``` sh
$ sudo raspi-config

# System Options -> Auto Login -> ...automatically log in to the console? -> Yes
```

Install prerequisites:

``` sh
$ sudo apt update
$ sudo apt install -y elixir x11-xserver-utils xterm mpv aosd_cat libvips-tools
```

Download `bash_profile`, `easytv.exs`, and `.xinitrc` from this repo:

``` sh
$ cd
$ wget https://raw.githubusercontent.com/ccammack/easytv/main/.bash_profile
$ wget https://raw.githubusercontent.com/ccammack/easytv/main/easytv.exs
$ chmod +x ./easytv.exs
$ wget https://raw.githubusercontent.com/ccammack/easytv/main/.xinitrc
$ chmod +x ./.xinitrc
```

The `.bash_profile` just starts X:

``` sh
$ cat .bash_profile
# ~/.bash_profile

if [[ -f ~/.bashrc ]]; then
  . ~/.bashrc
fi

if [[ -z "$DISPLAY" && "$(tty)" == "/dev/tty1" ]]; then
  exec startx
fi
```

The `.xinit` file runs `easytv` with one or more *media root* directories:

``` sh
$ cat .xinitrc
#!/bin/sh

exec $HOME/easytv.exs /mnt/media/ 2>&1 | systemd-cat -t easytv
```

In this `.xinit`, */mnt/media* is a mount point for a [TrueNAS](https://www.truenas.com/) SMB share, which is mounted as read-only using a root-owned credentials file:

``` sh
$ sudo apt install -y cifs-utils
$ sudo mkdir -p /mnt/media

$ sudo vi /root/.smbcredentials
$ sudo cat /root/.smbcredentials
username=USERNAME
password=PASSWORD
domain=WORKGROUP

$ sudo chmod 600 /root/.smbcredentials

$ sudo vi /etc/fstab
$ sudo cat /etc/fstab
proc            /proc           proc    defaults          0       0
PARTUUID=4ea15ea7-01  /boot/firmware  vfat    defaults          0       2
PARTUUID=4ea15ea7-02  /               ext4    defaults,noatime  0       1
//TRUENAS/media  /mnt/media  cifs  credentials=/root/.smbcredentials,iocharset=utf8,uid=ccammack,gid=ccammack,file_mode=0444,dir_mode=0555,ro,nofail,x-systemd.automount,_netdev  0  0

$ sudo mount -a
mount: (hint) your fstab has been modified, but systemd still uses
       the old version; use 'systemctl daemon-reload' to reload.
$ sudo systemctl daemon-reload

$ sudo mount -a

$ ls -l /mnt/media
total 0
dr-xr-xr-x 2 ccammack ccammack 0 May 16 05:05 scrapbook

$ touch /mnt/media/test
touch: cannot touch '/mnt/media/test': Read-only file system
```

# Hotkeys

`easytv` supports 6 hotkeys. Use FLIRC's configuration software to map remote buttons to these keyboard keys:

| Hotkey   | Behavior                         |
| ---------| -------------------------------- |  
| Spacebar | Toggle playback and screen power |
| +        | Volume up                        |
| -        | Volume down                      |
| m        | Toggle volume mute               |
| PageUp   | Next channel                     |
| PageDown | Prev channel                     |

# Development

To run the script manually over `ssh`, add a line to `.xinitrc` to start an X11 app like `xterm` instead of `easytv` on login. This will start X and assign values to `DISPLAY` and `XAUTHORITY`:

``` sh
$ cat .xinitrc
#!/bin/sh

exec xterm -fa Monospace -fs 12
# exec $HOME/easytv.exs /mnt/media/ 2>&1 | systemd-cat -t easytv
```

Specify `DISPLAY` and `XAUTHORITY` when running over `ssh`:

```
$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv.exs --scan /mnt/media
```

Use `journalctl` to view the logs:

```
$ journalctl -t easytv -f
$ journalctl -t easytv --since "1 hour ago"
```

Stop a running instance and delete the current state files to rescan and replay media from the beginning:

```
$ pkill easytv
$ pkill mpv
$ rm -rf .local/state/easytv/
```

# References

* https://github.com/danya02/babooshka-tv
* https://github.com/cwilliamsbiz/DadTV
* https://github.com/shane-mason/FieldStation42
* https://github.com/ErsatzTV
