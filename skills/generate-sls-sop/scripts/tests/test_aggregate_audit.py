#!/usr/bin/env python3
"""
Unit tests for aggregate_audit.py.

Covers JSON aggregation, statistics calculation, and report generation.

Run:
    python3 scripts/tests/test_aggregate_audit.py
"""

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from aggregate_audit import aggregate_audits, generate_summary_md, validate_result


def _create_audit_result_old_format(logstore: str, checks: list[dict]) -> dict:
    """Create an audit_result.json content in OLD format (checks array)."""
    total_issues = sum(len(c.get("issues", [])) for c in checks)
    by_severity = {}
    for check in checks:
        for issue in check.get("issues", []):
            sev = issue.get("severity", "WARN")
            by_severity[sev] = by_severity.get(sev, 0) + 1
    
    return {
        "logstore": logstore,
        "checks": checks,
        "summary": {
            "total_issues": total_issues,
            "by_severity": by_severity,
        },
    }


def _create_audit_result_new_format(logstore: str, issues: list[dict]) -> dict:
    """Create an audit_result.json content in NEW format (issues array at top level)."""
    by_severity = {}
    by_check = {}
    for issue in issues:
        sev = issue.get("severity", "WARN")
        check = issue.get("check", "unknown")
        by_severity[sev] = by_severity.get(sev, 0) + 1
        by_check[check] = by_check.get(check, 0) + 1
    
    return {
        "logstore": logstore,
        "context": {
            "candidates_total": 20,
            "selected_count": 15,
            "extra_count": 5,
            "categories": [],
        },
        "issues": issues,
        "summary": {
            "total_issues": len(issues),
            "by_severity": by_severity,
            "by_check": by_check,
        },
    }


# Alias for backward compatibility in existing tests
def _create_audit_result(logstore: str, checks: list[dict]) -> dict:
    return _create_audit_result_old_format(logstore, checks)


