#!/usr/bin/env python3
"""
Unit tests for fetch_sls_data.py.

Run:
    python3 scripts/tests/test_fetch_sls_data.py
"""

import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from fetch_sls_data import (
    _load_internal_logstores,
    _validate_fetch_stats_partition,
    _write_data_summary_md,
)


class TestLoadInternalLogstores(unittest.TestCase):

    def test_load_returns_set_when_file_exists(self):
        """Returns expected set when file exists with valid lines."""
        with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False, encoding="utf-8") as f:
            f.write("internal-alert-history\n")
            f.write("internal-operation_log\n")
            path = Path(f.name)
        try:
            result = _load_internal_logstores(config_path=path)
            self.assertEqual(result, {"internal-alert-history", "internal-operation_log"})
        finally:
            path.unlink(missing_ok=True)

    def test_load_returns_empty_when_file_missing(self):
        """Returns empty set when file does not exist."""
        path = Path("/nonexistent/internal_logstores.txt")
        result = _load_internal_logstores(config_path=path)
        self.assertEqual(result, set())

    def test_load_ignores_empty_lines_and_comments(self):
        """Ignores empty lines and lines starting with #."""
        with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False, encoding="utf-8") as f:
            f.write("# comment\n")
            f.write("\n")
            f.write("internal-alert-history\n")
            f.write("  \n")
            f.write("# another comment\n")
            f.write("internal-ml-log\n")
            path = Path(f.name)
        try:
            result = _load_internal_logstores(config_path=path)
            self.assertEqual(result, {"internal-alert-history", "internal-ml-log"})
        finally:
            path.unlink(missing_ok=True)

    def test_load_strips_whitespace(self):
        """Strips leading and trailing whitespace from lines."""
        with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False, encoding="utf-8") as f:
            f.write("  internal-alert-history  \n")
            f.write("\tinternal-operation_log\t\n")
            path = Path(f.name)
        try:
            result = _load_internal_logstores(config_path=path)
            self.assertEqual(result, {"internal-alert-history", "internal-operation_log"})
        finally:
            path.unlink(missing_ok=True)


class TestWriteDataSummaryMd(unittest.TestCase):
    """Test _write_data_summary_md function."""

    def test_generates_full_table(self):
        """Generates markdown table with all rows when all stats are non-zero."""
        with tempfile.TemporaryDirectory() as tmp:
            fetch_stats = {
                "total_logstores": 200,
                "internal_skipped": 5,
                "no_resource_skipped": 71,
                "no_index_skipped": 15,
                "fetched_logstores": 109,
            }
            _write_data_summary_md(tmp, fetch_stats)

            md_path = Path(tmp) / "data_summary.md"
            self.assertTrue(md_path.exists())

            content = md_path.read_text(encoding="utf-8")

            # Check header
            self.assertIn("数据摘要", content)
            self.assertIn("| 阶段 | 过滤数 | 剩余 | 说明 |", content)

            # Check rows (200 - 5 = 195, 195 - 71 = 124, 124 - 15 = 109)
            self.assertIn("| SLS 全部 | - | 200 |", content)
            self.assertIn("| internal 跳过 | -5 | 195 |", content)
            self.assertIn("无关联资源跳过 | -71 | 124 |", content)
            self.assertIn("无索引配置跳过 | -15 | 109 |", content)
            # Final row removed - last filter row shows the final count

    def test_skips_zero_rows(self):
        """Skips rows with zero values."""
        with tempfile.TemporaryDirectory() as tmp:
            fetch_stats = {
                "total_logstores": 100,
                "internal_skipped": 0,
                "no_resource_skipped": 0,
                "no_index_skipped": 0,
                "fetched_logstores": 100,
            }
            _write_data_summary_md(tmp, fetch_stats)

            md_path = Path(tmp) / "data_summary.md"
            content = md_path.read_text(encoding="utf-8")

            # These rows should NOT appear
            self.assertNotIn("internal 跳过", content)
            self.assertNotIn("无关联资源跳过", content)
            self.assertNotIn("无索引配置跳过", content)

            # These should appear
            self.assertIn("| SLS 全部 | - | 100 |", content)
            # No final summary row when all filters are zero

    def test_handles_missing_stats(self):
        """Handles empty stats gracefully."""
        with tempfile.TemporaryDirectory() as tmp:
            fetch_stats = {}
            _write_data_summary_md(tmp, fetch_stats)

            md_path = Path(tmp) / "data_summary.md"
            self.assertTrue(md_path.exists())

            content = md_path.read_text(encoding="utf-8")
            self.assertIn("数据摘要", content)
            self.assertIn("| SLS 全部 | - | 0 |", content)


class TestFetchStatsPartition(unittest.TestCase):
    """Test _validate_fetch_stats_partition function."""

    def test_partition_full_project(self):
        """Full project mode: 200 = 5 + 71 + 15 + 109."""
        fetch_stats = {
            "total_logstores": 200,
            "internal_skipped": 5,
            "no_resource_skipped": 71,
            "no_index_skipped": 15,
            "fetched_logstores": 109,
        }
        _validate_fetch_stats_partition(fetch_stats)  # does not raise

    def test_partition_filtered_single(self):
        """Single logstore filter: 1 = 0 + 0 + 0 + 1."""
        fetch_stats = {
            "total_logstores": 1,
            "internal_skipped": 0,
            "no_resource_skipped": 0,
            "no_index_skipped": 0,
            "fetched_logstores": 1,
        }
        _validate_fetch_stats_partition(fetch_stats)  # does not raise

    def test_partition_filtered_multiple(self):
        """Multiple logstores filter: 3 = 0 + 0 + 1 + 2."""
        fetch_stats = {
            "total_logstores": 3,
            "internal_skipped": 0,
            "no_resource_skipped": 0,
            "no_index_skipped": 1,
            "fetched_logstores": 2,
        }
        _validate_fetch_stats_partition(fetch_stats)  # does not raise

    def test_partition_invalid_raises(self):
        """Raises AssertionError when partition does not sum correctly."""
        fetch_stats = {
            "total_logstores": 28,
            "internal_skipped": 0,
            "no_resource_skipped": 0,
            "no_index_skipped": 0,
            "fetched_logstores": 1,
        }
        with self.assertRaises(AssertionError) as ctx:
            _validate_fetch_stats_partition(fetch_stats)
        self.assertIn("Statistical partition mismatch", str(ctx.exception))


if __name__ == "__main__":
    unittest.main()
