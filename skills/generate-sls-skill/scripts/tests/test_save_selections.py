#!/usr/bin/env python3
"""
Unit tests for save_selections.py.

Run:
    python3 scripts/tests/test_save_selections.py
"""

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parent.parent / "save_selections.py"


class TestSaveSelections(unittest.TestCase):
    """Tests for SKILL-only selection persistence."""

    def setUp(self):
        self.temp_dir = tempfile.mkdtemp()
        self.project_dir = os.path.join(self.temp_dir, "test_project")
        os.makedirs(self.project_dir)

    def tearDown(self):
        import shutil
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def run_script(self, input_data: dict, expect_success: bool = True):
        result = subprocess.run(
            [sys.executable, str(SCRIPT_PATH), self.project_dir],
            input=json.dumps(input_data),
            capture_output=True,
            text=True,
        )
        if expect_success and result.returncode != 0:
            raise RuntimeError(f"Script failed: {result.stderr}")
        if not expect_success:
            self.assertNotEqual(result.returncode, 0)
            return result
        stdout_json = json.loads(result.stdout) if result.stdout.strip() else {}
        return stdout_json, result.stderr

    def read_manifest(self) -> dict:
        manifest_path = os.path.join(self.project_dir, "selected_logstores.json")
        with open(manifest_path, "r", encoding="utf-8") as f:
            return json.load(f)

    def test_reference_paths_are_persisted(self):
        input_data = {
            "output_root": "skills",
            "project_alias": "k8s-log",
            "selections": {
                "audit": "skills/k8s-log/references/audit.md",
            },
        }

        self.run_script(input_data)
        manifest = self.read_manifest()

        self.assertNotIn("output_format", manifest)
        self.assertEqual(manifest["output_root"], "skills")
        self.assertEqual(manifest["project_alias"], "k8s-log")
        self.assertEqual(len(manifest["logstores"]), 1)
        self.assertEqual(
            manifest["logstores"][0]["output_path"],
            "skills/k8s-log/references/audit.md",
        )

        opts_path = os.path.join(self.project_dir, "audit", "skill_options.json")
        with open(opts_path, "r", encoding="utf-8") as f:
            opts = json.load(f)
        self.assertEqual(opts["output_path"], "skills/k8s-log/references/audit.md")

    def test_rejects_output_format_field(self):
        input_data = {
            "output_root": "skills",
            "project_alias": "k8s-log",
            "output_format": "SKILL",
            "selections": {
                "audit": "skills/k8s-log/references/audit.md",
            },
        }

        result = self.run_script(input_data, expect_success=False)
        self.assertIn("output_format", result.stderr)

    def test_rejects_legacy_overview_path(self):
        input_data = {
            "output_root": "skills",
            "project_alias": "k8s-log",
            "selections": {
                "audit": "skills/k8s-log/audit/overview.md",
            },
        }

        result = self.run_script(input_data, expect_success=False)
        self.assertIn("references", result.stderr)

    def test_rejects_nested_skill_path(self):
        input_data = {
            "output_root": "skills",
            "project_alias": "k8s-log",
            "selections": {
                "audit": "skills/k8s-log/audit/SKILL.md",
            },
        }

        result = self.run_script(input_data, expect_success=False)
        self.assertIn("references", result.stderr)

    def test_rejects_extra_query_path_as_main_output(self):
        input_data = {
            "output_root": "skills",
            "project_alias": "k8s-log",
            "selections": {
                "audit": "skills/k8s-log/references/audit-queries-extra.md",
            },
        }

        result = self.run_script(input_data, expect_success=False)
        self.assertIn("extra query", result.stderr)

    def test_multiple_logstores(self):
        input_data = {
            "output_root": "custom-output",
            "project_alias": "my-project",
            "selections": {
                "audit": "custom-output/my-project/references/audit.md",
                "access": "custom-output/my-project/references/access.md",
                "network": "custom-output/my-project/references/network.md",
            },
        }

        stdout_json, _ = self.run_script(input_data)
        manifest = self.read_manifest()

        self.assertEqual(stdout_json["count"], 3)
        self.assertEqual(manifest["output_root"], "custom-output")
        self.assertEqual(manifest["project_alias"], "my-project")
        self.assertNotIn("output_format", manifest)

        for name in ["audit", "access", "network"]:
            opts_path = os.path.join(self.project_dir, name, "skill_options.json")
            with open(opts_path, "r", encoding="utf-8") as f:
                opts = json.load(f)
            self.assertEqual(
                opts["output_path"],
                f"custom-output/my-project/references/{name}.md",
            )


if __name__ == "__main__":
    unittest.main()
