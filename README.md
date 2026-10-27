# EasyTV (Go)

EasyTV provides directory-based looping mpv channels for a Linux/Raspberry Pi appliance. The Go executable owns playback, persistence, optional Linux HDMI-CEC, optional HTTP controls/received-packet streams, and optional channel-preview overlays.

This README describes `cmd/easytv`. `easytv.exs` is retained as historical/reference source. The obsolete `volume.sh`, `power.sh`, and `channel.sh` hooks have been removed; Go controls mpv and CEC internally. The Go entrypoint does not install legacy evdev/FLIRC input handling.

The historical Elixir reference still contains calls to those retired hooks. It is not a runnable rollback package as checked out; use a complete backed-up deployment or restore a matching earlier revision if legacy rollback is needed.

## Validation boundary

Functional checkpoint `70ef2d5` adds opt-in owned OSD previews after the IPC-teardown and successful-start persistence fixes. Isolated validation passed with real mpv/null outputs, mock CEC, HTTP controls/streams, image timing/preparation, persistence/restart, SIGTERM, and fake-overlay process lifecycle tests.

Actual display/audio, real `aosd_cat` rendering/timing, physical CEC behavior, and unattended boot remain separate acceptance work. See [Go deployment and acceptance](docs/go-deployment.md) before changing a live installation.

## Requirements and build

- Linux and a Go toolchain compatible with the `go 1.26.5` directive in `go.mod`.
- `mpv` on PATH for playback.
- Image tools: `vipsheader` and `vipsthumbnail` on PATH. The preparer reads image dimensions with the former and downsizes oversized images with the latter. The reference Debian installation uses `libvips-tools`. Audio/video-only playback does not invoke image conversion tools.
- For `--osd`, `aosd_cat` on PATH and an existing authorized X session. EasyTV does not start X or grant display access.
- For physical CEC, an explicitly selected accessible Linux CEC device without a competing controller.

Build from the repository root:

```bash
mkdir -p "$HOME/.local/bin"
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off \
  go build -mod=readonly -o "$HOME/.local/bin/easytv" ./cmd/easytv
"$HOME/.local/bin/easytv" --help
```

Help does not start the runtime. Building does not install a service, alter login profiles, select an audio route, or configure media mounts.

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
"$HOME/.local/bin/easytv" --headless \
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

For hardware-free headless use, omit `--cec-device` entirely. Hardware mpv arguments currently select fullscreen output and ALSA audio; there is no CLI audio-device selector. `--temp-dir` does not relocate every temporary file: the player also owns an IPC/log directory under `/tmp`.

## Playback and persistence

- Startup restores the saved channel if available, otherwise the first active channel, and attempts playback automatically. Readiness does not prove the TV is awake, selected, or visibly rendering.
- Successful starts persist the selected channel and actual original media path, including fallback after failed candidates. Prepared temporary image paths are not stored as media identities.
- Position observations apply a three-second audio/video resume rewind. The Go implementation retains the restored offset until the next observation rather than copying the reference's initial zero reset. Images remain at zero.
- Image advancement waits for both a ten-second timer and completed next-item preparation; preparation can extend the interval.
- Save failures are reported without stopping a successfully started player. A switch can continue after a save failure if the old child stopped. A child-stop failure blocks replacement.
- Unexpected IPC loss cancels pending work, persists known position, and stops/reaps the player. It does not manufacture EOF or automatically restart/reclaim input. A later explicit channel request can restart playback.
- Use foreground Ctrl-C or SIGTERM to the exact EasyTV instance for orderly shutdown. Application exit does not send TV standby.

Never share a writable state directory between simultaneous instances or between Go and legacy applications. Use fresh paths for acceptance and back up legacy state before migration. Invalid state/cache is reported rather than silently replaced with defaults.

## HTTP controls

HTTP is disabled unless `--listen` is supplied. There is no built-in authentication or TLS. Use loopback for initial acceptance; do not expose unauthenticated controls to an untrusted network.

| Method | Path | Request / result |
| --- | --- | --- |
| `POST` | `/channel` | JSON `{"direction":"next"}` or `{"direction":"previous"}`. |
| `POST` | `/volume` | JSON `{"volume":50}`; finite numeric range 0 through 100. |
| `POST` | `/mute` | JSON `{"muted":true}` or `{"muted":false}`. |
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

The Go entrypoint does not provide legacy evdev/FLIRC bindings, legacy shell hooks, a REST playback status/start/stop API, or autostart installation. Complete target-device acceptance before replacing a live launcher.
