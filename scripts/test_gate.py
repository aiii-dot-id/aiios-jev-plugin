import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
GATE = ROOT / "scripts" / "gate.sh"


class GateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="jev gate ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.calls = self.root / "calls"
        for tool in ("go", "tinygo", "python3"):
            fake = self.bin / tool
            fake.write_text("#!/bin/sh\necho %s >> '%s'\nexit 0\n" % (tool, self.calls))
            fake.chmod(0o755)

    def run_gate(self, **env):
        base = {"PATH": str(self.bin) + os.pathsep + "/usr/bin:/bin"}
        base.update(env)
        return subprocess.run(["sh", str(GATE)], cwd=self.root, env=base,
                              capture_output=True, text=True, timeout=10)

    def test_missing_host_and_oracle_is_incomplete_and_runs_nothing(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 3, result.stderr)
        self.assertIn("INCOMPLETE", result.stderr)
        self.assertIn("AII_OS_DIR", result.stderr)
        self.assertIn("AII_OS_BIN", result.stderr)
        self.assertFalse(self.calls.exists(), "a step ran before the prerequisites were checked")

    def test_a_missing_directory_is_missing(self):
        result = self.run_gate(AII_OS_DIR=str(self.root / "absent"), AII_OS_BIN=str(self.bin))
        self.assertEqual(result.returncode, 3, result.stderr)
        self.assertIn("AII_OS_DIR", result.stderr)
        self.assertNotIn("AII_OS_BIN", result.stderr)

    def test_a_missing_tool_is_named(self):
        (self.bin / "tinygo").unlink()
        result = self.run_gate(AII_OS_DIR=str(self.root), AII_OS_BIN=str(self.bin))
        self.assertEqual(result.returncode, 3, result.stderr)
        self.assertIn("tinygo", result.stderr)


if __name__ == "__main__":
    unittest.main()
