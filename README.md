# EasyTV (Go)

EasyTV provides directory-based looping mpv channels for a Linux/Raspberry Pi appliance. The Go executable owns playback, persistence, optional Linux HDMI-CEC, optional HTTP controls/received-packet streams, and optional channel-preview overlays.

This README describes `cmd/easytv`. `easytv.exs` is retained as historical/reference source. The obsolete `volume.sh`, `power.sh`, and `channel.sh` hooks have been removed; Go controls mpv and CEC internally. The Go entrypoint does not install legacy evdev/FLIRC input handling.

The historical Elixir reference still contains calls to those retired hooks. It is not a runnable rollback package as checked out; use a complete backed-up deployment or restore a matching earlier revision if legacy rollback is needed.

## Validation boundary

Functional checkpoint `70ef2d5` adds opt-in owned OSD previews after the IPC-teardown and successful-start persistence fixes. Isolated validation passed with real mpv/null outputs, mock CEC, HTTP controls/streams, image timing/preparation, persistence/restart, SIGTERM, and fake-overlay process lifecycle tests.

Actual display/audio, real `aosd_cat` rendering/timing, physical CEC behavior, and unattended boot remain separate acceptance work. See [Go deployment and acceptance](docs/go-deployment.md) before changing a live installation.

## Clone and build

