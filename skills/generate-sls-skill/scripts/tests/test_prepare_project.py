#!/usr/bin/env python3
"""
Unit tests for prepare_project.py.

Tests:
- is_valid_logstore checks for index.json
- _read_source_dist reads source distribution from prepare_summary.json

Run:
    python3 scripts/tests/test_prepare_project.py
"""

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from prepare_project import is_valid_logstore, _read_source_dist


class TestIsValidLogstore(unittest.TestCase):
    """Test is_valid_logstore function."""

    def test_returns_true_when_index_exists(self):
        """Returns True when index.json exists in directory."""
        with tempfile.TemporaryDirectory() as tmp:
            index_path = Path(tmp) / "index.json"
            index_path.write_text("{}", encoding="utf-8")

            result = is_valid_logstore(tmp)
            self.assertTrue(result)

    def test_returns_false_when_index_missing(self):
        """Returns False when index.json does not exist."""
        with tempfile.TemporaryDirectory() as tmp:
            result = is_valid_logstore(tmp)
            self.assertFalse(result)


class TestReadSourceDist(unittest.TestCase):
    """Test _read_source_dist function."""

    def test_reads_source_dist_from_prepare_summary(self):
        """Reads deduped_source_dist from parsed/prepare_summary.json."""
        with tempfile.TemporaryDirectory() as tmp:
            parsed_dir = Path(tmp) / "parsed"
            parsed_dir.mkdir()

            summary_data = {
                "deduped_source_dist": {
                    "dashboard": 10,
                    "alert": 5,
                }
            }
            (parsed_dir / "prepare_summary.json").write_text(
                json.dumps(summary_data), encoding="utf-8"
            )

            result = _read_source_dist(tmp)
            self.assertEqual(result, {"dashboard": 10, "alert": 5})

    def test_returns_empty_dict_when_file_missing(self):
        """Returns empty dict when prepare_summary.json does not exist."""
        with tempfile.TemporaryDirectory() as tmp:
            result = _read_source_dist(tmp)
            self.assertEqual(result, {})

    def test_returns_empty_dict_when_key_missing(self):
        """Returns empty dict when deduped_source_dist key is missing."""
        with tempfile.TemporaryDirectory() as tmp:
            parsed_dir = Path(tmp) / "parsed"
            parsed_dir.mkdir()

            (parsed_dir / "prepare_summary.json").write_text(
                "{}", encoding="utf-8"
            )

            result = _read_source_dist(tmp)
            self.assertEqual(result, {})


if __name__ == "__main__":
    unittest.main()
