"""Exercise the shell runner without Go, an SDK build, or network access."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "integration" / "run.sh"


class IntegrationRunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="jev runner ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.host = self.root / "host source"
        self.host.mkdir()
        (self.host / "sentinel").write_text("do not change")
        self.stage = self.root / "staged package"
        self.packed = self.stage / "install-root" / "settings.json"
        self.packed.parent.mkdir(parents=True)
        self.packed.write_text(json.dumps(json.loads((ROOT / "plugin.json").read_text())["settings"]))
        self.scratch = self.root / "scratch space"
        self.scratch.mkdir()
        (self.scratch / "sentinel").write_text("not owned by runner")
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.capture = self.root / "go-call.json"
        fake_go = self.bin / "go"
        fake_go.write_text("#!" + sys.executable + "\n" + '''
import json, os, pathlib, signal, sys
overlay = pathlib.Path(sys.argv[sys.argv.index("-overlay") + 1])
pathlib.Path(os.environ["GO_CAPTURE"]).write_text(json.dumps({
    "args": sys.argv[1:], "cwd": os.getcwd(),
    "stage": os.environ["JEV_STAGE"], "host": os.environ["AII_OS_DIR"],
    "tmpdir": os.environ["TMPDIR"], "overlay_path": str(overlay),
    "overlay": json.loads(overlay.read_text()),
}))
if os.environ.get("GO_SIGNAL"):
    os.kill(os.getppid(), int(os.environ["GO_SIGNAL"]))
sys.exit(int(os.environ.get("GO_EXIT", "0")))
''')
        fake_go.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        AII_OS_DIR="host source", JEV_STAGE="staged package",
                        TMPDIR="scratch space", GO_CAPTURE=str(self.capture),
                        GO_EXIT="0", GO_SIGNAL="", CDPATH="/tmp")

    def run_runner(self, expected=0):
        before = {str(p.relative_to(self.host)): p.read_bytes()
                  for p in self.host.rglob("*") if p.is_file()}
        result = subprocess.run(["sh", str(RUNNER)], cwd=self.root, env=self.env,
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        after = {str(p.relative_to(self.host)): p.read_bytes()
                 for p in self.host.rglob("*") if p.is_file()}
        self.assertEqual(before, after, "runner changed the host checkout")
        self.assertEqual(list(self.scratch.iterdir()), [self.scratch / "sentinel"])
        if self.capture.exists():
            call = json.loads(self.capture.read_text())
            self.assertFalse(Path(call["overlay_path"]).parent.exists(), "temp directory leaked")
            return call
        return None

    def test_relative_paths_spaces_and_cleanup(self):
        call = self.run_runner()
        self.assertEqual(call["cwd"], str(self.host))
        self.assertEqual(call["host"], str(self.host))
        self.assertEqual(call["stage"], str(self.stage))
        self.assertEqual(call["tmpdir"], str(self.scratch))
        self.assertEqual(Path(call["overlay_path"]).parent.parent, self.scratch)
        self.assertEqual(call["args"], ["test", "-race", "-overlay", call["overlay_path"],
                         "./internal/pluginhost", "./internal/conversation", "-run",
                         "^TestGenericJev", "-count=1", "-v"])
        self.assertEqual(call["overlay"]["Replace"], {
            str(self.host / "internal/pluginhost/generic_jev_external_test.go"):
                str(ROOT / "integration/broker_test.go.txt"),
            str(self.host / "internal/conversation/generic_jev_delivery_test.go"):
                str(ROOT / "integration/delivery_test.go.txt"),
        })

    def test_absolute_paths(self):
        self.env.update(AII_OS_DIR=str(self.host), JEV_STAGE=str(self.stage), TMPDIR=str(self.scratch))
        call = self.run_runner()
        self.assertEqual(call["stage"], str(self.stage))

    def test_unset_and_empty_tmpdir_default_to_tmp(self):
        for value in (None, ""):
            with self.subTest(value=value):
                if value is None:
                    self.env.pop("TMPDIR", None)
                else:
                    self.env["TMPDIR"] = value
                call = self.run_runner()
                self.assertEqual(Path(call["overlay_path"]).parent.parent, Path("/tmp").resolve())

    def test_go_failure_preserves_exit_and_cleans_up(self):
        self.env["GO_EXIT"] = "23"
        self.run_runner(23)

    def test_signal_exit_cleans_up(self):
        for number in (1, 2, 15):
            with self.subTest(signal=number):
                self.env["GO_SIGNAL"] = str(number)
                self.run_runner(128 + number)

    def test_lost_settings_fail_before_go(self):
        settings = json.loads(self.packed.read_text())
        del settings[1]["choices_from"]
        self.packed.write_text(json.dumps(settings))
        self.assertIsNone(self.run_runner(1))

    def test_bad_host_or_stage_fails_before_go(self):
        for name in ("AII_OS_DIR", "JEV_STAGE"):
            with self.subTest(name=name):
                original = self.env[name]
                self.env[name] = "does-not-exist"
                self.assertIsNone(self.run_runner(2))
                self.env[name] = original


if __name__ == "__main__":
    unittest.main()
