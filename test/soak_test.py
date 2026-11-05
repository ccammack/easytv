import importlib.util
import io
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "soak", Path(__file__).resolve().parents[1] / "scripts" / "soak.py"
)
soak = importlib.util.module_from_spec(spec)
spec.loader.exec_module(soak)


class SoakTest(unittest.TestCase):
    def make_soak(self):
        args = SimpleNamespace(seed=42, duration=40, steady=10, changing=10)
        baseline = {"ActiveState": "active", "SubState": "running",
                    "InvocationID": "first", "NRestarts": "0"}
        with patch.object(soak, "service_state", return_value=baseline):
            runner = soak.Soak(args, io.StringIO())
        runner.cursor = "before"
        return runner

    def test_duration_units_and_invalid_values(self):
        self.assertEqual(soak.duration("1.5h"), 5400)
        self.assertEqual(soak.duration("2d"), 172800)
        self.assertEqual(soak.duration("30"), 30)
        for value in ("0", "-1h", "inf", "abc", "2weeks"):
            with self.assertRaises(soak.argparse.ArgumentTypeError):
                soak.duration(value)

    def test_press_repeat_and_release_packets(self):
        runner = self.make_soak()
        runner.fd = 7
        packets = []
        with patch.object(soak.os, "write", side_effect=lambda _, data: packets.append(data) or len(data)), \
                patch.object(runner, "wait"):
            runner.key(soak.PAGEDOWN, repeats=2)
        decoded = [soak.EVENT.unpack(packet[:soak.EVENT.size])[2:] for packet in packets]
        self.assertEqual(decoded, [(1, 109, 1), (1, 109, 2), (1, 109, 2), (1, 109, 0)])
        for packet in packets:
            self.assertEqual(soak.EVENT.unpack(packet[soak.EVENT.size:])[2:], (0, 0, 0))
        self.assertEqual(runner.sent, 1)

    def test_key_is_released_when_wait_fails(self):
        runner = self.make_soak()
        with patch.object(runner, "event") as event, \
                patch.object(runner, "wait", side_effect=RuntimeError("health failed")):
            with self.assertRaisesRegex(RuntimeError, "health failed"):
                runner.key(soak.PAGEUP)
        self.assertEqual(event.call_args_list[-1].args, (1, soak.PAGEUP, 0))

    def test_running_service_restart_is_a_failure(self):
        runner = self.make_soak()
        changed = dict(runner.baseline, InvocationID="second")
        with patch.object(soak, "service_state", return_value=changed):
            with self.assertRaisesRegex(RuntimeError, "stopped or restarted"):
                runner.health()

    def test_missing_player_and_missing_keys_are_failures(self):
        runner = self.make_soak()
        vm = {"pid": 100, "start": "1000", "name": "beam.smp"}
        runner.missing_player_since = 0
        with patch.object(soak, "service_state", return_value=runner.baseline), \
                patch.object(soak, "processes", return_value=[vm]), \
                patch.object(runner, "journal"), patch.object(soak.time, "monotonic", return_value=21):
            with self.assertRaisesRegex(RuntimeError, "no mpv"):
                runner.health()
            runner.pending_since = 0
            with self.assertRaisesRegex(RuntimeError, "keys are not reaching"):
                runner.health()

    def test_journal_counts_formatted_keys_and_tolerates_only_as_rate_limits(self):
        runner = self.make_soak()
        entries = [
            {"__CURSOR": "a", "MESSAGE": "[info] Next channel", "PRIORITY": "6"},
            {"__CURSOR": "b", "MESSAGE": "Previous channel", "PRIORITY": "6"},
            {"__CURSOR": "c", "MESSAGE": "[error] CEC Active Source failed: 429", "PRIORITY": "6"},
            {"__CURSOR": "d", "MESSAGE": "pw.conf: can't load config client.conf", "PRIORITY": "3"},
        ]
        with patch.object(soak, "command", return_value="\n".join(map(json.dumps, entries))), \
                patch.object(runner, "log"):
            runner.journal()
        self.assertEqual(runner.received, 2)
        self.assertEqual(runner.cursor, "d")
        entries = [{"__CURSOR": "d", "MESSAGE": "[error] player IPC failed", "PRIORITY": "6"}]
        with patch.object(soak, "command", return_value=json.dumps(entries[0])), \
                patch.object(runner, "log"):
            with self.assertRaisesRegex(RuntimeError, "player IPC failed"):
                runner.journal()

    def test_mute_is_set_function_not_toggle_and_is_not_retried(self):
        with patch.object(soak.urllib.request, "urlopen") as request:
            request.return_value.__enter__.return_value.status = 204
            soak.request_mute("http://127.0.0.1:8080", 0)
            payload = json.loads(request.call_args.args[0].data)
            self.assertEqual(payload, {"command": "remote_key", "destination": 0,
                                       "control": "MUTE_FUNCTION"})
            self.assertEqual(request.call_count, 1)
            request.side_effect = soak.urllib.error.URLError("uncertain")
            with self.assertRaises(soak.urllib.error.URLError):
                soak.request_mute("http://127.0.0.1:8080", 0)
            self.assertEqual(request.call_count, 2)
        with self.assertRaisesRegex(RuntimeError, "local HTTP"):
            soak.request_mute("http://example.com", 0)

    def test_run_alternates_steady_and_changing_without_keys_in_steady(self):
        clock = [0]
        with patch.object(soak.time, "monotonic", side_effect=lambda: clock[0]):
            runner = self.make_soak()
            events = []

            def wait(seconds):
                clock[0] = min(clock[0] + seconds, runner.deadline)

            def key(*args, **kwargs):
                events.append(("key", None))
                runner.sent += 1
                runner.received += 1

            with patch.object(runner, "wait", side_effect=wait), \
                    patch.object(runner, "key", side_effect=key), \
                    patch.object(runner, "health"), \
                    patch.object(runner, "log", side_effect=lambda event, **fields: events.append((event, fields))):
                runner.run()
        phase = None
        for event, fields in events:
            if event == "phase":
                phase = fields["phase"]
            if event == "key":
                self.assertEqual(phase, "changing")
        self.assertGreater(runner.sent, 0)
        self.assertEqual(events[-1][0], "pass")


if __name__ == "__main__":
    unittest.main()
