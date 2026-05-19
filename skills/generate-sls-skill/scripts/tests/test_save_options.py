#!/usr/bin/env python3
"""
Unit tests for save_options.py.

Run:
    python3 scripts/tests/test_save_options.py
"""

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parent.parent / "save_options.py"


class TestSaveOptions(unittest.TestCase):
    """Tests for SKILL-only option persistence."""

    def setUp(self):
        self.temp_dir = tempfile.mkdtemp()
        self.project_dir = Path(self.temp_dir) / "project"
        self.logstore_dir = self.project_dir / "audit"
        self.logstore_dir.mkdir(parents=True)
        (self.logstore_dir / "index.json").write_text("{}", encoding="utf-8")

    def tearDown(self):
        import shutil
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_writes_logstore_options_without_output_format(self):
        result = subprocess.run(
            [sys.executable, str(SCRIPT_PATH), str(self.project_dir), "--validate-queries"],
            capture_output=True,
            text=True,
            check=True,
        )

        summary = json.loads(result.stdout)
        self.assertNotIn("output_format", summary)
        self.assertEqual(summary["count"], 1)

        opts_path = self.logstore_dir / "skill_options.json"
        opts = json.loads(opts_path.read_text(encoding="utf-8"))
        self.assertEqual(opts, {"validate_queries": True})

        self.assertFalse((self.project_dir / "skill_options.json").exists())

    def test_rejects_output_format_argument(self):
        result = subprocess.run(
            [
                sys.executable,
                str(SCRIPT_PATH),
                str(self.project_dir),
                "--output-format",
                "SKILL",
            ],
            capture_output=True,
            text=True,
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unrecognized arguments", result.stderr)


if __name__ == "__main__":
    unittest.main()
