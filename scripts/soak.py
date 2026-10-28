#!/usr/bin/env python3
"""Bounded player and appliance soak runners; standard library only."""

import argparse
import contextlib
import datetime
import json
import os
import pwd
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import wave
import math


ROOT = Path(__file__).resolve().parent.parent


def duration(text):
    units = {"s": 1, "m": 60, "h": 3600}
    try:
        value = float(text[:-1]) * units[text[-1]] if text[-1] in units else float(text)
        if not math.isfinite(value) or not 0 < value <= 7 * 86400:
            raise ValueError()
        return value
    except (ValueError, KeyError, IndexError):
        raise argparse.ArgumentTypeError("use a positive duration up to 7d, e.g. 30s, 10m, 8h")


def request(base, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=8) as response:
        if body is not None:
            if response.status != 204:
                raise RuntimeError(f"{path}: expected 204, got {response.status}")
            return None
        if response.status != 200:
            raise RuntimeError(f"{path}: expected 200, got {response.status}")
        return json.load(response)


def status(base):
    value = request(base, "/status")
    required = {"channel_id", "file_path", "playback", "position_seconds",
                "pending_channel_id", "last_error", "next_retry_at"}
    if not isinstance(value, dict) or set(value) != required:
        raise RuntimeError(f"unexpected status shape: {value!r}")
    if value["playback"] not in ("playing", "stopped", "retrying"):
        raise RuntimeError(f"unexpected playback state: {value!r}")
    return value


def wait_status(base, predicate, process, timeout=15):
    deadline = time.monotonic() + timeout
    while True:
        if process.poll() is not None:
            raise RuntimeError(f"isolated player exited with {process.returncode}")
        value = status(base)
        if predicate(value):
            return value
        if time.monotonic() >= deadline:
            raise RuntimeError(f"status deadline exceeded: {value!r}")
        time.sleep(0.1)


def run(command, **kwargs):
    process = subprocess.Popen(command, start_new_session=True, **kwargs)
    try:
        code = process.wait(timeout=60)
        if code:
            raise subprocess.CalledProcessError(code, command)
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)


def fixtures(root):
    for channel in ("Channel One", "Channel Two"):
        folder = root / channel
        folder.mkdir(parents=True)
        with wave.open(str(folder / "01.wav"), "wb") as stream:
            stream.setnchannels(1)
            stream.setsampwidth(2)
            stream.setframerate(8000)
            stream.writeframes(bytes(8000 * 2 * 2))
        run(["ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i",
             "color=c=blue:s=160x90:r=10", "-t", "2", "-c:v", "mpeg4",
             str(folder / "02.mp4")])
        (folder / "03.ppm").write_bytes(b"P6\n16 16\n255\n" + bytes([0, 128, 0]) * 256)
        (folder / "04.mp4").write_bytes(b"deliberately invalid media\n")


def resources(pid):
    proc = Path("/proc") / str(pid)
    values = {}
    for line in (proc / "status").read_text().splitlines():
        if line.startswith("VmRSS:"):
            values["rss_kib"] = int(line.split()[1])
    values["fds"] = len(list((proc / "fd").iterdir()))
    values["children"] = (proc / "task" / str(pid) / "children").read_text().split()
    return values