class TestAggregateAudits(unittest.TestCase):
    """Test audit result aggregation."""

    def test_empty_audit(self):
        """No audit results returns empty aggregation."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "empty_project"
            project_dir.mkdir()

            result = aggregate_audits(str(project_dir))

            self.assertEqual(result["logstores_audited"], 0)
            self.assertEqual(result["total_issues"], 0)

    def test_single_logstore_aggregation(self):
        """Single logstore audit result is properly aggregated."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "single_project"
            audit_dir = project_dir / "_audit" / "test_logstore"
            audit_dir.mkdir(parents=True)

            audit_result = _create_audit_result("test_logstore", [
                {
                    "check": "field_semantic",
                    "issues": [
                        {"field": "status", "issue": "too vague", "severity": "WARN"},
                        {"field": "type", "issue": "missing context", "severity": "ERROR"},
                    ],
                },
            ])
            (audit_dir / "audit_result.json").write_text(
                json.dumps(audit_result, ensure_ascii=False)
            )

            result = aggregate_audits(str(project_dir))

            self.assertEqual(result["logstores_audited"], 1)
            self.assertEqual(result["total_issues"], 2)
            self.assertEqual(result["issues_by_check"]["field_semantic"], 2)
            self.assertEqual(result["issues_by_severity"]["WARN"], 1)
            self.assertEqual(result["issues_by_severity"]["ERROR"], 1)

    def test_multiple_logstores_aggregation(self):
        """Multiple logstore audit results are properly aggregated."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "multi_project"
            
            # Create audit results for 3 logstores
            for i, ls_name in enumerate(["logstore_a", "logstore_b", "logstore_c"]):
                audit_dir = project_dir / "_audit" / ls_name
                audit_dir.mkdir(parents=True)

                audit_result = _create_audit_result(ls_name, [
                    {
                        "check": "field_semantic",
                        "issues": [{"field": f"field_{i}", "severity": "WARN"}],
                    },
                    {
                        "check": "annotation_semantic",
                        "issues": [
                            {"id": f"q{i}", "severity": "ERROR"},
                            {"id": f"q{i+10}", "severity": "WARN"},
                        ],
                    },
                ])
                (audit_dir / "audit_result.json").write_text(
                    json.dumps(audit_result, ensure_ascii=False)
                )

            result = aggregate_audits(str(project_dir))

            self.assertEqual(result["logstores_audited"], 3)
            self.assertEqual(result["total_issues"], 9)  # 3 logstores * 3 issues each
            self.assertEqual(result["issues_by_check"]["field_semantic"], 3)
            self.assertEqual(result["issues_by_check"]["annotation_semantic"], 6)

    def test_typical_issues_by_check(self):
        """One representative issue per check is stored in typical_issues_by_check."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "typical_project"
            audit_dir = project_dir / "_audit" / "test_logstore"
            audit_dir.mkdir(parents=True)

            audit_result = _create_audit_result("test_logstore", [
                {
                    "check": "field_semantic",
                    "issues": [
                        {"detail": "First vague desc", "severity": "WARN"},
                        {"detail": "Second vague desc", "severity": "WARN"},
                    ],
                },
            ])
            (audit_dir / "audit_result.json").write_text(
                json.dumps(audit_result, ensure_ascii=False)
            )

            result = aggregate_audits(str(project_dir))

            self.assertIn("typical_issues_by_check", result)
            self.assertEqual(result["typical_issues_by_check"]["field_semantic"], "First vague desc")

    def test_per_logstore_summary_sorted(self):
        """Per-logstore summary is sorted by issue count descending."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "sorted_project"
            
            # Create audit results with different issue counts
            for ls_name, issue_count in [("few_issues", 1), ("many_issues", 5), ("medium_issues", 3)]:
                audit_dir = project_dir / "_audit" / ls_name
                audit_dir.mkdir(parents=True)

                issues = [{"field": f"f{i}", "severity": "WARN"} for i in range(issue_count)]
                audit_result = _create_audit_result(ls_name, [
                    {"check": "field_semantic", "issues": issues},
                ])
                (audit_dir / "audit_result.json").write_text(
                    json.dumps(audit_result, ensure_ascii=False)
                )

            result = aggregate_audits(str(project_dir))

            # Should be sorted by issue count descending
            summary = result["per_logstore_summary"]
            self.assertEqual(summary[0]["name"], "many_issues")
            self.assertEqual(summary[1]["name"], "medium_issues")
            self.assertEqual(summary[2]["name"], "few_issues")


class TestGenerateSummaryMd(unittest.TestCase):
    """Test markdown summary generation."""

    def test_summary_contains_overview(self):
        """Generated summary contains overview table."""
        audit_data = {
            "project": "test_project",
            "logstores_audited": 5,
            "total_issues": 10,
            "issues_by_severity": {"ERROR": 3, "WARN": 7},
            "issues_by_check": {"field_semantic": 6, "annotation_semantic": 4},
            "typical_issues_by_check": {"field_semantic": "example", "annotation_semantic": "example"},
            "per_logstore_summary": [{"name": "ls1", "total_issues": 5, "by_severity": {"ERROR": 2, "WARN": 3}}],
        }

        md = generate_summary_md(audit_data, "/tmp/test")

        self.assertIn("# 审计报告：test_project", md)
        self.assertIn("| 审计 logstore 数 | 5 |", md)
        self.assertIn("| 发现问题总数 | 10 |", md)
        self.assertIn("| ERROR 级别 | 3 |", md)
        self.assertIn("| WARN 级别 | 7 |", md)

    def test_summary_contains_check_distribution(self):
        """Generated summary contains issue distribution by check."""
        audit_data = {
            "project": "test",
            "logstores_audited": 1,
            "total_issues": 10,
            "issues_by_severity": {},
            "issues_by_check": {"field_semantic": 6, "annotation_semantic": 4},
            "typical_issues_by_check": {},
            "per_logstore_summary": [],
        }

        md = generate_summary_md(audit_data, "/tmp/test")

        self.assertIn("field_semantic", md)
        self.assertIn("annotation_semantic", md)

    def test_summary_contains_typical_issues(self):
        """Generated summary contains typical issue examples per check."""
        audit_data = {
            "project": "test",
            "logstores_audited": 1,
            "total_issues": 10,
            "issues_by_severity": {},
            "issues_by_check": {"field_semantic": 5, "annotation_semantic": 3},
            "typical_issues_by_check": {
                "field_semantic": "VAGUE_DESC example",
                "annotation_semantic": "MISSING_CONTEXT example",
            },
            "per_logstore_summary": [],
        }

        md = generate_summary_md(audit_data, "/tmp/test")

        self.assertIn("典型问题示例", md)
        self.assertIn("field_semantic", md)
        self.assertIn("VAGUE_DESC example", md)


class TestNewFormat(unittest.TestCase):
    """Test new format audit_result.json (issues at top level)."""

    def test_new_format_aggregation(self):
        """New format audit_result.json is properly aggregated."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "new_format_project"
            audit_dir = project_dir / "_audit" / "test_logstore"
            audit_dir.mkdir(parents=True)

            audit_result = _create_audit_result_new_format("test_logstore", [
                {"check": "title_accuracy", "query_id": "q1", "severity": "WARN", "detail": "标题不准确"},
                {"check": "title_accuracy", "query_id": "q2", "severity": "ERROR", "detail": "标题缺失"},
                {"check": "category_reasonableness", "query_id": "q3", "severity": "WARN", "detail": "分类错误"},
            ])
            (audit_dir / "audit_result.json").write_text(
                json.dumps(audit_result, ensure_ascii=False)
            )

            result = aggregate_audits(str(project_dir))

            self.assertEqual(result["logstores_audited"], 1)
            self.assertEqual(result["total_issues"], 3)
            self.assertEqual(result["issues_by_check"]["title_accuracy"], 2)
            self.assertEqual(result["issues_by_check"]["category_reasonableness"], 1)
            self.assertEqual(result["issues_by_severity"]["WARN"], 2)
            self.assertEqual(result["issues_by_severity"]["ERROR"], 1)

    def test_filters_non_error_warn(self):
        """OK and INFO severity issues are filtered out."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "filter_project"
            audit_dir = project_dir / "_audit" / "test_logstore"
            audit_dir.mkdir(parents=True)

            audit_result = _create_audit_result_new_format("test_logstore", [
                {"check": "title_accuracy", "query_id": "q1", "severity": "WARN", "detail": "warn"},
                {"check": "title_accuracy", "query_id": "q2", "severity": "OK", "detail": "ok"},
                {"check": "title_accuracy", "query_id": "q3", "severity": "INFO", "detail": "info"},
            ])
            (audit_dir / "audit_result.json").write_text(
                json.dumps(audit_result, ensure_ascii=False)
            )

            result = aggregate_audits(str(project_dir))

            self.assertEqual(result["total_issues"], 1)
            self.assertEqual(result["issues_by_severity"]["WARN"], 1)
            self.assertNotIn("OK", result["issues_by_severity"])
            self.assertNotIn("INFO", result["issues_by_severity"])

    def test_backward_compatibility(self):
        """Old format with checks array still works."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "old_format_project"
            audit_dir = project_dir / "_audit" / "test_logstore"
            audit_dir.mkdir(parents=True)

            audit_result = _create_audit_result_old_format("test_logstore", [
                {
                    "check": "field_semantic",
                    "issues": [
                        {"field": "status", "issue": "too vague", "severity": "WARN"},
                    ],
                },
            ])
            (audit_dir / "audit_result.json").write_text(
                json.dumps(audit_result, ensure_ascii=False)
            )

            result = aggregate_audits(str(project_dir))

            self.assertEqual(result["logstores_audited"], 1)
            self.assertEqual(result["total_issues"], 1)
            self.assertEqual(result["issues_by_check"]["field_semantic"], 1)


