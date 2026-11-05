#!/usr/bin/env python3
"""Exercise the installed EasyTV service through its existing evdev input."""

import argparse
import datetime
import fcntl
import glob
import json
import math
import os
from pathlib import Path
import pwd
import random
import re
import signal
import stat
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


EVENT = struct.Struct("@llHHi")
PAGEUP, PAGEDOWN = 104, 109
STOPPING = False


def duration(value):
    match = re.fullmatch(r"(\d+(?:\.\d+)?)([smhd]?)", value)
    if not match:
        raise argparse.ArgumentTypeError("use a positive duration such as 30s, 10m, 8h, or 2d")
    seconds = float(match[1]) * {"": 1, "s": 1, "m": 60, "h": 3600, "d": 86400}[match[2]]
    if not math.isfinite(seconds) or seconds <= 0:
        raise argparse.ArgumentTypeError("duration must be finite and positive")
    return seconds


def command(*args):
    result = subprocess.run(args, capture_output=True, text=True, timeout=15)
    if result.returncode:
        raise RuntimeError(f"{' '.join(args)} failed: {result.stderr.strip()}")
    return result.stdout


def service_state():
    output = command(
        "systemctl", "show", "easytv.service",
        "--property=ActiveState,SubState,InvocationID,NRestarts",
    )
    return dict(line.split("=", 1) for line in output.splitlines())


def processes(uid):
    result = []
    for directory in Path("/proc").iterdir():
        if not directory.name.isdigit():
            continue
        try:
            if directory.stat().st_uid != uid:
                continue
            name = (directory / "comm").read_text().strip()
            args = (directory / "cmdline").read_bytes().split(b"\0")
            status = (directory / "status").read_text()
            process_stat = (directory / "stat").read_text().rsplit(")", 1)[1].split()
            if process_stat[0] == "Z":
                continue
            # Field 22 is starttime; the split begins at field 3.
            start = process_stat[19]
            rss = re.search(r"^VmRSS:\s+(\d+)", status, re.MULTILINE)
            result.append({
                "pid": int(directory.name), "name": name, "start": start,
                "args": [os.fsdecode(arg) for arg in args if arg],
                "rss_kib": int(rss[1]) if rss else 0,
                "threads": len(list((directory / "task").iterdir())),
                "fds": len(list((directory / "fd").iterdir())),
            })
        except (FileNotFoundError, ProcessLookupError):
            continue
    return result


def request_mute(url, destination):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "http" or parsed.hostname not in ("localhost", "127.0.0.1", "::1"):
        raise RuntimeError("--cec-url must be a local HTTP daemon URL")
    if parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ("", "/"):
        raise RuntimeError("--cec-url must be a base URL without credentials, path, query, or fragment")
    body = json.dumps({
        "command": "remote_key", "destination": destination, "control": "MUTE_FUNCTION",
    }).encode()
    request = urllib.request.Request(
        url.rstrip("/") + "/commands", body, {"Content-Type": "application/json"},
    )
    # No retry: even a failed/uncertain request may have reached the TV.
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            if response.status != 204:
                raise RuntimeError(f"mute returned HTTP {response.status}")
    except urllib.error.HTTPError as error:
        detail = error.read(2048).decode(errors="replace")
        raise RuntimeError(f"mute returned HTTP {error.code}: {detail}; not retrying") from error