def service_snapshot(service):
    result = subprocess.run(
        ["systemctl", "show", service, "--property=NRestarts,ActiveState,MainPID"],
        capture_output=True, text=True, timeout=10, check=True)
    return dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["player", "appliance"])
    parser.add_argument("--duration", type=duration, default=3600,
                        help="soak duration, default 1h; setup/scenarios/cleanup add time")
    parser.add_argument("--interval", type=duration, default=45,
                        help="normal channel dwell, default 45s (alternates 1x/2x)")
    parser.add_argument("--url", default="http://127.0.0.1:8080",
                        help="appliance only: running service; changes real channels")
    parser.add_argument("--service", default="easytv.service",
                        help="appliance only: local systemd unit to monitor")
    parser.add_argument("--binary", type=Path, default=ROOT / "bin/easytv")
    parser.add_argument("--go", default=os.environ.get("GO", "go"),
                        help="player only: Go executable for focused integration scenarios")
    parser.add_argument("--output", type=Path,
                        help="new report directory; defaults to a private directory in /tmp")
    args = parser.parse_args()
    output = args.output
    if output is None:
        output = Path(tempfile.mkdtemp(prefix="easytv-soak-report-"))
    else:
        output.mkdir(mode=0o700, parents=True, exist_ok=False)
    output = output.resolve()
    print(f"Mode={args.mode} duration={args.duration}s report={output}", flush=True)
    print("Appliance mode changes channels and may select the Pi's HDMI input."
          if args.mode == "appliance" else "Isolated fixtures; no installed service or CEC access.",
          flush=True)
    actions = 0
    samples = 0
    process = None
    result = {"result": "FAIL", "mode": args.mode, "started_at": datetime.datetime.now(
        datetime.timezone.utc).isoformat()}
    started = time.monotonic()
    baseline = None
    initial_service = None
    base = args.url.rstrip("/")

    def record(event, **fields):
        with (output / "events.jsonl").open("a") as log:
            log.write(json.dumps({"elapsed": round(time.monotonic() - started, 3),
                                  "event": event, **fields}) + "\n")

    def interrupted(signum, frame):
        raise KeyboardInterrupt()

    signal.signal(signal.SIGTERM, interrupted)
    try:
        with contextlib.ExitStack() as stack:
            if args.mode == "player":
                for executable in ("mpv", "ffmpeg", "vipsheader", "vipsthumbnail"):
                    if shutil.which(executable) is None:
                        raise RuntimeError(f"missing prerequisite: {executable}")
                if not args.binary.is_file():
                    raise RuntimeError("build bin/easytv first with make build")
                with (output / "scenarios.log").open("w") as scenario_log:
                    environment = dict(os.environ, EASYTV_TEST_REAL_MPV="1",
                                       GOTOOLCHAIN="local", GOPROXY="off",
                                       GOSUMDB="off", GOWORK="off")
                    run([args.go, "test", "-mod=readonly", "-count=1", "-timeout=50s",
                         "-run", "TestReal(MediaRecovery|MissingMedia|CECService|HTTPRescan|Rescan)",
                         "./internal/app"], cwd=ROOT, env=environment,
                        stdout=scenario_log, stderr=subprocess.STDOUT)
                record("focused-recovery-rescan-mock-cec-scenarios-passed")
                work = Path(stack.enter_context(tempfile.TemporaryDirectory(prefix="easytv-soak-")))
                media = work / "media"
                fixtures(media)
                log = stack.enter_context((output / "player.log").open("w"))
                process = subprocess.Popen(
                    [str(args.binary.resolve()), "--headless", "--scan",
                     "--state-dir", str(work / "state"), "--temp-dir", str(work / "temp"),
                     "--listen", "127.0.0.1:0", str(media)],
                    stdout=log, stderr=log)
                # Ensure process shutdown precedes workspace removal, even on failure.
                def stop():
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=20)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait(timeout=5)
                            raise RuntimeError("isolated shutdown exceeded 20 seconds")
                    if process.returncode != 0:
                        raise RuntimeError(f"isolated exit={process.returncode}; see player.log")
                stack.callback(stop)
                deadline = time.monotonic() + 30
                while True:
                    lines = (output / "player.log").read_text().splitlines()
                    address = next((line.split("HTTP listening=", 1)[1].split()[0]
                                    for line in lines if "HTTP listening=" in line), None)
                    if address:
                        base = "http://" + address
                        break
                    if process.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError("isolated startup failed; see player.log")
                    time.sleep(0.1)
                # Exercise normal startup fallback with a missing saved file.
                current = status(base)
                target = Path(current["file_path"])
                request(base, "/channel", {"direction": "next"})
                wait_status(base, lambda s: s["channel_id"] != current["channel_id"], process)
                backup = target.with_suffix(target.suffix + ".offline")
                target.rename(backup)
                request(base, "/channel", {"direction": "previous"})
                # Other candidates may play normally; exact-file retry behavior is
                # covered by the focused Go recovery tests, not inferred here.
                wait_status(base, lambda s: s["channel_id"] == current["channel_id"], process)
                backup.rename(target)
                request(base, "/rescan", {})
                record("missing-file-and-rescan-completed")
            else:
                initial_service = service_snapshot(args.service)
                if initial_service.get("ActiveState") != "active":
                    raise RuntimeError(f"service not active: {initial_service}")
            print(f"Target={base}", flush=True)
            initial_error = status(base)["last_error"]
            deadline = time.monotonic() + args.duration
            next_action = time.monotonic()
            last_file = None
            files_seen = set()
            pending_since = None
            while time.monotonic() < deadline:
                value = status(base)
                samples += 1
                files_seen.add(value["file_path"])
                sample = {"status": value}
                if process is not None:
                    if process.poll() is not None:
                        raise RuntimeError(f"player exited: {process.returncode}")
                    sample["resources"] = resources(process.pid)
                    temp_count = sum(1 for _ in (work / "temp").rglob("*"))
                    sample["temp_entries"] = temp_count
                    if baseline is None and samples >= 3:
                        baseline = sample["resources"]
                    if baseline:
                        r = sample["resources"]
                        if r["fds"] > baseline["fds"] + 32 or r["rss_kib"] > baseline["rss_kib"] + 131072:
                            raise RuntimeError(f"resource growth exceeded allowance: {r}")
                    if len(sample["resources"]["children"]) > 4 or temp_count > 100:
                        raise RuntimeError(f"process/temp growth exceeded allowance: {sample}")
                else:
                    observed = service_snapshot(args.service)
                    sample["service"] = observed
                    if observed != initial_service:
                        raise RuntimeError(f"service changed/restarted: {observed}")
                record("sample", **sample)
                if value["pending_channel_id"]:
                    if pending_since is None:
                        pending_since = time.monotonic()
                    elif time.monotonic() - pending_since > 15:
                        raise RuntimeError(f"channel preview did not commit: {value}")
                else:
                    pending_since = None
                if value["playback"] != "playing":
                    raise RuntimeError(f"unexpected non-playing state: {value}")
                if args.mode == "appliance" and value["last_error"] != initial_error:
                    raise RuntimeError(f"new playback error: {value['last_error']}")
                if value["file_path"] != last_file:
                    record("file-observed", path=value["file_path"])
                    last_file = value["file_path"]
                if time.monotonic() >= next_action:
                    direction = "previous" if actions % 3 == 2 else "next"
                    burst = 4 if actions % 6 == 5 else 1
                    for _ in range(burst):
                        request(base, "/channel", {"direction": direction})
                        record("channel", direction=direction)
                        time.sleep(0.25)
                    actions += 1
                    dwell = 600 if actions % 10 == 0 else args.interval * (1 + actions % 2)
                    next_action = time.monotonic() + dwell
                time.sleep(min(2, max(0, deadline - time.monotonic())))
            result.update(result="PASS", unique_files_observed=len(files_seen))
    except KeyboardInterrupt:
        result.update(result="INTERRUPTED", error="interrupted by user or signal")
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError,
            urllib.error.URLError) as exc:
        result.update(result="FAIL", error=str(exc))
    finally:
        if args.mode == "appliance":
            with (output / "journal.log").open("w") as log:
                try:
                    subprocess.run(["journalctl", "-b", "--since", result["started_at"],
                                    "_UID=" + str(pwd.getpwnam("easytv").pw_uid),
                                    "--no-pager", "-n", "2000"], stdout=log,
                                   stderr=subprocess.STDOUT, timeout=15, check=True)
                except (KeyError, OSError, subprocess.SubprocessError) as exc:
                    result["journal_warning"] = str(exc)
        result.update(actions=actions, samples=samples,
                      elapsed_seconds=round(time.monotonic() - started, 3))
        (output / "summary.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps(result, indent=2), flush=True)
    return 0 if result["result"] == "PASS" else 130 if result["result"] == "INTERRUPTED" else 1


if __name__ == "__main__":
    sys.exit(main())