The appliance installation targets Raspberry Pi OS Lite with systemd, KMS graphics, and a working HDMI display/CEC adapter. No desktop environment is needed. The commands below use Raspberry Pi OS Lite Trixie; older releases may need a newer Go bootstrap toolchain from [go.dev](https://go.dev/doc/install).

Install the build prerequisites: Git, Make, CA certificates, and Go. A Go toolchain compatible with the `go 1.26.5` directive in `go.mod` is required. Trixie's Go package can download that version using Go's toolchain mechanism (requires Go 1.21 or newer):

```bash
sudo apt-get update
sudo apt-get install -y git make ca-certificates golang-go

# Download/select the required Go toolchain as your normal user.
export PATH="$(GOTOOLCHAIN=go1.26.5 go env GOROOT)/bin:$PATH"
go version
```

Replace `<repository-url>` with the URL of your EasyTV repository. Build as your normal user, not root:

```bash
git clone <repository-url> easytv
cd easytv
make
./bin/easytv --help
make test
```

`make` outputs `./bin/easytv`. `make clean` removes that binary. Builds/tests use the selected local toolchain with module-network access disabled; the application has no external Go module dependencies. The PATH export above lasts for the current shell; repeat it in a new shell if needed.

Help and building do not start playback or install anything. Ordinary Go tests do not access real CEC/display hardware; real-mpv integration tests are opt-in. Installer tests use an isolated filesystem when `bwrap` is already available, otherwise report that those scenarios were skipped.

## Install on Raspberry Pi OS Lite

From the built checkout:

```bash
sudo make install
```

The installer installs these runtime prerequisites using apt:

- `mpv` for playback.
- `libvips-tools` for `vipsheader` and `vipsthumbnail` image preparation.
- `aosd-cat` and fonts for channel previews.
- Minimal Xorg, `xinit`/`startx`, `xauth`, and `libpam-systemd` for the console X session.

CEC is part of EasyTV's Go implementation: no separately cloned CEC project, `cec-client`, or libCEC daemon is needed.

The installer creates the non-login `easytv` user/group, adds `video`, `render`, and `audio` hardware access, installs a root-owned `/usr/local/bin/easytv`, and installs/enables `easytv.service`. State/home is private to the service at `/var/lib/easytv`; systemd creates `/run/easytv` for temporary workspace and X authorization. No audio server, mixer hooks, or system-volume configuration is installed by EasyTV.

**Installation enables next-boot startup but deliberately does not start/restart playback or take over your current console.** It does not configure media mounts, change media ownership, or modify your login profiles. Reinstalling updates the binary and base unit; local systemd drop-ins and saved state are preserved.

### Review paths and start

The supplied service uses `/dev/cec0`, `/mnt/media`, X display `:0` on `tty1`, channel overlays, a catalog rescan on every start, and HTTP controls bound to `127.0.0.1:8080`. Verify the correct CEC device and prepare the media before starting or rebooting:

```bash
ls -l /dev/cec*
ls -la /mnt/media
sudo -u easytv test -r /dev/cec0
sudo -u easytv test -w /dev/cec0
sudo -u easytv ls /mnt/media
```

Each media-root child directory is a channel (see below); files must be readable and directories traversable by `easytv`. Mount external storage persistently, for example through `/etc/fstab`, before starting. Use appropriate read-only permissions, filesystem mount options, or an existing media group; do not recursively hand ownership of your library to the service or make devices world-writable. The default CEC node normally belongs to `video`; if yours differs, grant narrowly scoped access through the appliance's device/group configuration. Do not run another CEC controller alongside EasyTV.

All application arguments live in the systemd command line, not a separate config file. For different paths/options:

```bash
sudo systemctl edit easytv.service
```

For example, replace the command and mount dependency as follows (keep all options before the media roots):

```ini
[Unit]
RequiresMountsFor=
RequiresMountsFor=/srv/media

[Service]
ExecStart=
ExecStart=/usr/bin/startx /usr/local/bin/easytv --cec-device=/dev/cec1 --state-dir=/var/lib/easytv --temp-dir=/run/easytv --listen=127.0.0.1:8080 --osd --scan /srv/media -- /usr/bin/Xorg :0 vt1 -keeptty -nolisten tcp -s 0 -dpms
```

Supply every media root both in the command and in `RequiresMountsFor`. Omit `--listen` to disable HTTP, or `--osd` to disable previews. Remove `--scan` to reuse the cached catalog rather than refresh on every start.

Start from SSH or another console: the service reserves `tty1` and stops its login prompt while running. Do not use it alongside a desktop/display manager or another X session on that display.

```bash
sudo systemctl daemon-reload
sudo systemctl restart easytv.service
systemctl status easytv.service --no-pager
journalctl -u easytv.service -b --no-pager -n 100
```

Once physical display/CEC acceptance is complete, reboot to check unattended startup:

```bash
sudo reboot
```

The service uses `startx` to authorize and start X without `.xinitrc` or `.bash_profile`; there is no shell autologin requirement. It waits for the configured media mounts, disables X screen blanking/DPMS, logs to the journal, and restarts the session after exit. EasyTV attempts playback automatically, but does **not** wake the television or select its HDMI input at startup. A running service is not proof of visible output; validate on the target Pi.

### Audio responsibility

EasyTV lets mpv select its audio backend/device. Volume and mute requests only send mpv IPC commands; EasyTV does not change OS or TV volume, select an audio route, or repair sound configuration. Installing mpv alone does not guarantee audible playback: the OS's audio setup and the `easytv` user's device/session access must already work. Diagnose silent playback or ineffective volume controls with mpv and Raspberry Pi OS, outside EasyTV.

mpv is launched with `--no-config`, so ordinary mpv configuration files are not loaded. Volume/mute changes apply to the current player and are not retained across player replacement.

### Stop and uninstall

```bash
sudo systemctl stop easytv.service
sudo make uninstall
```

The scripts can also be run with `sudo bash scripts/install.sh` or `sudo bash scripts/uninstall.sh`. Uninstall stops/disables the service and removes the installed binary/base unit. It preserves media, `/var/lib/easytv`, the account/groups, runtime prerequisite packages, and local systemd overrides. Reboot to restore the normal `tty1` login. Neither installation nor uninstallation requires running the Go compiler as root.

## Media and catalog

Each non-hidden immediate child directory of a media root is a channel. Recognized media files are collected recursively and sorted; hidden entries, symbolic-link entries, and empty channels are skipped.

```text
media-root/
  Movies/
    first.mp4
    archive/second.mkv
  Scrapbook/
    photo.jpg
  .inbox/
```

Pass the root, not each channel, as the positional argument. Multiple roots are supported. Absolute media/channel paths are persistent identities; moving files or changing roots can affect resume.

`--scan` rebuilds the catalog using the supplied roots: include every root you want retained. Without it, existing cached roots are reused and missing roots are scanned. Cached roots are not automatically refreshed when files appear.

### Rescan after changing media

After adding, removing, or moving media, SSH into the Pi and request a live rescan:

```bash
curl --fail-with-body -i -X POST http://127.0.0.1:8080/rescan
journalctl -u easytv.service -b --no-pager -n 100
```

HTTP must be enabled (`--listen=127.0.0.1:8080` in the supplied service). No request body or media-root arguments are needed: the scan uses all roots configured on the running instance.

The filesystem scan runs in the background while playback and controls continue. After a successful scan, the playback owner cancels pending channel previews/preparation, stops its current player, installs the refreshed catalog, and resumes from the latest known position **only if playback was running when the result was applied**. Playback stopped by CEC, including a source switch or standby, stays stopped. Rescanning does not claim Active Source or wake the TV, and it does not restart X or CEC.

If the selected channel was removed, EasyTV selects the next surviving channel from the previous catalog order, wrapping around; if none survive, it selects the first new channel. If the saved file was removed, playback starts with the channel's first remaining item. Failed preparation/start candidates are skipped; if a whole channel cannot start, the rescan tries the next channel once around the catalog. These playback failures are still reported, even if a fallback starts successfully.

The request waits for the scan and application of the result. HTTP 204 means success; 409 means another rescan is already in progress. Scan errors, an entirely empty library, and catalog-save failures return 500 without replacing the live catalog or stopping playback. Playback/state-save failures also return 500 and are logged; the new catalog may already be installed, so inspect the logs rather than assuming failure means nothing changed. An accepted rescan continues if its HTTP client disconnects; application shutdown cancels and joins the worker.

Alternatively, restart the service to rescan:

```bash
sudo systemctl restart easytv.service
systemctl status easytv.service --no-pager
journalctl -u easytv.service -b --no-pager -n 100
```

The supplied service includes `--scan`, so every service start (including boot and restart) rebuilds the catalog from its configured media roots. If you removed that flag in a local service customization, restore it before using restart to rescan. There is no periodic background scan.

Restart briefly interrupts playback and restarts the X session. EasyTV saves its known playback position during orderly shutdown, then attempts to restore playback using the rebuilt catalog. If the saved channel no longer exists, it selects the first remaining channel. If the saved file no longer exists in that channel, it starts from the first catalog item; failed preparation/start candidates are skipped. With no playable channels, startup fails and reports an error.

Do not run a second EasyTV instance against the service's state directory to force a scan. The running application keeps its catalog in memory; changing the catalog file externally does not refresh it. Use `/rescan` or a service restart. Finish copying media before requesting a scan; scanning is not a filesystem snapshot and cannot prevent files from changing afterward.

## Isolated headless smoke run

After building, this example creates a tiny WAV and private state/workspace, runs for five seconds, and removes only its scratch directory. It starts real mpv, but no CEC, HTTP listener, real audio/video output, or overlay.

```bash
bash <<'EASYTV_HEADLESS'
set -euo pipefail
umask 077
work="$(mktemp -d /tmp/easytv-doc-example.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/media/Sample"
python3 - "$work/media/Sample/silence.wav" <<'PY_MEDIA'
import sys, wave
with wave.open(sys.argv[1], 'wb') as stream:
    stream.setnchannels(1)
    stream.setsampwidth(2)
    stream.setframerate(8000)
    stream.writeframes(bytes(8000 * 2 * 3))
PY_MEDIA
./bin/easytv --headless \
  --state-dir "$work/state" --temp-dir "$work/tmp" \
  --scan --run-for 5s "$work/media"
EASYTV_HEADLESS
```

## CLI options

One or more media roots are required. Both storage flags are required; the program does not automatically choose the legacy state directory.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--headless` | false | Null mpv audio/video outputs; CEC disabled unless a device is supplied. |
| `--cec-device` | empty | Explicit Linux CEC device; required for non-headless output. Supplying it in headless mode still enables physical CEC. |
| `--osd` | false | Enable owned `aosd_cat` channel previews; incompatible with headless mode. |
| `--listen` | empty | Optional HTTP bind address, for example `127.0.0.1:8080`; empty disables HTTP. |
| `--device-name` | `rpi-tv` | Name in HTTP adapter/event metadata, not the HDMI-CEC OSD identity. |
| `--scan` | false | Rebuild catalog from supplied roots. |
| `--state-dir` | empty, required | Directory containing `state.json` and `catalog.json`. |
| `--temp-dir` | empty, required | Parent of the runtime's private preparation workspace. |
| `--run-for` | zero/unbounded | Optional bounded duration, such as `30s`; negative values rejected. |

For hardware-free headless use, omit `--cec-device` entirely. Hardware mpv arguments select fullscreen output and leave audio-backend/device selection to mpv; there is no CLI audio-device selector. `--temp-dir` does not relocate every temporary file: the player also owns an IPC/log directory under `/tmp`.

## Playback and persistence

- Startup restores the saved channel if available, otherwise the first active channel, and attempts playback automatically. Readiness does not prove the TV is awake, selected, or visibly rendering.
- Successful starts persist the selected channel and actual original media path, including fallback after failed candidates. Prepared temporary image paths are not stored as media identities.
- Position observations apply a three-second audio/video resume rewind. The Go implementation retains the restored offset until the next observation rather than copying the reference's initial zero reset. Images remain at zero.
- Image advancement waits for both a ten-second timer and completed next-item preparation; preparation can extend the interval.
- Save failures are reported without stopping a successfully started player. A switch can continue after a save failure if the old child stopped. A child-stop failure blocks replacement.
- Unexpected IPC loss cancels pending work, persists known position, and stops/reaps the player. It does not manufacture EOF or automatically restart/reclaim input. A later explicit channel request can restart playback.
- Use foreground Ctrl-C or SIGTERM to the exact EasyTV instance for orderly shutdown. Application exit does not send TV standby.

Never share a writable state directory between simultaneous instances or between Go and legacy applications. Use fresh paths for acceptance and back up legacy state before migration. Invalid state/cache is reported rather than silently replaced with defaults.

### Disconnected or unavailable media

If mpv reports a media failure, EasyTV stops that player, saves the latest known position, and stays on the selected channel. With overlays enabled it shows, for example, `I Love Lucy: playback unavailable. Retrying...`; the message uses the normal five-second overlay duration and is refreshed between attempts. Without overlays it is logged as a channel-preview diagnostic.

Recovery retries the **same saved file and position**, waiting ten seconds after each failed attempt. Attempts run in a background worker, never overlap, and do not claim Active Source or change channels. Recovery does not skip to another file simply because the saved file is missing. Once that file is available and mpv loads it, playback resumes. If ordinary end-of-file advancement cannot start any candidate, the last played file/position is retained for recovery.

A channel request cancels the old recovery sequence; the newly selected channel gets its own normal startup attempt. CEC standby/source-away, explicit stop, successful live rescan, and application shutdown also cancel and join recovery. After a successful rescan while stopped, playback stays stopped: send a channel request to resume with the refreshed catalog. A failed rescan leaves the existing recovery sequence intact. There is no periodic catalog scan.

Startup now requires mpv to report `file-loaded`, not merely expose an IPC socket, before EasyTV persists a successful start. Media loading has a five-second timeout; failed attempts are reaped before another begins. Fallback to a different file during normal startup begins at offset zero, not at another file's resume position.

Filesystem disconnection behavior still depends on the OS/filesystem: a blocked kernel read, stuck network mount, or image copy may not be promptly interruptible. Configure mount/network timeouts appropriately. EasyTV cannot guarantee immediate cancellation of uninterruptible OS I/O, and does not infer a disconnection merely because playback position stops moving.

## HTTP controls

HTTP is disabled unless `--listen` is supplied. There is no built-in authentication or TLS. Use loopback for initial acceptance; do not expose unauthenticated controls to an untrusted network.

| Method | Path | Request / result |
| --- | --- | --- |
| `POST` | `/channel` | JSON `{"direction":"next"}` or `{"direction":"previous"}`. |
| `POST` | `/volume` | JSON `{"volume":50}`; finite numeric range 0 through 100. |
| `POST` | `/mute` | JSON `{"muted":true}` or `{"muted":false}`. |
| `POST` | `/rescan` | No body; refresh configured media roots and apply the result before returning 204. Concurrent rescan: 409; failures: 500. |
| `POST` | `/power` | JSON `{"state":"on"}` or `{"state":"off"}`; requires CEC. |
| `POST` | `/active-source` | No body required; explicit CEC Active Source assertion. |
| `GET` | `/adapter` | Current adapter metadata/logical and physical addresses; requires CEC. |
| `GET` | `/events` | Live received-packet SSE stream. |
| `GET` | `/events.ndjson` | Live received-packet newline-delimited JSON stream. |

Successful controls return HTTP 204. Channel success acknowledges accepted preview scheduling, not the delayed commit. With CEC enabled, every accepted channel request asserts Active Source immediately. Rapid requests move the pending selection and reset its two-second delay. Wrapping to the current channel does not restart an already-running player.

Volume/mute change the current mpv instance, not TV hardware volume, and require running playback. They are not persisted state fields; do not assume they survive player replacement.

Power and Active Source endpoints are CEC commands, not local playback start/stop endpoints. Power-on and Active Source assertion do not themselves resume stopped playback. Power-off sends broadcast STANDBY, not a TV-only directed command, and is not an atomic local playback stop. Command success is not confirmation of the TV's eventual state.

Examples against a loopback runtime you deliberately started (POSTs change playback):

```bash
curl --fail-with-body -i -H 'Content-Type: application/json' \
  -d '{"direction":"next"}' http://127.0.0.1:8080/channel
curl --fail-with-body -i -H 'Content-Type: application/json' \
  -d '{"volume":50}' http://127.0.0.1:8080/volume
curl --fail-with-body -i -H 'Content-Type: application/json' \
  -d '{"muted":false}' http://127.0.0.1:8080/mute
```

## CEC policy and receive streams

The backend owns adapter access exclusively. Outgoing commands and received-observation evaluation use current kernel addresses; read `/adapter` instead of hardcoding a logical address.

Applicable observations of another Active Source, TV-reported standby/transition to standby, or TV standby addressed to the appliance/broadcast stop playback. Malformed, unrelated, or insufficient-identity observations do not establish source loss. Routing-message policy is not implemented as a substitute. There is no repeated input-seizing loop or inferred TV-state `/status` endpoint.

SSE/NDJSON contain received packets, not fabricated power/source events or outgoing-command echoes. Fields include `id`, `event` (`cec_message_received`), `device_name`, `received_at`, `raw`, `length`, `initiator`, `destination`, `opcode`, `message_name`, and optional operands/validation errors. Receipt time is userspace time. Kernel adapter diagnostics/loss notices are logged separately.

```bash
curl --no-buffer --fail http://127.0.0.1:8080/events
# Alternative transport; choose one or run in another terminal:
curl --no-buffer --fail http://127.0.0.1:8080/events.ndjson
```

Streams are live with no replay. A nonempty `Last-Event-ID` is rejected with HTTP 409; reconnect without it for a fresh subscription. Gap events cannot be recovered. Slow subscribers are disconnected on overflow or failed/bounded writes rather than silently dropping a packet and continuing. Service-level queue overflow is a runtime failure. Kernel packet loss cannot be recovered here, and an idle stream can remain silent.

## Tests and operational limits

With real-integration opt-in variables unset:

```bash
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off \
  go test -mod=readonly ./...
```

Ordinary tests include private fake-overlay helpers; opt-in real-mpv integration suites require explicit enabling and isolated media/state. They are not live-appliance acceptance commands.

The Go entrypoint does not provide legacy evdev/FLIRC bindings, legacy shell hooks, or a REST playback status/start/stop API. Boot startup is provided separately by the installer/systemd unit. Complete target-device acceptance before replacing a live launcher.
