# easytv

**Raspberry Pi TV appliance for a six-button remote**

easytv wraps [mpv](https://github.com/mpv-player/mpv) to create a minimal
TV appliance for Raspberry Pi. It was developed on a Raspberry Pi 4B v1.2 
with 4 GB of RAM at 720p, using [FLIRC](https://flirc.tv/) and an EZclicker 
six-button remote.

Channels are defined by directory layout rather than configuration.
Pass one or more *media root* directories on the command line, and each
immediate child directory containing media is treated as a *channel*.
For example, passing `/mnt/media` as a *media root* defines two channels
from the directory structure below: **Movies (1940s)** and **Scrapbook**.

```text
/mnt/media
├── .inbox
├── Movies (1940s)
│   ├── ...
│   └── ...
├── Scrapbook
│   ├── ...
│   ├── ...
│   └── ...
```

Videos, music, and photos anywhere beneath a channel directory form one
sorted, looping playlist. Nested directories organize the media; they do
not create additional channels. Hidden files and directories beginning
with `.` are skipped, so `.inbox` will not be treated as a channel.

## Platform support

easytv was written for Raspberry Pi and tested on Raspberry Pi OS Lite. 
No desktop environment is needed: the supplied service starts a minimal 
Xorg session on the first console. Other Linux systems may work, but have
not been tested.

## Optional HDMI-CEC control

HDMI-CEC is optional. Without it, easytv plays continuously, even while
the TV is off. This works well when the Raspberry Pi is the TV's only
connected device: FLIRC handles channel changes, and the remote controls
TV power and volume directly.

For TV-aware playback, easytv relies on
[cecmond](https://github.com/ccammack/cecmond), installed and running on
the Pi. Its setup instructions cover enabling CEC on the TV and checking
communication before connecting easytv.

The supplied easytv service already includes `--cec-url` to connect to
cecmond. A different cecmond address or port requires a matching change
to this option.

With CEC connected, easytv can stop playback when the TV enters standby
or another device becomes active, resume when the TV selects the Pi,
and request the Pi's HDMI input when a channel button is pressed.
This is especially useful when other HDMI devices share the TV.

CEC behavior varies by TV and connected equipment. These actions are
not guaranteed; basic playback and FLIRC channel control do not require CEC.

## Build and install over SSH

Start with a fresh **64-bit Raspberry Pi OS Lite** installation.
Enable `ssh` when writing the microSD card using the 
[Raspberry Pi Imager](https://www.raspberrypi.com/software/).

Log in as a regular user over `ssh` and install the application 
from source:

```sh
sudo apt update
sudo apt install -y git elixir erlang-dev build-essential
elixir --version # >= Elixir 1.14
git clone https://github.com/ccammack/easytv.git
cd easytv
mix local.hex --force
mix deps.get
mix test
MIX_ENV=prod mix release --overwrite
bash scripts/install.sh --check
sudo bash scripts/install.sh
```

The installer installs runtime dependencies, creates a non-login `easytv`
service account, and configures display, audio, and keyboard permissions.
The release is installed at `/opt/easytv`, user configuration at
`/etc/easytv/config.exs`, and playback state under `/var/lib/easytv`.

Installation does not start the service or enable it at boot. Configure
display, audio, and media before starting playback.

All attached keyboards, including FLIRC, can control easytv together.
Connect them before starting; restart the service after attaching another
keyboard. easytv reads physical keyboard input directly, not commands typed
over `ssh`.

## Configure HDMI audio and 720p video

On the Rpi 4B, HDMI0 is the port closest to the power connector.
Configure that port as the audio output:

```sh
sudo env SUDO_USER=easytv raspi-config
```

Choose **System Options → Audio**, then HDMI0, usually named
`vc4-hdmi-0`. Finish and exit the menu.

Playback at high resolutions can be choppy on the Pi, so create
an Xorg monitor file to set its resolution to **1280 x 720 (720p)**.

```sh
sudo mkdir -p /etc/X11/xorg.conf.d
sudo nano /etc/X11/xorg.conf.d/10-easytv-monitor.conf
```

Configure the resolution mode, which must be supported by the TV.
`HDMI-1` is the usual Xorg name for the Pi 4B's HDMI0 connection.

```text
Section "Monitor"
    Identifier "HDMI-1"
    Option "PreferredMode" "1280x720"
EndSection
```

This sets the output resolution, but the Pi may still have trouble
decoding very high resolution source media.

## Initial playback test with local media

Use the provided script to generate some test channels under `/tmp/media`:

```sh
sudo bash scripts/generate-test-media.sh
ls -R /tmp/media
```

The default configuration uses `/mnt/media` as a *media root*, so 
temporarily bind-mount `/tmp/media` at `/mnt/media`. First, check that 
`/mnt/media` is not already mounted. Continue with the next step only if the 
output indicates that it does not exist or is not a mountpoint.

```sh
mountpoint /mnt/media
```

If the check confirms `/mnt/media` is not mounted, bind the temporary media
there. Any existing files will be temporarily hidden by this, but not changed.

```sh
sudo mkdir -p /mnt/media
sudo mount --bind /tmp/media /mnt/media
findmnt --mountpoint /mnt/media
sudo -u easytv ls /mnt/media
```

Connect a USB keyboard to the Pi and start the service. Once the video 
starts, use `PgUp` and `PgDown` on the keyboard to change channels.

```sh
sudo systemctl start easytv.service
```

After testing, stop the service and unmount the temporary media:

```sh
sudo systemctl stop easytv.service
sudo umount /mnt/media
```

The test files can be removed from `/tmp` if no longer needed.

To configure real media for local playback, choose one of these approaches:

* Copy media to channel subdirectories under `/mnt/media`.
* Store media elsewhere, then update both the media path in `ExecStart`
  and `RequiresMountsFor` in `systemd/easytv.service`. Rebuild and reinstall
  using the update instructions below.
* Store media elsewhere and configure a persistent bind mount at
  `/mnt/media` in `/etc/fstab`.

Once the media is accessible to the `easytv` account, start the service
and enable automatic startup at boot:

```sh
sudo systemctl start easytv.service
sudo systemctl enable easytv.service
```

For playback from a network SMB share, follow the next section instead.

## Mount SMB share as a media root

To play media from a LAN-hosted SMB share, configure */mnt/media* as
a read-only mount point using a root-owned credentials file:

``` sh
sudo apt install -y cifs-utils
sudo mkdir -p /mnt/media
sudo nano /root/.smbcredentials
```

Enter the domain and access credentials for the SMB share:

```sh
username=USERNAME
password=PASSWORD
domain=WORKGROUP
```

Set the file mode and configure `/etc/fstab`:

```sh
sudo chmod 600 /root/.smbcredentials
sudo nano /etc/fstab
```

Append this line to `/etc/fstab`, replacing `TRUENAS` and `media` 
with the NAS hostname and share name:

```sh
//TRUENAS/media  /mnt/media  cifs  credentials=/root/.smbcredentials,iocharset=utf8,uid=easytv,gid=easytv,file_mode=0444,dir_mode=0555,ro,nofail,x-systemd.automount,_netdev  0  0
```

Mount the share, list its contents, and make sure it is read-only.
Confirm that the output matches the SMB share and includes `ro` 
in the mount options. No matching entry means the check failed; resolve the
mount issue before starting easytv.

```sh
sudo systemctl daemon-reload
sudo mount -a
sudo -u easytv ls /mnt/media
findmnt -rn -t cifs --mountpoint /mnt/media -o TARGET,SOURCE,FSTYPE,OPTIONS
```

Start the service and configure it to start automatically on boot:

```sh
sudo systemctl start easytv.service
sudo systemctl enable easytv.service
sudo systemctl status easytv.service
```

## Configure and update from source

Edit configuration in the checkout:

* `priv/easytv.config.exs` for keyboard mappings and CEC behavior.
* `systemd/easytv.service` for media paths and startup arguments.
* `systemd/70-easytv-remote.rules` for keyboard permissions, if needed.

Rebuild and deploy:

```sh
mix test
MIX_ENV=prod mix release --overwrite
bash scripts/install.sh --check
sudo bash scripts/install.sh
sudo systemctl start easytv.service
```

The installer validates the release and config, stops playback, and replaces
installed files. Playback state is preserved. Installation leaves the service
stopped and does not change boot enablement.

Edit checkout files rather than installed copies: installation replaces them,
and integrity checks reject locally modified installed files. If deployment
fails, leave the service stopped and resolve the reported error before retrying.
The read-only `--check` does not verify actual playback, display, audio, or input.

## Uninstalling

Uninstall stops/disables easytv, removes the managed release, config, unit,
input rule, and manifest, and restores the tty1 console login. Media, saved
state, the service account, prerequisite packages, and cecmond are retained.
The source checkout is not removed.

```sh
bash scripts/uninstall.sh --check
sudo bash scripts/uninstall.sh
```

## Configure EZclicker remote

Follow manufacturer instructions to configure the remote to control the
TV's power and volume. The TV should ignore the Channel buttons when connected
to an HDMI source.

## Configure FLIRC

Use [FLIRC](https://flirc.tv/)'s configuration software to map the IR signals 
from the remote's Channel buttons to the PgUp and PgDown keyboard keys.

## Refreshing the media catalog

After adding or removing media, restart easytv to rebuild the catalog.
The supplied service includes `--scan`, so it scans the configured *media
roots* each time it starts.

```sh
sudo systemctl restart easytv.service
```

## Troubleshooting

- **No picture:** Confirm that the TV has the Pi's HDMI input selected.
  Check service status with the command below. The configured display
  mode must also be supported by the TV.
- **Picture but no sound:** Confirm that the TV is not muted and that
  HDMI audio is configured for the port connected to the TV.
- **No channels or missing media:** Media must be inside channel
  subdirectories, readable by the `easytv` account, and have recognized
  file extensions. Restart easytv after adding or removing files.
- **Channel buttons do nothing:** Test with PgUp and PgDown on a USB
  keyboard. If those work, check FLIRC's button mappings. Input devices
  must be connected before easytv starts; restart the service after
  attaching another device.
- **Network media is unavailable:** Repeat the checks in
  [Mount SMB share as a media root](#mount-smb-share-as-a-media-root).
  Confirm that `/mnt/media` points to the intended share and can be
  read by the `easytv` account.
- **TV power or input changes do not affect playback:** Confirm that
  cecmond is running and follow its
  [CEC testing instructions](https://github.com/ccammack/cecmond).
  CEC behavior varies by equipment.

Check whether the service is running:

```sh
systemctl status easytv.service --no-pager
```

Inspect recent application logs:

```sh
sudo journalctl _UID=$(id -u easytv) -b -n 60 --no-pager
```

Application logs may belong to the service account's session rather than
the systemd unit, so `journalctl -u easytv.service` may show only startup
and shutdown messages.

## Customizing behavior

### Configuration script

[priv/easytv.config.exs](priv/easytv.config.exs) defines easytv's behavior:
how remote buttons control playback, how the application reacts to TV source
changes, and how mpv command results are handled. Customize this script to
change those behaviors without modifying the application.

Incoming events are handled by three callbacks:

* `on_key/2` handles keyboard presses, releases, and repeats.
* `on_cec/2` handles CEC observations, including TV standby and source changes.
* `on_mpv/2` handles results from commands sent to mpv.

Each callback receives an event and the current script state, then returns
the updated state. State is shared between callbacks, so an action can begin
with a keypress and continue when a response arrives. It is not preserved
across application restarts.

Outgoing actions use the control API described below. The script decides
what to do; easytv handles execution and communication.

The configuration script is ordinary Elixir code. Edit it in the checkout, 
then rebuild and reinstall using the update instructions above.

### Control API

[EasyTV.Controls](lib/easytv/controls.ex) exposes:

* `channel_next/0` and `channel_previous/0` to select channels.
* `stop/0` and `resume/0` to stop playback and resume the current channel.
* `current_media/0` to synchronously inspect the active item. It returns
  `{:ok, %{path: original_path, kind: kind, playback_id: id}}`, where `kind`
  is `:image`, `:audio`, or `:video`, based on the original file extension.
  With no active player it returns `{:error, :no_player}`; with no playback
  owner it returns `{:error, :not_running}`.
* `send_mpv_command/1` to send mpv command lists, such as
  `["get_property", "volume"]` or `["add", "volume", 5]`.
* `send_cec_command/1` to send command maps supported by cecmond, such as
  `%{command: "active_source"}`.
* `cec_enabled?/0` to check whether CEC integration is enabled, not whether
  cecmond is reachable.

Playback actions and command senders are asynchronous. Playback actions
return `:ok` on dispatch; command senders return `{:ok, ref}`.
Immediate failures return `{:error, reason}`.

Current-media information is a snapshot. The playback ID changes whenever
the player is replaced, even if the same file plays again. Comparing IDs can
detect an item change, but does not guarantee a later command targets that item.

mpv command results arrive through `on_mpv/2`, with the returned reference
identifying the request. Store that reference in script state when a result
requires a follow-up action. Handle both success and failure to clear any
pending operation.

CEC transmission results are logged rather than delivered to a script
callback. Incoming CEC messages arrive separately through `on_cec/2`.

## Logging

The installed service writes logs to the system journal. Show recent logs
from the current boot:

```sh
sudo journalctl _UID=$(id -u easytv) -b -n 60 --no-pager
```

Follow new messages; press Ctrl-C to stop watching without stopping playback:

```sh
sudo journalctl _UID=$(id -u easytv) -f
```

Application logs may belong to the service account's session rather than
the systemd unit, so `journalctl -u easytv.service` may show only startup
and shutdown messages.

Production releases log informational messages, warnings, and errors.
For detailed diagnostics, add `Logger.configure(level: :debug)` inside
the user config's `config/0` function, then rebuild and reinstall.
Remove it and redeploy to restore normal logging.

## Appliance soak test

Run `scripts/soak.py` on the Pi against an already-running installed service.
It requires Python 3 and root access to inject Linux input events. It writes
PageUp/PageDown presses and releases to the existing FLIRC keyboard device;
it does not install a synthetic device, grab input, or restart easytv.

```sh
sudo python3 scripts/soak.py --duration 24h
sudo python3 scripts/soak.py --duration 48h --steady 2h --changing 20m
sudo python3 scripts/soak.py --duration 2m --steady 15s --changing 30s --skip-mute
```

## References

* [cecmond](https://github.com/ccammack/cecmond)
* [babooshka-tv](https://github.com/danya02/babooshka-tv)
* [DadTV](https://github.com/cwilliamsbiz/DadTV)
* [FieldStation42](https://github.com/shane-mason/FieldStation42)
* [ErsatzTV](https://github.com/ErsatzTV)
