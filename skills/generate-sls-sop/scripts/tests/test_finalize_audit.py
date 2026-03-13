#!/usr/bin/env python3
"""
Unit tests for finalize_audit.py.

Covers issue filtering, context reading, summary computation, and report generation.

Run:
    python3 scripts/tests/test_finalize_audit.py
"""

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from finalize_audit import (
    filter_issues,
    finalize_audit,
    generate_report_md,
    compute_summary,
    read_context_from_audit_context,
)


class TestFilterIssues(unittest.TestCase):
    """Test issue filtering by severity."""

    def test_keeps_error_and_warn(self):
        """ERROR and WARN issues are kept."""
        issues = [
            {"check": "x", "severity": "ERROR", "detail": "e"},
            {"check": "x", "severity": "WARN", "detail": "w"},
        ]
        result = filter_issues(issues)
        self.assertEqual(len(result), 2)

    def test_filters_ok_and_info(self):
        """OK and INFO issues are filtered out."""
        issues = [
            {"check": "x", "severity": "WARN", "detail": "w"},
            {"check": "x", "severity": "OK", "detail": "o"},
            {"check": "x", "severity": "INFO", "detail": "i"},
        ]
        result = filter_issues(issues)
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["severity"], "WARN")


class TestReadContext(unittest.TestCase):
    """Test context extraction from audit_context.json."""

    def test_maps_candidates_count(self):
        """candidates_count maps to candidates_total."""
        audit_context = {
            "candidates_count": 32,
            "selected_count": 20,
            "extra_count": 5,
            "categories": ["A", "B"],
        }
        ctx = read_context_from_audit_context(audit_context)
        self.assertEqual(ctx["candidates_total"], 32)
        self.assertEqual(ctx["selected_count"], 20)
        self.assertEqual(ctx["extra_count"], 5)
        self.assertEqual(ctx["categories"], ["A", "B"])


class TestComputeSummary(unittest.TestCase):
    """Test summary computation."""

    def test_summary_counts(self):
        """Summary correctly counts by severity and check."""
        issues = [
            {"check": "title_accuracy", "severity": "WARN"},
            {"check": "title_accuracy", "severity": "ERROR"},
            {"check": "category_reasonableness", "severity": "WARN"},
        ]
        summary = compute_summary(issues)
        self.assertEqual(summary["total_issues"], 3)
        self.assertEqual(summary["by_severity"]["WARN"], 2)
        self.assertEqual(summary["by_severity"]["ERROR"], 1)
        self.assertEqual(summary["by_check"]["title_accuracy"], 2)
        self.assertEqual(summary["by_check"]["category_reasonableness"], 1)


class TestGenerateReportMd(unittest.TestCase):
    """Test markdown report generation."""

    def test_report_contains_overview(self):
        """Report contains overview table."""
        md = generate_report_md(
            "test_ls",
            {"candidates_total": 10, "selected_count": 5, "extra_count": 2, "categories": []},
            [],
            {"total_issues": 0, "by_severity": {}, "by_check": {}},
        )
        self.assertIn("# 审计报告：test_ls", md)
        self.assertIn("| 候选查询数 | 10 |", md)
        self.assertIn("| 问题总数 | 0 |", md)

    def test_report_contains_issues(self):
        """Report contains issue details."""
        issues = [
            {"check": "title_accuracy", "query_id": "q1", "severity": "WARN", "detail": "标题不准确"},
        ]
        summary = compute_summary(issues)
        md = generate_report_md(
            "test_ls",
            {"candidates_total": 5, "selected_count": 3, "extra_count": 1, "categories": []},
            issues,
            summary,
        )
        self.assertIn("title_accuracy", md)
        self.assertIn("q1", md)
        self.assertIn("标题不准确", md)


class TestFinalizeAudit(unittest.TestCase):
    """Test full finalize_audit flow."""

    def test_finalize_produces_result_and_report(self):
        """finalize_audit produces audit_result.json and audit_report.md."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "project"
            audit_dir = project_dir / "_audit" / "test_ls"
            audit_dir.mkdir(parents=True)

            (audit_dir / "audit_issues.json").write_text(
                json.dumps({
                    "issues": [
                        {"check": "title_accuracy", "query_id": "q1", "severity": "WARN", "detail": "d1"},
                    ],
                }, ensure_ascii=False)
            )
            (audit_dir / "audit_context.json").write_text(
                json.dumps({
                    "logstore": "test_ls",
                    "candidates_count": 10,
                    "selected_count": 5,
                    "extra_count": 2,
                    "categories": ["A", "B"],
                }, ensure_ascii=False)
            )

            result = finalize_audit(str(project_dir), "test_ls")

            self.assertEqual(result["logstore"], "test_ls")
            self.assertEqual(len(result["issues"]), 1)
            self.assertEqual(result["summary"]["total_issues"], 1)
            self.assertEqual(result["context"]["candidates_total"], 10)

    def test_finalize_filters_non_error_warn(self):
        """finalize_audit filters OK/INFO from issues."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "project"
            audit_dir = project_dir / "_audit" / "test_ls"
            audit_dir.mkdir(parents=True)

            (audit_dir / "audit_issues.json").write_text(
                json.dumps({
                    "issues": [
                        {"check": "x", "query_id": "q1", "severity": "WARN", "detail": "w"},
                        {"check": "x", "query_id": "q2", "severity": "OK", "detail": "o"},
                    ],
                }, ensure_ascii=False)
            )
            (audit_dir / "audit_context.json").write_text(
                json.dumps({
                    "candidates_count": 5, "selected_count": 3, "extra_count": 1,
                    "categories": [],
                }, ensure_ascii=False)
            )

            result = finalize_audit(str(project_dir), "test_ls")

            self.assertEqual(len(result["issues"]), 1)
            self.assertEqual(result["issues"][0]["severity"], "WARN")


if __name__ == "__main__":
    unittest.main()