class Soak:
    def __init__(self, args, report):
        self.args = args
        self.report = report
        self.random = random.Random(args.seed)
        self.uid = pwd.getpwnam("easytv").pw_uid
        self.baseline = service_state()
        self.vm = None
        self.cursor = None
        self.sent = 0
        self.received = 0
        self.pending_since = None
        self.missing_player_since = None
        self.next_check = 0
        self.started = time.monotonic()
        self.deadline = self.started + args.duration
        self.fd = None

    def log(self, event, **fields):
        record = {
            "time": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "elapsed_s": round(time.monotonic() - self.started, 2),
            "event": event, **fields,
        }
        self.report.write(json.dumps(record) + "\n")
        self.report.flush()
        if event != "health":
            print(json.dumps(record), flush=True)

    def prepare(self):
        if self.baseline["ActiveState"] != "active" or self.baseline["SubState"] != "running":
            raise RuntimeError("easytv.service must already be running")
        device = self.args.device
        if device is None:
            matches = glob.glob("/dev/input/by-id/usb-flirc.tv_*-event-kbd")
            if len(matches) != 1:
                raise RuntimeError("expected exactly one FLIRC keyboard; specify --device explicitly")
            device = matches[0]
        device = Path(device).resolve(strict=True)
        if device.parent != Path("/dev/input") or not re.fullmatch(r"event\d+", device.name):
            raise RuntimeError("--device must resolve to /dev/input/eventN")
        if not stat.S_ISCHR(device.stat().st_mode):
            raise RuntimeError("--device is not an input character device")
        running = processes(self.uid)
        readers = [p for p in running if p["name"] == "input_event"]
        if not any(
            len(p["args"]) >= 2 and Path(p["args"][1]).resolve() == device for p in readers
        ):
            raise RuntimeError("EasyTV is not reading this device; check permissions and startup discovery")
        self.fd = os.open(device, os.O_WRONLY | os.O_NONBLOCK)
        # EVIOCGKEY: refuse to interfere with a physically held channel key.
        bitmap = bytearray(96)
        fcntl.ioctl(self.fd, (2 << 30) | (len(bitmap) << 16) | (ord("E") << 8) | 0x18, bitmap)
        if any(bitmap[key // 8] & (1 << (key % 8)) for key in (PAGEUP, PAGEDOWN)):
            raise RuntimeError("release physical PageUp/PageDown keys before starting")
        entries = command(
            "journalctl", f"_UID={self.uid}", "-n", "1", "--output=json", "--no-pager",
        ).splitlines()
        if not entries:
            raise RuntimeError("no service-user journal cursor available")
        self.cursor = json.loads(entries[-1])["__CURSOR"]
        self.log("start", device=str(device), seed=self.args.seed,
                 duration_s=self.args.duration, baseline=self.baseline)
        self.health()
        if not self.args.skip_mute:
            request_mute(self.args.cec_url, self.args.mute_destination)
            self.log("mute_requested", destination=self.args.mute_destination,
                     note="transmission accepted; verify TV actually muted; no restore or retry")

    def journal(self):
        output = command(
            "journalctl", f"_UID={self.uid}", f"--after-cursor={self.cursor}",
            "--output=json", "--no-pager",
        )
        for line in output.splitlines():
            entry = json.loads(line)
            self.cursor = entry["__CURSOR"]
            message = entry.get("MESSAGE", "")
            if re.fullmatch(
                r"(?:\[info\] )?(?:Next channel|Previous channel|Return to current channel)", message
            ):
                self.received += 1
            priority = int(entry.get("PRIORITY", 6))
            level = re.match(r"^\[(error|warning|critical|alert|emergency)\]", message)
            if level:
                priority = min(priority, {"error": 3, "warning": 4, "critical": 2,
                                          "alert": 1, "emergency": 0}[level[1]])
            if priority <= 4:
                self.log("journal", priority=priority, message=message)
                # AS bursts can legitimately hit cecmond's admission rate limit.
                rate_limit = "CEC Active Source failed:" in message and "429" in message
                if level and priority <= 3 and not rate_limit:
                    raise RuntimeError(f"EasyTV logged an error: {message}")
        if self.received >= self.sent:
            self.pending_since = None

    def health(self):
        state = service_state()
        if state != self.baseline:
            raise RuntimeError(f"service stopped or restarted: baseline={self.baseline}, now={state}")
        running = processes(self.uid)
        vms = [p for p in running if p["name"] == "beam.smp"]
        if len(vms) != 1:
            raise RuntimeError(f"expected one EasyTV VM, found {len(vms)}")
        identity = (vms[0]["pid"], vms[0]["start"])
        if self.vm is not None and identity != self.vm:
            raise RuntimeError(f"playback VM changed from {self.vm} to {identity}")
        self.vm = identity
        self.journal()
        now = time.monotonic()
        if self.pending_since is not None and now - self.pending_since > 15:
            raise RuntimeError(f"keys are not reaching sample config: sent={self.sent}, received={self.received}")
        players = [p for p in running if p["name"] == "mpv"]
        if players:
            self.missing_player_since = None
        elif self.missing_player_since is None:
            self.missing_player_since = now
        elif now - self.missing_player_since > 20:
            raise RuntimeError("no mpv for over 20 seconds; playback may have stalled or TV focus was lost")
        vm_metrics = {key: value for key, value in vms[0].items() if key != "args"}
        self.log("health", vm=vm_metrics, mpv_count=len(players),
                 osd_count=sum(p["name"] == "aosd_cat" for p in running),
                 sent=self.sent, received=self.received)
        self.next_check = now + 5

    def wait(self, seconds):
        end = min(time.monotonic() + seconds, self.deadline)
        while not STOPPING and time.monotonic() < end:
            if time.monotonic() >= self.next_check:
                self.health()
            time.sleep(min(0.1, max(0, end - time.monotonic())))

    def event(self, event_type, code, value):
        packet = EVENT.pack(0, 0, event_type, code, value) + EVENT.pack(0, 0, 0, 0, 0)
        if os.write(self.fd, packet) != len(packet):
            raise RuntimeError("short input-event write")

    def key(self, code, hold=0.03, repeats=0):
        self.event(1, code, 1)
        self.sent += 1
        if self.pending_since is None:
            self.pending_since = time.monotonic()
        try:
            for _ in range(repeats):
                self.wait(0.08)
                if STOPPING or time.monotonic() >= self.deadline:
                    break
                self.event(1, code, 2)
            self.wait(hold)
        finally:
            self.event(1, code, 0)

    def run(self):
        cycle = 0
        while not STOPPING and time.monotonic() < self.deadline:
            cycle += 1
            self.log("phase", phase="steady", cycle=cycle, seconds=self.args.steady)
            self.wait(self.args.steady)
            if STOPPING or time.monotonic() >= self.deadline:
                break
            self.log("phase", phase="changing", cycle=cycle, seconds=self.args.changing)
            end = min(time.monotonic() + self.args.changing, self.deadline)
            while not STOPPING and time.monotonic() < end:
                mode = self.random.choice(("burst", "alternate", "hold", "slow"))
                code = self.random.choice((PAGEUP, PAGEDOWN))
                count = self.random.randint(1, 16) if mode in ("burst", "alternate") else 1
                self.log("keys", mode=mode, count=count, key=code)
                for index in range(count):
                    if STOPPING or time.monotonic() >= end:
                        break
                    key = (PAGEUP if index % 2 else PAGEDOWN) if mode == "alternate" else code
                    self.key(key, repeats=8 if mode == "hold" else 0)
                    self.wait(self.random.uniform(0.02, 0.2) if mode != "slow" else 2.05)
                self.wait(self.random.choice((0.05, 0.3, 1.9, 2.05, 3, 8)))
            self.wait(4)
        if not STOPPING:
            # Check outstanding key delivery even when duration ended during a burst.
            self.deadline = time.monotonic() + 16
            self.wait(5)
            self.health()
            if self.received < self.sent:
                raise RuntimeError(f"undelivered keys at finish: sent={self.sent}, received={self.received}")
        self.log("interrupted" if STOPPING else "pass", sent=self.sent, received=self.received)


def stop(_signal, _frame):
    global STOPPING
    STOPPING = True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--duration", type=duration, default=duration("24h"), help="e.g. 30m, 8h, 2d (default 24h)")
    parser.add_argument("--steady", type=duration, default=duration("30m"), help="steady phase length (default 30m)")
    parser.add_argument("--changing", type=duration, default=duration("10m"), help="changing phase length (default 10m)")
    parser.add_argument("--device", help="existing input device; defaults to the single FLIRC keyboard")
    parser.add_argument("--cec-url", default="http://127.0.0.1:8080")
    parser.add_argument("--mute-destination", type=int, choices=range(15), default=0, metavar="0-14")
    parser.add_argument("--skip-mute", action="store_true")
    parser.add_argument("--seed", type=int, default=random.SystemRandom().randrange(2**32))
    parser.add_argument("--report", type=Path, help="new JSONL report path (default .easytv/soak-TIMESTAMP.jsonl)")
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error("run with sudo; injecting evdev events requires root")
    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    report_path = args.report or Path(".easytv") / (
        "soak-" + datetime.datetime.now().strftime("%Y%m%d-%H%M%S") + ".jsonl"
    )
    soak = None
    try:
        report_path.parent.mkdir(parents=True, exist_ok=True)
        with open("/run/lock/easytv-deploy.lock", "a") as lock, report_path.open("x") as report:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            soak = Soak(args, report)
            try:
                soak.prepare()
                soak.run()
                return 130 if STOPPING else 0
            except (OSError, RuntimeError, subprocess.SubprocessError, ValueError) as error:
                soak.log("fail", error=str(error))
                return 1
            finally:
                if soak.fd is not None:
                    os.close(soak.fd)
    except (OSError, KeyError) as error:
        print(f"soak failed: {error}", file=sys.stderr)
        return 1
    finally:
        print(f"Report: {report_path.resolve()}", flush=True)


if __name__ == "__main__":
    sys.exit(main())
