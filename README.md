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
Xorg session on the first console. Other Linux systems may work, but the 
installer and console service are intended for Raspberry Pi OS Lite.

## Optional HDMI-CEC control

If the Raspberry Pi is the only device connected to the TV, easytv 
can run by itself. Select the Pi as the TV's active source and use
the remote to control TV power and volume. When deployed this way,
media playback will continue even while the TV is off.

For automatic playback control when the TV powers off or switches inputs,
run [cecmond](https://github.com/ccammack/cecmond) alongside easytv and 
enable it using `--cec-url`. This is especially useful when other HDMI 
devices share the same TV.

Deploying this way allows easytv to stop playback when the TV enters 
standby or another device becomes active, resume when the TV selects 
the Pi, and request the Pi's HDMI input when a channel button is pressed.
These actions depend on HDMI-CEC support from the TV and other connected
equipment to operate as expected.

# Build and install over SSH

Start with a fresh install of Raspberry Pi OS Lite.
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

# Configure HDMI audio and 720p video

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

# Initial playback test with local media

Use the provided script to generate some test channels under `/tmp/media`:

```sh
sudo bash scripts/generate-test-media.sh
ls -R /tmp/media
```

The default configuration uses `/mnt/media` as a *media root*, so 
temporarily bind-mount `/tmp/media` at `/mnt/media`. First, check that 
`/mnt/media` is not already mounted. Continue with the next step only if the 
output says it is not a mountpoint.

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

There are several ways to configure real media for local playback:

* copy real media to channel subdirectories under `/mnt/media`
* copy real media to channel subdirectories elsewhere and change `systemd/easytv.service` to play them instead of `/mnt/media` and redeploy
* copy real media to channel subdirectories elsewhere and mount that location as `/mnt/media` in `/etc/fstab`

See the next section to configure playback from a network SMB share.

# Mount a SMB share as a media root

To play media from a LAN-hosted SMB share, configure */mnt/media* as
a read-only mount point using a root-owned credentials file:

``` sh
sudo apt install -y cifs-utils
sudo mkdir -p /mnt/media
sudo nano /root/.smbcredentials
```

Enter the domain and credentials:

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

Append one new line to `/etc/fstab`, replacing `TRUENAS`, `media`, and
`USERNAME` with the NAS hostname, share name, and Pi account username:

```sh
//TRUENAS/media  /mnt/media  cifs  credentials=/root/.smbcredentials,iocharset=utf8,uid=USERNAME,gid=USERNAME,file_mode=0444,dir_mode=0555,ro,nofail,x-systemd.automount,_netdev  0  0
```

Mount the share, list its contents, and make sure it is read-only.
Confirm that the output matches the SMB share and includes `ro` 
in the mount options. No matching entry means the check failed; resolve the
mount issue before starting easytv.

```console
sudo systemctl daemon-reload
sudo mount -a
sudo -u easytv ls /mnt/media
findmnt -rn -t cifs --mountpoint /mnt/media -o TARGET,SOURCE,FSTYPE,OPTIONS
```

Restart the service and configure it to start automatically on boot:

```sh
sudo systemctl start easytv.service
sudo systemctl enable easytv.service
sudo systemctl status easytv.service
```

## Configure and Update from Source

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
The read-only --check does not verify actual playback, display, audio, or input.

## Uninstalling

Uninstall stops/disables easytv, removes the managed release, config, unit,
input rule, and manifest, and restores the tty1 console login. Media, saved
state, the service account, prerequisite packages, and cecmond are retained.
The source checkout is not removed.

```sh
bash scripts/uninstall.sh --check
sudo bash scripts/uninstall.sh
```

## Configure EZclicker Remote

Follow manufacturer instructions to configure the remote to control the
TV's power and volume. The TV should ignore the Channel buttons when connected
to an HDMI source.

## Configure FLIRC

Use [FLIRC](https://flirc.tv/)'s configuration software to map the IR signals 
from the remote's Channel buttons to the PgUp and PgDown keyboard keys.

## Development

* soak test
* APIs for easytv, mpv, and cecmond
* user script
* flattened state machine

## Logging

# References

* https://github.com/danya02/babooshka-tv
* https://github.com/cwilliamsbiz/DadTV
* https://github.com/shane-mason/FieldStation42
* https://github.com/ErsatzTV

---
---
---
---
---
---
---
---
---



### Direct application actions

`EasyTV.Controls` exposes `channel_next/0`, `channel_previous/0`, `stop/0`,
and `resume/0` for in-process callers. Actions are asynchronous:
`:ok` means sent, not completed. Calling while the input process is absent
logs an error and returns `{:error, :not_running}`.

Channel actions retain the two-second preview delay. Stop cancels the pending preview, clears OSD, saves the playback
position, clears prepared-next tracking, and requests mpv stop without waiting
for the player to exit. Resume restores the current channel only when no
player is active. Keyboard channel events use the same action handlers.
Keyboard actions are selected by the user configuration described below.

### User configuration

Edit `priv/easytv.config.exs` in the checkout, then rebuild the release. It is
packaged with the application and loaded at startup. Alternatively, select an
external file explicitly:

```sh
mix easytv --config ./priv/easytv.config.exs --scan /mnt/media/
_build/prod/rel/easytv/bin/play --config /etc/easytv/config.exs /mnt/media/
```

The script defines `EasyTV.UserConfig.config/0`, returning a keyword list with
`:state`, `:on_key`, `:on_cec`, and `:on_mpv`. All callbacks receive `(event, state)` and
return the next state. State may be any Elixir term and is shared between all
callbacks; it is not persisted across application restarts.

Key callbacks receive individual `{:ev_key, key, value}` events, including
releases and repeats, as well as other Linux input events. The sample acts only
on key-down (`value == 1`), preserving the original mappings. Put catch-all
clauses last to log unmatched events and state. The CEC callback is registered,
and receives interpreted CEC observations when `--cec-url` is enabled.

`EasyTV.Controls.send_mpv_command/1` accepts an mpv command list, such as
`["get_property", "volume"]` or `["add", "volume", 5]`, and returns `{:ok, ref}`
after dispatch. Invalid commands return `{:error, :invalid_command}`; an absent
playback owner returns `{:error, :not_running}`. These failures are logged.
Each dispatched command produces one `on_mpv/2` result during the session:

```elixir
%{type: :reply, ref: ref, command: ["get_property", "volume"],
  result: {:ok, %{"error" => "success", "data" => 50}}}
```

Failures use `result: {:error, reason}`: an mpv error string, `:no_player`,
`:player_stopping`, `:player_replaced`, `:player_exited`, `:timeout`, or an IPC
failure reason. Store the reference in script state to correlate a later result
and send follow-up commands. Callback execution is serial, so results are handled
after the initiating callback returns its updated state.

Commands target the current player when the playback owner processes them.
They are not coalesced, retried, or carried over to a replacement player.
Outstanding requests are cancelled when playback stops or changes; commands
time out if no reply arrives. Internal position polls and unsolicited mpv events
are not delivered to this callback. Commands such as `loadfile` and `quit` can
interfere with managed playback; command policy belongs to the script.

Callbacks execute serially in a separate process, not in the playback or
preview process. Action APIs remain asynchronous. A slow callback or breakpoint
delays subsequent callbacks, but playback and preview timers continue. Pending
events can accumulate during a pause; this is not a whole-appliance pause.
Callback exceptions are logged with event/state and terminate the application,
rather than silently resetting the user's state. Missing or invalid scripts
fail startup. Config files are trusted executable code; do not load untrusted
files or call `Mix.install` inside them.

For VS Code debugging, install ElixirLS and use "Debug EasyTV config" from
`.vscode/launch.json`. Adjust the media directory and display environment for
your machine. The `requireFiles` entry loads the config module before the Mix
task, allowing breakpoints in its callback functions. If using another config,
change both `taskArgs` and `requireFiles`. Editing the script requires restarting
the application/debug session; hot reload is not supported.

### cecmond connection

Enable the optional connection to the separately installed daemon:

```sh
mix easytv --cec-url http://127.0.0.1:8080 /mnt/media/
```

EasyTV subscribes to `/events.ndjson` and passes interpreted observations to `on_cec/2`.
Keepalives are ignored. Disconnects, malformed records, and idle timeouts are
logged and delivered as `%{type: :observation_gap, reason: reason}`; the client
reconnects after two seconds without a replay token.

`EasyTV.Controls.send_cec_command/1` accepts a map, encodes it as JSON, and
asynchronously posts it to cecmond's `/commands` endpoint. The sample config's
`assert_active_source/0` helper sends `%{command: "active_source"}` when CEC
is enabled.
`{:ok, ref}` means dispatched, not transmitted; the HTTP result is logged.
The reference is reserved for future correlation; no completion callback is
currently delivered for CEC commands.
Each call sends independently, without coalescing or automatic retry.
HTTP 429 and uncertain outcomes are reported, not silently retried.
Non-map or non-JSON-encodable payloads are logged and return `{:error, :invalid_command}`.
Without `--cec-url`, calling this API logs an error and returns
`{:error, :not_running}`. Channel changes no longer invoke `cec-client` or
assert AS implicitly; the config decides when to request it.

The stream waits for each CEC callback to finish before requesting another HTTP
chunk. There is no callback deadline, so debugger pauses are allowed. During a
long pause, cecmond may disconnect a slow subscription; after continuing, EasyTV
reports the interruption and reconnects. Observations are live and cannot be
replayed. Network I/O runs outside playback and the callback dispatcher.
Use a local daemon; HTTPS connections verify certificates if explicitly used.

### CEC policy in the sample config

Observations have `:type`, `:packet`, and `:adapter` fields. Transport gaps have
`:type` and `:reason` instead. Raw cecmond JSON is retained in `:packet`.
Types include `:focus_lost`, `:focus_here`, `:tv_selected`, `:tv_standby`,
`:observation_gap`, `:invalid_packet`, `:unhandled_packet`, and `:unhandled_event`.

The interpreter checks received raw bytes, validation errors, and the packet's
current adapter identity. Another device's valid broadcast Active Source means
focus loss. TV-originated broadcast/addressed standby and standby power reports
are recognized; unrelated devices' standby is ignored. Explicit TV
`SET_STREAM_PATH` selecting our physical address means `:tv_selected`.
Routing-away messages are not interpreted as focus loss in this version.

The sample config stops on focus loss or TV standby. The first channel key
after either resumes the current channel and requests AS without stepping.
Later presses change channels. `:return_requested` means a request was made,
not confirmed TV selection. Explicit TV selection resumes; repeated selection
does not cancel a channel preview. Observation gaps leave playback unchanged
and reset focus to unknown. Adapter loss and capture diagnostics are gaps, not
proof that the TV is off. Edit the callback clauses to change these policies.

Use cecmond's `/adapter` endpoint to inspect the TV connection. Do not run
`cec-client` alongside cecmond: only cecmond should own the CEC adapter.
Customize the callbacks in `priv/easytv.config.exs` for your equipment.

# Deploy

`systemd/easytv.service` describes the dedicated appliance deployment. Use the
install scripts above; the unit is not installed by building the release alone.
It expects:

* The entire release at `/opt/easytv`.
* The checkout's config copied to `/etc/easytv/config.exs`.
* A non-login `easytv` account, with video/render/audio and keyboard read access.
* Read access to `/mnt/media` and writable systemd-managed state/runtime paths.
* Xorg, startx, mpv, vips, and aosd_cat installed on the host.

The service owns X on tty1 using a logind/PAM session; it conflicts with the
console getty while running. Do not also start EasyTV/X using console autologin
or login startup scripts. X resolution and mpv audio
configuration belong to the host/service account and are not changed by EasyTV.

The service uses foreground playback and `Restart=on-failure`. Explicit
`systemctl stop` does not restart it. cecmond is optional and is not started or
stopped by EasyTV; add `--cec-url http://127.0.0.1:8080` to the unit's application
arguments if using it. Edit the unit in the checkout before deployment.

After installation:

```sh
sudo systemctl start easytv.service
sudo systemctl stop easytv.service
sudo systemctl restart easytv.service
journalctl -u easytv.service -b --no-pager
```

## Lifecycle and shutdown

An OTP application supervisor owns a single runtime session. Input, config,
playback, OSD, and background tasks are supervised together. A session failure
stops the remaining children instead of restarting playback or resetting user
focus state silently; the foreground command exits unsuccessfully.

On application shutdown, inputs/CEC streaming and the callback dispatcher stop
before the playback owner. The owner saves the latest known position, clears
transitions, stops preparation work, and waits for player cleanup; OSD and task
supervisors then stop. Waiting is restricted to teardown: ordinary channel,
stop/resume, and CEC command APIs remain asynchronous.

SIGTERM requests Erlang's orderly shutdown.
The unit's `ExecStop` signals the recorded playback VM and waits for it to exit
before systemd tears down startx/X. This ordering matters because PAM/logind
moves playback into a session scope, outside the service's control group, and
xinit teardown alone does not guarantee a final state save.

During development,
Ctrl-C followed by `a` is an abrupt abort and does not guarantee a final state
save. For orderly shutdown from IEx, use `System.stop()`. SIGKILL/power loss
cannot run cleanup callbacks. The service also uses control-group cleanup, but session-scoped processes are
not covered by that alone. Check shutdown logs and remaining `easytv` processes
on the target host if graceful shutdown fails.

Application logs may be attributed to the logind session rather than
`easytv.service`. If `journalctl -u easytv.service` only shows startup, use
`sudo journalctl _UID=$(id -u easytv) -n 60 --no-pager` for service-user logs.


# Logs

Production releases log at `info` level: startup, playback transitions, and
user-config actions remain visible, along with warnings and errors. Detailed
channel previews, OSD/process cleanup, input events, and position polling are
debug diagnostics. Development (`mix easytv`) retains debug logging.

``` sh
sudo journalctl _UID=$(id -u easytv) -f
sudo journalctl _UID=$(id -u easytv) --since "1 hour ago" --no-pager
```

For temporary production diagnostics, add `Logger.configure(level: :debug)`
inside the user config's `config/0` function and redeploy. Remove it to restore
normal logging.

## Appliance soak test

Run `scripts/soak.py` on the Pi against an already-running installed service.
It requires Python 3 and root access to inject Linux input events. It writes
PageUp/PageDown presses and releases to the existing FLIRC keyboard device;
it does not install a synthetic device, grab input, or restart EasyTV.

```sh
sudo python3 scripts/soak.py --duration 24h
sudo python3 scripts/soak.py --duration 48h --steady 2h --changing 20m
sudo python3 scripts/soak.py --duration 2m --steady 15s --changing 30s --skip-mute
```
