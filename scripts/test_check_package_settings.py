import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
GUARD = ROOT / "scripts" / "check_package_settings.py"


class SettingsGuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.author = self.root / "plugin.json"
        self.stage = self.root / "staged package"
        self.packed = self.stage / "install-root" / "settings.json"
        self.packed.parent.mkdir(parents=True)
        self.settings = json.loads((ROOT / "plugin.json").read_text())["settings"]
        self.settings[0]["oauth"] = {"provider": "example", "services": ["read"]}
        self.settings[1]["future_metadata"] = {"nested": ["preserve", True]}
        self.author.write_text(json.dumps({"settings": self.settings}))
        self.write_settings(self.settings)

    def write_settings(self, settings):
        self.packed.write_text(json.dumps(settings, sort_keys=True, indent=4))

    def run_guard(self, expected=0, default_author=False):
        args = [sys.executable, str(GUARD), str(self.stage)]
        if not default_author:
            args += ["--plugin", str(self.author)]
        before = {p: p.read_bytes() for p in self.root.rglob("*") if p.is_file()}
        result = subprocess.run(args, cwd=self.root, capture_output=True, text=True)
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        after = {p: p.read_bytes() for p in self.root.rglob("*") if p.is_file()}
        self.assertEqual(before, after, "guard must not modify its inputs")
        return result

    def test_formatting_and_object_order_do_not_matter(self):
        self.assertIn("complete author settings preserved", self.run_guard().stdout)

    def test_default_author_is_repository_not_cwd(self):
        self.write_settings(json.loads((ROOT / "plugin.json").read_text())["settings"])
        self.run_guard(default_author=True)

    def test_every_dropped_field_fails(self):
        for index, setting in enumerate(self.settings):
            for key in setting:
                with self.subTest(index=index, key=key):
                    changed = copy.deepcopy(self.settings)
                    del changed[index][key]
                    self.write_settings(changed)
                    result = self.run_guard(1)
                    self.assertIn("SDK 0.1.18", result.stderr)

    def test_changed_added_reordered_or_missing_settings_fail(self):
        variants = []
        changed = copy.deepcopy(self.settings)
        changed[1]["choices_from"] = "wrong_operation"
        variants.append(changed)
        changed = copy.deepcopy(self.settings)
        changed[0]["required"] = 1
        variants.append(changed)
        changed = copy.deepcopy(self.settings)
        changed[1]["added"] = "unexpected"
        variants += [changed, list(reversed(self.settings)), self.settings[:1], [], {}]
        for changed in variants:
            with self.subTest(settings=changed):
                self.write_settings(changed)
                self.run_guard(1)

    def test_missing_or_malformed_staged_file_fails(self):
        self.packed.unlink()
        self.run_guard(1)
        for raw in ("{", '[{"key":"a","key":"b"}]', "[NaN]", "[Infinity]"):
            with self.subTest(raw=raw):
                self.packed.write_text(raw)
                self.run_guard(1)

    def test_missing_or_invalid_author_settings_fail(self):
        for raw in ("{}", "[]", '{"settings":[]}', '{"settings":null}',
                    '{"settings":[],"settings":[]}'):
            with self.subTest(raw=raw):
                self.author.write_text(raw)
                self.run_guard(1)
        self.author.unlink()
        self.run_guard(1)


if __name__ == "__main__":
    unittest.main()
