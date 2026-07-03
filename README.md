# Introduction

`easytv` wraps [mpv](https://github.com/mpv-player/mpv) to create a minimal TV appliance for Raspberry Pi.
I run it on a Raspberry Pi 4B v1.2 4GB at 1080P using [FLIRC](https://flirc.tv/) and an EZclicker 6-button remote.

The application calls external shell scripts to display OSD messages using `aosd_cat`, change volume using `amixer`, and control TV power using  `dpms` or `cec-client`.
Aside from that, it is not really user-configurable and may not run on anything else without modification.

The application defines *channels* by convention;
it accepts a list of *media root* directories passed on the command line and treats each child directory below a *media root* as a *channel*.
For example, given the directory structure shown below, passing `/mnt/media` as a *media root* will create *channels* called `Movies (1940s)` and `Scrapbook`.

Hidden files and directories starting with `.` will be skipped, so `.inbox` will not act as a *channel*.

``` sh
/mnt/media
├── .inbox
├── Movies (1940s)
│   ├── Casablanca (1942)
│   └── The-Maltese-Falcon (1941)
├── Scrapbook
│   ├── iphone
│   ├── 8mm family videos
│   └── digital frame media
```

The files in each *channel* subtree, at any depth, will be played as a long looping playlist when that *channel* is active.

Switching *channels* will save the current playhead position so it can be restored later when the *channel* is resumed.

The *power off* command will save the current playhead position and send a poweroff signal to the connected TV over HDMI.

-- TODO: I cover the IR sensor on the TV to allow the Pi to control it.

# Installation

Start with a fresh install of Raspberry Pi OS Lite x64.
Enable `ssh` when writing the microSD card using the [Raspberry Pi Imager](https://www.raspberrypi.com/software/).

Connect the Pi to a USB keyboard and to the TV over HDMI, boot, and log in over `ssh`:

```sh
ccammack@host:~$ ssh rpi
Linux rpi 6.12.75+rpt-rpi-v8 #1 SMP PREEMPT Debian 1:6.12.75-1+rpt1 (2026-03-11) aarch64

The programs included with the Debian GNU/Linux system are free software;
the exact distribution terms for each program are described in the
individual files in /usr/share/doc/*/copyright.

Debian GNU/Linux comes with ABSOLUTELY NO WARRANTY, to the extent
permitted by applicable law.
Last login: Sun Jun 21 05:59:21 2026 from 2607:fb90:4080:a836:5487:6fea:0:c09

$ pwd
/home/ccammack

$ whoami
ccammack

$ cat /etc/os-release | grep PRETTY_NAME
PRETTY_NAME="Debian GNU/Linux 13 (trixie)"

$ uname -a
Linux rpi 6.12.75+rpt-rpi-v8 #1 SMP PREEMPT Debian 1:6.12.75-1+rpt1 (2026-03-11) aarch64 GNU/Linux
```

Configure the user to log into the console automatically:

``` sh
$ sudo raspi-config

# System Options -> Auto Login -> ...automatically log in to the console? -> Yes
```

Configure HDMI audio output using PulseAudio:

$ sudo apt install pulseaudio

$ systemctl --user enable pulseaudio
Created symlink '/home/ccammack/.config/systemd/user/default.target.wants/pulseaudio.service' → '/usr/lib/systemd/user/pulseaudio.service'.
Created symlink '/home/ccammack/.config/systemd/user/sockets.target.wants/pulseaudio.socket' → '/usr/lib/systemd/user/pulseaudio.socket'.
$ systemctl --user enable pulseaudio.socket

$ pactl list sinks short
0       alsa_output.platform-fe00b840.mailbox.stereo-fallback   module-alsa-card.c      s16le 2ch 48000Hz       RUNNING
1       alsa_output.platform-fef00700.hdmi.hdmi-stereo  module-alsa-card.c      s16le 2ch 44100Hz       SUSPENDED



$ pactl set-default-sink <sink-name>
$ speaker-test -t sine -f 440

If you want the default sink to persist across reboots, you can either:

Re-run pactl set-default-sink ... from a small script on startup, or

Put a set-default-sink line into /etc/pulse/default.pa so PulseAudio initializes with that default

---

Configure audio output on `hdmi-0` using `raspi-config`.
On the Pi 4B, `hdmi-0` is the one in the middle, next to the USB power port.

```sh
$ sudo raspi-config

# System Options -> Audio -> *-hdmi-0
```

Configure video output on that port to 720p by appending `video=HDMI-A-1:1280x720` to the end of `/boot/firmware/cmdline.txt`:

``` sh
$ sudo nano /boot/firmware/cmdline.txt
[...]

$ sudo cat /boot/firmware/cmdline.txt
console=serial0,115200 console=tty1 root=PARTUUID=4ea15ea7-02 rootfstype=ext4 fsck.repair=yes rootwait ds=nocloud;i=rpi-imager-1778412046820 cfg80211.ieee80211_regdom=US video=HDMI-A-1:1280x720
```

Install prerequisites:

``` sh
$ sudo apt update
$ sudo apt install -y git elixir x11-xserver-utils xterm mpv aosd_cat libvips-tools cec-utils
```

Clone the repo:

-- TODO: clone the repo

``` sh
$ git clone
[...]

$ cd easytv
$ pwd
/home/ccammack/easytv
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

The default `.xinit` file just runs `xterm` for use during installation,
but also contains a commented line to run `easytv` with `/mnt/media/` as its *media root* directory:

``` sh
$ cat .xinitrc
#!/bin/sh

exec xterm -maximized   
# exec systemd-cat -t easytv "$HOME"/easytv.exs --scan /mnt/media/ 2>&1
```

Copy `.bash_profile` and `.xinit` from `easytv` to the `$HOME` directory, then run `startx` to start X Windows and `xterm`:

``` sh
$ cp .bash_profile $HOME
$ cp .xinitrc $HOME
$ startx
```

Use `cec_client` to get the logical device numbers for the TV and the RPi, the change `easytv-power.sh` to use those device numbers:

``` sh
$ echo "scan" | cec-client -d 1 -s -o easytv
log level set to 1
using osd name easytv
opening a connection to the CEC adapter...
requesting CEC bus information ...
CEC bus information
===================
device #0: TV
address:       0.0.0.0
active source: no
vendor:        Unknown
osd string:    TV
CEC version:   1.4
power status:  in transition from standby to on
language:      ???

device #1: Recorder 1
address:       1.0.0.0
active source: no
vendor:        Pulse Eight
osd string:    easytv
CEC version:   1.4
power status:  on
language:      eng

device #4: Playback 1
address:       2.0.0.0
active source: yes
vendor:        Toshiba
osd string:    Fire TV Stick
CEC version:   1.4
power status:  on
language:      ???

currently active source: Playback 1 (4)

$ nano easytv-power.sh
[...]

$ cat -8 easytv-power.sh
[...]
```

# Test Equipment Control Scripts

`easytv` relies on three shell scripts to control the connected TV.
These can be modified as needed to support different configurations.
After `xterm` starts, test a few of the included scripts to see what they do.

* `easytv-osd.sh` displays on-screen messages
* `easytv-power.sh` turns the connected TV on and off
* `easytv-volume.sh` adjusts the volume

## OSD

The `easytv-osd.sh` script uses `aosd_cat` to display text messages on screen:

``` sh
$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv-osd.sh Hello, World!
```

## Power

The `easytv-power.sh` script uses `cec-client` to control the TV power state using HDMI-CEC.
The repo includes a sample script that uses `dpms` instead, intended for use with a computer monitor.

``` sh
$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv-power.sh 0
$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv-power.sh 1
```

## Volume

My remote works to control the TV volume directly over IR, so the default `easytv-volume.sh` is just a placeholder.
The repo includes a sample script to adjust the volume on the Pi using `amixer` instead if needed.

``` sh
$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv-volume.sh +
$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv-volume.sh -
```

# Test Main Application

If the shell scripts control the TV properly, download some sample videos and test the main application:

``` sh
$ curl http://vjs.zencdn.net/v/oceans.mp4 --create-dirs --output /tmp/media/ocean/video.mp4
$ curl https://media.w3.org/2010/05/sintel/trailer.mp4 --create-dirs --output /tmp/media/sintel/video.mp4
$ curl https://samplelib.com/lib/preview/mp4/sample-5s.mp4 --create-dirs --output /tmp/media/sample/video.mp4

$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv.exs --scan /tmp/media/ 2>&1
[...]

$ pkill easytv
$ pkill mpv
```

# Test Hotkeys

`easytv` supports 7 keyboard hotkeys, which should work using the keyboard connected to the Pi.
Use [FLIRC](https://flirc.tv/)'s configuration software to map remote buttons to these keyboard keys:

| Hotkey   | Behavior                         |
| ---------| -------------------------------- |  
| Spacebar | Toggle playback and screen power |
| +        | Volume up                        |
| -        | Volume down                      |
| m        | Toggle volume mute               |
| PageUp   | Next channel                     |
| PageDown | Prev channel                     |
| q        | Quit                             |

# Deploy

After everything works, organize your media files.
They can be placed directly on the Pi, or on an external drive, or accessed over the LAN with proper configuration.
The next section explains how to mount `/mnt/media` to a NAS on the LAN over `smb`.

Change the `.xinitrc` file in the `$HOME` directory to run `easytv` instead of `xterm` on login.
Change the *media root* path from `/mnt/media` to something else if needed.

After rebooting, `easytv` should automatically start and begin playing media.

``` sh
$ nano $HOME/.xinitrc
[...]

$ cat $HOME/.xinitrc
#!/bin/sh

# exec xterm -maximized   
exec systemd-cat -t easytv "$HOME"/easytv.exs --scan /mnt/media/ 2>&1

$ sudo reboot
```

# Organize Media

In my `.xinitrc`, */mnt/media* is a mount point for a [TrueNAS](https://www.truenas.com/) SMB share, which I mounted as read-only using a root-owned credentials file:

``` sh
$ sudo apt install -y cifs-utils
$ sudo mkdir -p /mnt/media

$ sudo nano /root/.smbcredentials
[...]

$ sudo cat /root/.smbcredentials
username=USERNAME
password=PASSWORD
domain=WORKGROUP

$ sudo chmod 600 /root/.smbcredentials

$ sudo nano /etc/fstab
[...]

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

# Development Over `ssh`

The deployed user configuration described here makes it difficult to start and stop the application manually for testing.

To work around this, temporarily change `.xinitrc` to run `xterm` rather than `easytv`:

``` sh
$ cat .xinitrc
#!/bin/sh

exec xterm
# exec systemd-cat -t easytv "$HOME"/easytv.exs --scan /mnt/media/ 2>&1

$ sudo reboot
```

After doing this, it will be possible to run and `pkill` the application as needed for development and testing.

``` sh
$ DISPLAY=:0 XAUTHORITY=$HOME/.Xauthority ./easytv.exs --scan /mnt/media
[...]

$ pkill easytv
$ pkill mpv
```

# Application State

The application's media cache and playback state are stored in `~.local/state/easytv`. Delete those files to restart everything from scratch.

# Logs

Use `journalctl` to view the logs:

``` sh
$ journalctl -t easytv -f
$ journalctl -t easytv --since "1 hour ago"
```

# Coexisting with Other HDMI Devices

The configuration described here works very well if the RPi is the only device attached to the TV.
Adding other devices can lead to contention.
I also have a Firestick on the TV as HDMI-2, and it seems that the Firestick will notice the TV turning on and try to make itself the active input.
To work around this, the `easytv-power.sh` script runs a background process to seize the input as long as media is playing.

# References

* https://github.com/danya02/babooshka-tv
* https://github.com/cwilliamsbiz/DadTV
* https://github.com/shane-mason/FieldStation42
* https://github.com/ErsatzTV
