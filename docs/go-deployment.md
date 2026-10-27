# Go deployment and appliance acceptance

These are instructions to review, not automatic installation or approval of physical-device tests. Documentation validation does not execute the examples.

## Retired control scripts

`volume.sh`, `power.sh`, and `channel.sh` have been removed. Go volume/mute uses mpv IPC; TV power and Active Source use the internal CEC service; channel changes use the serialized playback owner. No replacement shell hooks are required by the Go entrypoint.

`easytv.exs` remains unmodified historical/reference source and still contains calls to the retired hooks. Do not treat that file alone as a runnable legacy rollback. Keep a complete backed-up working deployment or restore the matching historical revision, including its scripts, if rollback is required. This retirement does not update or remove any separately deployed copies outside this repository.

## Preserve the existing installation

Record the current launcher, media roots, state path, X-session environment, audio route, and adapter selection. Keep a rollback launcher and backed-up legacy state. Use separate writable state/workspace paths for Go validation, never simultaneous writers to the same state directory.

The version-1 state layout is retained, but absolute channel/media identities and resume behavior still need migration acceptance. Go intentionally preserves a restored audio/video offset until a new position observation, rather than initially resetting it to zero as the reference did. Image offsets stay zero.

## Prerequisites and environment

Build using the README. Confirm `mpv`; image preparation invokes `vipsheader` and `vipsthumbnail`, not the `vips` executable. Optional overlays require `aosd_cat` on PATH and the intended existing authorized X session.

An SSH shell may not have the console session's DISPLAY/XAUTHORITY environment. Use the actual authorized session; do not grant broad display access to bypass an error. EasyTV does not start X.

Hardware mpv arguments use ALSA. The historical PulseAudio walkthrough does not establish that this Go executable's selected route works. There is no CLI audio-device override; verify the intended route separately.

Select the actual CEC device and permissions from the appliance configuration. Metadata-only inspection can list nodes:

```bash
ls -l /dev/cec*
```

Presence alone does not establish the intended HDMI port or usable configuration. No adapter path is guessed here. Opening the Go backend acquires exclusive ownership and may clear/reconfigure logical addresses: it is not passive inspection. Do not run `cec-client`, a second EasyTV instance, or another adapter controller alongside it.

## Stage 1: isolated headless acceptance

Run the README's scratch-media example first, omitting both `--cec-device` and `--osd`. Add no listener unless needed. This starts real mpv/null outputs without physical CEC or real display/audio. Headless image playback still uses image tools and temporary files.

Check readiness, bounded shutdown, and cleanup. For persistence acceptance, keep dedicated validation state between deliberate runs rather than using the disposable smoke-test directory. Check selected channel/original filename on disk before shutdown, then check the next startup restores them. A clean exit alone does not establish resume correctness.

## Stage 2: physical display and CEC (separate approval)

The following template opens real outputs and a real CEC adapter. It can reconfigure adapter identity and, when controls are issued, affect TV/input/power state. It is not part of ordinary tests or documentation validation.

Before running it:

- Stop the existing appliance deliberately and retain rollback; do not use broad process-name kills.
- Confirm no competing adapter owner.
- Choose a small acceptance media root and separate writable validation state/workspace paths.
- Verify the actual character-device path and display authorization environment.

Export the reviewed variables before starting this nested shell; no live-path defaults are supplied.

```bash
bash <<'EASYTV_HARDWARE_ACCEPTANCE'
set -euo pipefail
: "${CEC_DEVICE:?Set the reviewed CEC device path}"
: "${MEDIA_ROOT:?Set the acceptance media root}"
: "${STATE_DIR:?Set a dedicated validation state directory}"
: "${TEMP_DIR:?Set a dedicated validation workspace parent}"
: "${DISPLAY:?Run in the intended authorized X session}"
[[ -c "$CEC_DEVICE" ]] || { printf 'Not a character device\n' >&2; exit 1; }
[[ -d "$MEDIA_ROOT" ]] || { printf 'Media root missing\n' >&2; exit 1; }
"$HOME/.local/bin/easytv" \
  --cec-device "$CEC_DEVICE" \
  --state-dir "$STATE_DIR" --temp-dir "$TEMP_DIR" \
  --listen 127.0.0.1:8080 --scan --run-for 30s \
  "$MEDIA_ROOT"
EASYTV_HARDWARE_ACCEPTANCE
```