class TestValidateResult(unittest.TestCase):
    """Test audit result validation."""

    def test_valid_result_no_warnings(self):
        """Valid result produces no warnings."""
        result = _create_audit_result_new_format("test_ls", [
            {"check": "title_accuracy", "severity": "WARN"},
            {"check": "title_accuracy", "severity": "ERROR"},
        ])
        
        warnings = validate_result(result, "test_ls")
        
        self.assertEqual(warnings, [])

    def test_total_issues_mismatch(self):
        """Mismatched total_issues produces warning."""
        result = _create_audit_result_new_format("test_ls", [
            {"check": "title_accuracy", "severity": "WARN"},
        ])
        # Manually corrupt the summary
        result["summary"]["total_issues"] = 999
        
        warnings = validate_result(result, "test_ls")
        
        self.assertEqual(len(warnings), 1)
        self.assertIn("total_issues mismatch", warnings[0])

    def test_severity_sum_mismatch(self):
        """Mismatched severity sum produces warning."""
        result = _create_audit_result_new_format("test_ls", [
            {"check": "title_accuracy", "severity": "WARN"},
        ])
        # Manually corrupt the severity breakdown
        result["summary"]["by_severity"]["ERROR"] = 100
        
        warnings = validate_result(result, "test_ls")
        
        self.assertEqual(len(warnings), 1)
        self.assertIn("severity sum mismatch", warnings[0])


if __name__ == "__main__":
    unittest.main()
