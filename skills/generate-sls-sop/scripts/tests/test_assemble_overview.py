#!/usr/bin/env python3
"""
Unit tests for assemble_overview.py — usage instruction conditional on queries_extra.

Run:
    python3 scripts/tests/test_assemble_overview.py
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from assemble_overview import main as assemble_main


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_logstore_dir():
    """Create a temp logstore dir with required subdirs."""
    base = tempfile.mkdtemp()
    os.makedirs(os.path.join(base, "fragments"))
    os.makedirs(os.path.join(base, "parsed"))
    return base


def _write(path, content):
    with open(path, "w", encoding="utf-8") as f:
        f.write(content)


def _write_json(path, data):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def _setup_required_fragments(logstore_dir, output_path):
    """Write the minimal required fragments and skill_options."""
    frag = os.path.join(logstore_dir, "fragments")
    _write(os.path.join(frag, "datasource.md"), "## 数据源\n\n测试数据源描述。")
    _write(os.path.join(frag, "fields_table.md"), "## 字段说明\n\n| 字段 | 说明 |\n|---|---|\n| level | 日志级别 |")
    _write_json(os.path.join(logstore_dir, "skill_options.json"), {"output_path": output_path})


# ===========================================================================
# Tests
# ===========================================================================

class TestUsageInstructionConditional(unittest.TestCase):
    """Usage instruction should mention queries_extra.md only when it exists."""

    def test_without_extra_no_mention(self):
        """When queries_extra.md does not exist, usage instruction should NOT mention it."""
        logstore_dir = _make_logstore_dir()
        output_dir = tempfile.mkdtemp()
        output_path = os.path.join(output_dir, "overview.md")
        _setup_required_fragments(logstore_dir, output_path)

        # Add queries_selected but NO queries_extra
        frag = os.path.join(logstore_dir, "fragments")
        _write(os.path.join(frag, "queries_selected.md"), "## 查询示例\n\n示例查询内容。")

        sys.argv = [
            "assemble_overview.py", logstore_dir,
            "--name", "test-logstore",
            "--description", "测试用 logstore",
        ]
        assemble_main()

        with open(output_path, "r", encoding="utf-8") as f:
            content = f.read()

        self.assertIn("## 使用说明", content)
        self.assertNotIn("queries_extra.md", content)
        self.assertIn("本文档中的查询示例", content)

    def test_with_extra_mentions_it(self):
        """When queries_extra.md exists, usage instruction SHOULD mention it."""
        logstore_dir = _make_logstore_dir()
        output_dir = tempfile.mkdtemp()
        output_path = os.path.join(output_dir, "overview.md")
        _setup_required_fragments(logstore_dir, output_path)

        frag = os.path.join(logstore_dir, "fragments")
        _write(os.path.join(frag, "queries_selected.md"), "## 查询示例\n\n示例查询内容。")
        _write(os.path.join(frag, "queries_extra.md"), "## 补充查询\n\n补充内容。")

        # Pipeline JSON required when extra exists
        _write_json(os.path.join(logstore_dir, "parsed", "query_pipeline.json"), {
            "selected": [{"id": "q0"}, {"id": "q1"}],
            "extra": [{"id": "q2"}],
        })

        sys.argv = [
            "assemble_overview.py", logstore_dir,
            "--name", "test-logstore",
            "--description", "测试用 logstore",
        ]
        assemble_main()

        with open(output_path, "r", encoding="utf-8") as f:
            content = f.read()

        self.assertIn("## 使用说明", content)
        self.assertIn("queries_extra.md", content)
        self.assertIn("优先使用本文档的查询示例", content)
        self.assertIn("找不到时再参考 queries_extra.md", content)

        # Also verify queries_extra.md was copied to output dir
        extra_output = os.path.join(output_dir, "queries_extra.md")
        self.assertTrue(os.path.exists(extra_output))


if __name__ == "__main__":
    unittest.main()