Readiness does not itself power on the TV or claim Active Source. Explicit controls have their own effects; wake/select/play is not one atomic operation. If the loopback port is occupied, identify its owner or choose another explicit loopback port, not a public bind. Point clients at the address of the runtime you intentionally started.

## Stage 3: physical OSD (separate approval)

After display/CEC acceptance, repeat the bounded hardware invocation with `--osd` added. It cannot be combined with headless. The CLI still requires a CEC device for non-headless output even if the immediate goal is checking an overlay.

Issue an intentional channel request and check:

- Channel name, position, padding, font/colors, and intended five-second full-display interval.
- Prompt replacement on rapid requests, without stale overlapping overlays.
- Clearing and child cleanup on application shutdown and accepted source-away/standby observations.
- Cancellation of pending playback restart after source loss.
- Visible diagnostics for overlay failures without crashing channel playback.

Fake helpers validate lifecycle/arguments, not X rendering, available fonts, visible timing, or the real display stack. OSD is opt-in; without it, previews are diagnostic text only.

## CEC and HTTP acceptance details

Read logical/physical addresses from `/adapter`, not hardcoded numbers. `--device-name` labels HTTP metadata, not HDMI-CEC identity.

- Each accepted channel request asserts Active Source with CEC enabled, before delayed commit. HTTP success does not prove the eventual TV/input state.
- Other Active Source and applicable TV standby observations stop playback. There is no loop that automatically fights another input.
- Absence of an observation is not proof the TV is on or this input is selected; a TV is not guaranteed to emit every useful packet.
- Power-off transmits broadcast STANDBY. Review effects on the attached HDMI setup before sending it. `/power` requests transmission, not an atomic local playback stop; local stop policy reacts to accepted received observations.
- Power-on or Active Source assertion does not itself resume a stopped player. An intentional channel request can restart playback.
- Volume/mute affect the current mpv instance, not TV hardware volume, and are not persisted across player replacement.
- SSE/NDJSON contain received packets only, with no replay across reconnect gaps. Outgoing commands and adapter diagnostics are not synthetic receive events.
- There is no inferred-TV-state `/status` or public REST playback start/stop endpoint.

HTTP has no built-in authentication/TLS. Keep acceptance local/trusted. Any remote-access arrangement needs separately reviewed protection; a bind address is not authentication.

## State and orderly shutdown

STATE_DIR contains `catalog.json` and `state.json`. Media may be read-only; state/workspace must be writable. Do not use the media directory as an application-owned cleanup location.

Explicit scan rebuilds the catalog from all roots supplied to that run. Cached roots are reused without automatic refresh otherwise. Root/path changes can change identities.

Successful starts save actual original media paths, including fallback candidates. Position observations apply the three-second audio/video rewind; images remain at zero. Save failures can leave disk state older than playback; inspect diagnostics and saved JSON rather than assuming a running player proves storage succeeded.

Unexpected IPC loss stops playback and cancels pending work without fabricating EOF or automatically reclaiming input. Distinguish it from ordinary advancement.

Use foreground Ctrl-C or SIGTERM to the exact instance. Workers and owned children are joined and private workspaces removed. The player also owns a separate `/tmp` IPC/log directory. Broad `pkill mpv`/`pkill aosd_cat` is not an orderly-cleanup acceptance test. Application exit does not send TV standby.

## Unattended launch comes last

Do not change `.xinitrc`, login profiles, or service definitions until hardware acceptance is complete. Go does not install/update those files.

After acceptance, adapt the existing authorized X-session launcher to the built Go binary, reviewed absolute media roots, explicit adapter, dedicated persistent state/workspace paths, optional `--osd`, and optional loopback HTTP. Remove the short runtime bound only when deliberately moving to unattended operation. Store/export required environment in the launcher rather than relying on a transient SSH shell; ensure media mounts are available first.

Retain a rollback launcher/state backup and review logging/restart behavior separately. EasyTV prints readiness/listener information to stdout and diagnostics to stderr; it does not configure journald or a restart policy.

Legacy evdev/FLIRC hotkeys and shell-script hooks are not provided by this entrypoint. Remote integration must explicitly use the implemented API or be developed separately. Do not claim the legacy keyboard table applies to Go.

Before marking deployment complete, record actual paths/device/environment, physical CEC observations, real OSD replacement/timing, audio/video output, persistence/restart, and shutdown/child cleanup results, including TV-specific limitations.
