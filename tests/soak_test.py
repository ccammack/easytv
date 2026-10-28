import contextlib
import argparse
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import sys
import unittest
from unittest.mock import patch


sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location(
    "soak", Path(__file__).resolve().parents[1] / "scripts" / "soak.py")
soak = importlib.util.module_from_spec(spec)
spec.loader.exec_module(soak)


class SoakTests(unittest.TestCase):
    def test_duration_validation(self):
        self.assertEqual(soak.duration("2m"), 120)
        for value in ("0", "-1", "nan", "inf", "8d", ""):
            with self.assertRaises(argparse.ArgumentTypeError):
                soak.duration(value)

    def appliance(self, playback="playing", changed=False):
        with tempfile.TemporaryDirectory() as folder:
            output = Path(folder) / "report"
            value = dict(channel_id="/Channel One", file_path="/media/file",
                         playback=playback, position_seconds=0,
                         pending_channel_id="", last_error="", next_retry_at=None)
            service = dict(NRestarts="0", ActiveState="active", MainPID="123")
            later = dict(service, NRestarts="1") if changed else service
            with patch("sys.argv", ["soak.py", "appliance", "--duration", "0.01s",
                                    "--output", str(output)]), \
                 patch.object(soak, "status", return_value=value), \
                 patch.object(soak, "request") as request, \
                 patch.object(soak, "service_snapshot", side_effect=[service, later]), \
                 patch.object(soak.subprocess, "run"), \
                 patch.object(soak.pwd, "getpwnam") as user, \
                 patch.object(soak.signal, "signal"), \
                 contextlib.redirect_stdout(io.StringIO()):
                user.return_value.pw_uid = 997
                code = soak.main()
            summary = json.loads((output / "summary.json").read_text())
            return code, summary, request.call_args_list

    def test_appliance_changes_channels_and_saves_report(self):
        code, summary, calls = self.appliance()
        self.assertEqual(code, 0)
        self.assertEqual(summary["result"], "PASS")
        self.assertEqual(calls[0].args[1:], ("/channel", {"direction": "next"}))

    def test_stopped_playback_fails(self):
        code, summary, _ = self.appliance(playback="stopped")
        self.assertEqual(code, 1)
        self.assertIn("non-playing", summary["error"])

    def test_service_restart_fails(self):
        code, summary, _ = self.appliance(changed=True)
        self.assertEqual(code, 1)
        self.assertIn("restarted", summary["error"])


if __name__ == "__main__":
    unittest.main()
