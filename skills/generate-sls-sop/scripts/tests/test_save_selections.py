#!/usr/bin/env python3
"""
Unit tests for save_selections.py

Test cases:
1. test_output_format_passthrough - Verify output_format from stdin writes to manifest
2. test_output_format_default - Defaults to "SOP" when not provided
3. test_output_root_rename - Verify output_root replaces sop_root
4. test_backward_compat - Existing SOP format input still works
"""

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

# Path to the script under test
SCRIPT_PATH = Path(__file__).parent.parent / "save_selections.py"


class TestSaveSelections(unittest.TestCase):
    """Tests for save_selections.py script."""

    def setUp(self):
        """Create a temporary directory for test outputs."""
        self.temp_dir = tempfile.mkdtemp()
        self.project_dir = os.path.join(self.temp_dir, "test_project")
        os.makedirs(self.project_dir)

    def tearDown(self):
        """Clean up temporary directory."""
        import shutil
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def run_script(self, input_data: dict) -> tuple[dict, str]:
        """
        Run save_selections.py with given input data.
        
        Returns:
            tuple: (stdout_json, stderr_text)
        """
        result = subprocess.run(
            [sys.executable, str(SCRIPT_PATH), self.project_dir],
            input=json.dumps(input_data),
            capture_output=True,
            text=True,
        )
        
        if result.returncode != 0:
            raise RuntimeError(f"Script failed: {result.stderr}")
        
        stdout_json = json.loads(result.stdout) if result.stdout.strip() else {}
        return stdout_json, result.stderr

    def read_manifest(self) -> dict:
        """Read the selected_logstores.json manifest file."""
        manifest_path = os.path.join(self.project_dir, "selected_logstores.json")
        with open(manifest_path, "r", encoding="utf-8") as f:
            return json.load(f)

    def test_output_format_passthrough(self):
        """Verify output_format from stdin correctly writes to selected_logstores.json."""
        input_data = {
            "output_root": "skills",
            "project_alias": "k8s-log",
            "output_format": "SKILL",
            "selections": {
                "audit": "skills/k8s-log/audit/SKILL.md",
            }
        }
        
        self.run_script(input_data)
        manifest = self.read_manifest()
        
        self.assertEqual(manifest["output_format"], "SKILL")
        self.assertEqual(manifest["output_root"], "skills")
        self.assertEqual(manifest["project_alias"], "k8s-log")
        self.assertEqual(len(manifest["logstores"]), 1)
        self.assertEqual(manifest["logstores"][0]["output_path"], "skills/k8s-log/audit/SKILL.md")

    def test_output_format_default(self):
        """When output_format is not provided, defaults to 'SOP'."""
        input_data = {
            "output_root": "sop-docs",
            "project_alias": "k8s-log",
            "selections": {
                "audit": "sop-docs/k8s-log/audit/overview.md",
            }
        }
        
        self.run_script(input_data)
        manifest = self.read_manifest()
        
        self.assertEqual(manifest["output_format"], "SOP")

    def test_output_root_rename(self):
        """Verify output_root replaces sop_root and works correctly."""
        input_data = {
            "output_root": "custom-output",
            "project_alias": "my-project",
            "output_format": "SOP",
            "selections": {
                "logstore1": "custom-output/my-project/logstore1/overview.md",
                "logstore2": "custom-output/my-project/logstore2/overview.md",
            }
        }
        
        self.run_script(input_data)
        manifest = self.read_manifest()
        
        # Verify output_root is used (not sop_root)
        self.assertIn("output_root", manifest)
        self.assertNotIn("sop_root", manifest)
        self.assertEqual(manifest["output_root"], "custom-output")
        
        # Verify logstores are correctly stored
        self.assertEqual(len(manifest["logstores"]), 2)
        logstore_names = {ls["name"] for ls in manifest["logstores"]}
        self.assertEqual(logstore_names, {"logstore1", "logstore2"})

    def test_backward_compat(self):
        """Existing SOP format input still works correctly."""
        # Create logstore directories
        os.makedirs(os.path.join(self.project_dir, "audit"))
        os.makedirs(os.path.join(self.project_dir, "access"))
        
        input_data = {
            "output_root": "sop-docs",
            "project_alias": "k8s-log",
            "selections": {
                "audit": "sop-docs/k8s-log/audit/overview.md",
                "access": "sop-docs/k8s-log/access/overview.md",
            }
        }
        
        stdout_json, stderr = self.run_script(input_data)
        manifest = self.read_manifest()
        
        # Verify manifest structure
        self.assertEqual(manifest["output_root"], "sop-docs")
        self.assertEqual(manifest["project_alias"], "k8s-log")
        self.assertEqual(manifest["output_format"], "SOP")  # Default
        self.assertEqual(len(manifest["logstores"]), 2)
        
        # Verify stdout summary
        self.assertEqual(stdout_json["count"], 2)
        self.assertIn("audit", stdout_json["logstores"])
        self.assertIn("access", stdout_json["logstores"])
        
        # Verify per-logstore skill_options.json updated
        audit_opts_path = os.path.join(self.project_dir, "audit", "skill_options.json")
        self.assertTrue(os.path.exists(audit_opts_path))
        with open(audit_opts_path, "r") as f:
            audit_opts = json.load(f)
        self.assertEqual(audit_opts["output_path"], "sop-docs/k8s-log/audit/overview.md")

    def test_multiple_logstores_skill_format(self):
        """Test multiple logstores with SKILL format."""
        # Create logstore directories
        os.makedirs(os.path.join(self.project_dir, "audit"))
        os.makedirs(os.path.join(self.project_dir, "access"))
        os.makedirs(os.path.join(self.project_dir, "network"))
        
        input_data = {
            "output_root": "skills",
            "project_alias": "k8s-log",
            "output_format": "SKILL",
            "selections": {
                "audit": "skills/k8s-log/audit/SKILL.md",
                "access": "skills/k8s-log/access/SKILL.md",
                "network": "skills/k8s-log/network/SKILL.md",
            }
        }
        
        stdout_json, _ = self.run_script(input_data)
        manifest = self.read_manifest()
        
        self.assertEqual(manifest["output_format"], "SKILL")
        self.assertEqual(stdout_json["count"], 3)
        
        # Verify all logstore skill_options.json files
        for name in ["audit", "access", "network"]:
            opts_path = os.path.join(self.project_dir, name, "skill_options.json")
            with open(opts_path, "r") as f:
                opts = json.load(f)
            self.assertEqual(opts["output_path"], f"skills/k8s-log/{name}/SKILL.md")


if __name__ == "__main__":
    unittest.main()
