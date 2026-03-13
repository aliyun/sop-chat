#!/usr/bin/env python3
"""
Unit tests for render_queries.py — focuses on PRE_CLEANED sentinel expansion
and rendering correctness.

Run:
    python3 scripts/tests/test_render_queries.py
"""

import json
import os
import sys
import tempfile
import unittest

# Add parent dir (scripts/) to path for imports
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from render_queries import (
    render_queries_md, render_selected_md, render_report,
    extract_common_values,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_input_dir():
    """Create a temp input dir with parsed/ and fragments/ subdirs."""
    base = tempfile.mkdtemp()
    os.makedirs(os.path.join(base, "parsed"))
    os.makedirs(os.path.join(base, "fragments"))
    return base


def _write_json(path, data):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


# ===========================================================================
# TestRenderQueriesMd
# ===========================================================================

class TestRenderQueriesMd(unittest.TestCase):
    """Tests for render_queries_md()."""

    def test_basic_render(self):
        """Basic rendering with title, category, cleaned_query."""
        entries = [
            {"title": "延迟分析", "category": "性能",
             "cleaned_query": "* | SELECT avg(latency)"},
        ]
        md = render_queries_md(entries, "## Test")
        self.assertIn("延迟分析", md)
        self.assertIn("* | SELECT avg(latency)", md)
        self.assertIn("### 性能", md)

    def test_multiple_categories(self):
        """Entries grouped by category in order."""
        entries = [
            {"title": "Q1", "category": "性能", "cleaned_query": "SELECT 1"},
            {"title": "Q2", "category": "错误", "cleaned_query": "SELECT 2"},
            {"title": "Q3", "category": "性能", "cleaned_query": "SELECT 3"},
        ]
        md = render_queries_md(entries, "## Test")
        perf_pos = md.index("### 性能")
        err_pos = md.index("### 错误")
        self.assertLess(perf_pos, err_pos)
        # Q1 and Q3 should both be under 性能
        self.assertEqual(md.count("### 性能"), 1)

    def test_source_type_reference_uses_reserved_fence(self):
        """source_type == 'reference' uses ```query reserved fence; others use ```query."""
        entries = [
            {"title": "Ref", "category": "C", "cleaned_query": "ref query", "source_type": "reference"},
            {"title": "Normal", "category": "C", "cleaned_query": "normal query"},
        ]
        md = render_queries_md(entries, "## Test")
        self.assertIn("```query reserved", md)
        self.assertIn("ref query", md)
        # Non-reference entry uses plain ```query (not reserved)
        self.assertIn("```query\nnormal query", md)

    def test_placeholder_uses_tpl_query_fence(self):
        """cleaned_query with <var> placeholder uses ```tpl_query fence."""
        entries = [
            {"title": "模板查询", "category": "C",
             "cleaned_query": "ProjectName: <目标project> | SELECT count(1)"},
        ]
        md = render_queries_md(entries, "## Test")
        self.assertIn("```tpl_query", md)
        self.assertIn("ProjectName: <目标project> | SELECT count(1)", md)

    def test_placeholder_with_reference_uses_tpl_query_reserved(self):
        """Placeholder + source_type reference → tpl_query reserved."""
        entries = [
            {"title": "参考模板", "category": "C",
             "cleaned_query": "ProjectName: <用户的project名称>",
             "source_type": "reference"},
        ]
        md = render_queries_md(entries, "## Test")
        self.assertIn("```tpl_query reserved", md)

    def test_no_placeholder_keeps_query_fence(self):
        """cleaned_query without placeholder keeps ```query fence."""
        entries = [
            {"title": "普通查询", "category": "C",
             "cleaned_query": "* | SELECT count(1)"},
        ]
        md = render_queries_md(entries, "## Test")
        self.assertIn("```query\n* | SELECT count(1)", md)
        self.assertNotIn("```tpl_query", md)

    def test_sql_comparison_not_mistaken_as_placeholder(self):
        """SQL comparison operators (x < 100 and y > 200) must not use tpl_query."""
        entries = [
            {"title": "比较查询", "category": "C",
             "cleaned_query": "x < 100 and y > 200 | SELECT 1"},
        ]
        md = render_queries_md(entries, "## Test")
        self.assertIn("```query\nx < 100 and y > 200", md)
        self.assertNotIn("```tpl_query", md)

    def test_fallback_to_query_when_cleaned_query_missing(self):
        """When cleaned_query missing, use query; placeholder in query → tpl_query."""
        entries = [
            {"title": "Fallback", "category": "C",
             "query": "host:<host> | SELECT 1"},
        ]
        md = render_queries_md(entries, "## Test")
        self.assertIn("```tpl_query", md)
        self.assertIn("host:<host> | SELECT 1", md)


# ===========================================================================
# TestNoChangeExpansion
# ===========================================================================

class TestNoChangeExpansion(unittest.TestCase):
    """Tests for PRE_CLEANED sentinel expansion in main() flow.

    Since main() reads files and writes output, we test the expansion
    logic by simulating the merge + expand steps.
    """

    SENTINEL = "PRE_CLEANED"

    def _simulate_merge_and_expand(self, pipeline, annotations):
        """Simulate the merge + PRE_CLEANED expansion from render_queries.main()."""
        selected = pipeline.get("selected", [])
        extra = pipeline.get("extra", [])

        # Merge annotations
        if annotations is not None:
            ann_map = {a["id"]: a for a in annotations if "id" in a}
            for entry in selected + extra:
                eid = entry.get("id", "")
                if eid in ann_map:
                    ann = ann_map[eid]
                    for field in ("title", "category", "cleaned_query"):
                        if field in ann:
                            entry[field] = ann[field]

        # Expand PRE_CLEANED
        expanded = 0
        for entry in selected + extra:
            if entry.get("cleaned_query") == self.SENTINEL:
                entry["cleaned_query"] = entry.get(
                    "pre_cleaned_query",
                    entry.get("normalized_query", ""))
                expanded += 1

        return selected, extra, expanded

    def test_no_change_expands_to_pre_cleaned(self):
        """PRE_CLEANED is expanded to pre_cleaned_query."""
        pipeline = {
            "selected": [
                {"id": "q-1", "query": "raw",
                 "pre_cleaned_query": "* | SELECT count(1)",
                 "normalized_query": "* | SELECT count(1)"},
            ],
            "extra": [],
        }
        annotations = [
            {"id": "q-1", "title": "计数", "category": "统计",
             "cleaned_query": "PRE_CLEANED"},
        ]
        selected, extra, expanded = self._simulate_merge_and_expand(pipeline, annotations)
        self.assertEqual(expanded, 1)
        self.assertEqual(selected[0]["cleaned_query"], "* | SELECT count(1)")

    def test_no_change_fallback_to_normalized(self):
        """PRE_CLEANED falls back to normalized_query when pre_cleaned_query absent."""
        pipeline = {
            "selected": [
                {"id": "q-1", "query": "raw",
                 "normalized_query": "status:200 | SELECT 1"},
            ],
            "extra": [],
        }
        annotations = [
            {"id": "q-1", "title": "状态查询", "category": "监控",
             "cleaned_query": "PRE_CLEANED"},
        ]
        selected, extra, expanded = self._simulate_merge_and_expand(pipeline, annotations)
        self.assertEqual(expanded, 1)
        self.assertEqual(selected[0]["cleaned_query"], "status:200 | SELECT 1")

    def test_non_sentinel_not_expanded(self):
        """Regular cleaned_query (not PRE_CLEANED) is kept as-is."""
        pipeline = {
            "selected": [
                {"id": "q-1", "query": "raw",
                 "pre_cleaned_query": "* | SELECT count(1)"},
            ],
            "extra": [],
        }
        annotations = [
            {"id": "q-1", "title": "自定义", "category": "其他",
             "cleaned_query": "* | SELECT count(1) WHERE <主机名> = 'web'"},
        ]
        selected, extra, expanded = self._simulate_merge_and_expand(pipeline, annotations)
        self.assertEqual(expanded, 0)
        self.assertEqual(selected[0]["cleaned_query"],
                         "* | SELECT count(1) WHERE <主机名> = 'web'")

    def test_mixed_sentinel_and_regular(self):
        """Mix of PRE_CLEANED and regular cleaned_query entries."""
        pipeline = {
            "selected": [
                {"id": "q-1", "query": "raw1",
                 "pre_cleaned_query": "SELECT 1"},
                {"id": "q-2", "query": "raw2",
                 "pre_cleaned_query": "SELECT 2"},
            ],
            "extra": [
                {"id": "q-3", "query": "raw3",
                 "pre_cleaned_query": "SELECT 3"},
            ],
        }
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "PRE_CLEANED"},
            {"id": "q-2", "title": "T2", "category": "C",
             "cleaned_query": "SELECT 2 WHERE <项目名> = 'test'"},
            {"id": "q-3", "title": "T3", "category": "C",
             "cleaned_query": "PRE_CLEANED"},
        ]
        selected, extra, expanded = self._simulate_merge_and_expand(pipeline, annotations)
        self.assertEqual(expanded, 2)
        self.assertEqual(selected[0]["cleaned_query"], "SELECT 1")
        self.assertEqual(selected[1]["cleaned_query"],
                         "SELECT 2 WHERE <项目名> = 'test'")
        self.assertEqual(extra[0]["cleaned_query"], "SELECT 3")

    def test_no_change_empty_fallback(self):
        """PRE_CLEANED with no pre_cleaned_query and no normalized_query gives empty string."""
        pipeline = {
            "selected": [
                {"id": "q-1", "query": "raw"},
            ],
            "extra": [],
        }
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "PRE_CLEANED"},
        ]
        selected, extra, expanded = self._simulate_merge_and_expand(pipeline, annotations)
        self.assertEqual(expanded, 1)
        self.assertEqual(selected[0]["cleaned_query"], "")

    def test_expanded_query_renders_correctly(self):
        """After PRE_CLEANED expansion, rendered markdown contains the correct query."""
        pipeline = {
            "selected": [
                {"id": "q-1", "query": "raw",
                 "pre_cleaned_query": "* | SELECT avg(<延迟>) GROUP BY <方法>"},
            ],
            "extra": [],
        }
        annotations = [
            {"id": "q-1", "title": "延迟分析", "category": "性能",
             "cleaned_query": "PRE_CLEANED"},
        ]
        selected, extra, expanded = self._simulate_merge_and_expand(pipeline, annotations)
        md = render_selected_md(selected)
        self.assertIn("* | SELECT avg(<延迟>) GROUP BY <方法>", md)
        self.assertIn("延迟分析", md)


# ===========================================================================
# TestRenderReportValidation
# ===========================================================================

class TestRenderReportValidation(unittest.TestCase):
    """Tests for render_report() reading metadata from validation files."""

    def _make_env(self, selected, extra=None, validation_result=None,
                  prepare_summary=None, reference_queries=None):
        """Create temp dir with required files and return (pipeline, input_dir)."""
        base = _make_input_dir()
        parsed = os.path.join(base, "parsed")

        pipeline = {
            "selected": selected,
            "extra": extra or [],
            "stats": {
                "input": len(selected) + len(extra or []),
                "selected": len(selected),
                "extra": len(extra or []),
            },
        }
        _write_json(os.path.join(parsed, "query_pipeline.json"), pipeline)

        if prepare_summary:
            _write_json(os.path.join(parsed, "prepare_summary.json"), prepare_summary)

        if reference_queries is not None:
            _write_json(os.path.join(parsed, "reference_queries.json"), reference_queries)

        return pipeline, base

    def test_failure_table_reads_metadata_from_validation(self):
        """Failure table uses source_type/dashboard_name/title/source from validation entries."""
        selected = [
            {"id": "q0", "display_name": "Good query", "source_type": "alert",
             "dashboard_name": "Monitor", "source": "alert.json",
             "title": "正常查询", "category": "监控",
             "cleaned_query": "* | SELECT 1"},
        ]
        validation_result = [
            {"id": "q0", "title": "正常查询", "source_type": "alert",
             "dashboard_name": "Monitor", "source": "alert.json",
             "executable_query": "* | SELECT 1", "pass": True, "error": ""},
            {"id": "r17", "title": "错误排查", "source_type": "reference",
             "dashboard_name": "错误信息排查", "source": "sls_op.yaml",
             "executable_query": "Method: xxx", "pass": False,
             "error": "syntax error near Method"},
        ]

        pipeline, input_dir = self._make_env(selected)
        report = render_report(pipeline, input_dir, validation_result, None)

        self.assertIn("验证失败清单", report)
        self.assertIn("r17", report)
        self.assertIn("reference", report)
        self.assertIn("错误信息排查", report)
        self.assertIn("错误排查", report)
        self.assertIn("sls_op.yaml", report)
        self.assertIn("syntax error near Method", report)

    def test_no_failures_no_table(self):
        """When all queries pass, no failure table is rendered."""
        selected = [
            {"id": "q0", "display_name": "Q", "source_type": "alert",
             "dashboard_name": "M", "source": "a.json",
             "title": "T", "category": "C", "cleaned_query": "SELECT 1"},
        ]
        validation_result = [
            {"id": "q0", "title": "T", "source_type": "alert",
             "dashboard_name": "M", "source": "a.json",
             "executable_query": "SELECT 1", "pass": True, "error": ""},
        ]

        pipeline, input_dir = self._make_env(selected)
        report = render_report(pipeline, input_dir, validation_result, None)

        self.assertNotIn("验证失败清单", report)

    def test_no_validation_result(self):
        """When validation_result is None, no validation row or failure table."""
        selected = [
            {"id": "q0", "display_name": "Q", "source_type": "alert",
             "dashboard_name": "M", "source": "a.json",
             "title": "T", "category": "C", "cleaned_query": "SELECT 1"},
        ]

        pipeline, input_dir = self._make_env(selected)
        report = render_report(pipeline, input_dir, None, None)

        self.assertNotIn("验证失败清单", report)
        self.assertNotIn("通过", report)


# ===========================================================================
# TestExtractCommonValuesCredentialFilter
# ===========================================================================

class TestExtractCommonValuesCredentialFilter(unittest.TestCase):
    """Tests for credential filtering in extract_common_values()."""

    def _extract(self, queries, fields=None):
        return extract_common_values(queries, fields or [])

    def test_accesskeyid_field_filtered(self):
        """Field named AccessKeyId is blocked regardless of value."""
        queries = [{"query": "AccessKeyId:LTAI4FxxxxxxxxxxVxtt"}]
        result = self._extract(queries)
        self.assertNotIn("AccessKeyId", result)

    def test_credential_field_variants_filtered(self):
        """Various credential field names are all blocked."""
        for field_name in ("Password", "secretkey", "Token",
                           "SecurityToken", "AK", "Credential"):
            queries = [{"query": f"{field_name}:placeholder"}]
            result = self._extract(queries)
            self.assertNotIn(field_name, result,
                             f"Field '{field_name}' should be filtered")

    def test_ltai_value_filtered_regardless_of_field(self):
        """LTAI* value pattern is blocked even under a non-credential field."""
        queries = [{"query": "SomeField:LTAI5tXxYyZzAaBbCcDdE"}]
        result = self._extract(queries)
        values = result.get("SomeField", [])
        self.assertTrue(
            all("LTAI" not in v for v in values),
            "LTAI* values should be filtered",
        )

    def test_normal_fields_not_affected(self):
        """Non-credential fields like VpcId, Method, Owner pass through."""
        queries = [
            {"query": "VpcId:vpc-123abc Method:GetLogStoreLogs Owner:admin"},
        ]
        result = self._extract(queries)
        self.assertIn("Method", result)
        self.assertIn("Owner", result)

    def test_tokentype_not_filtered(self):
        """Field named TokenType is NOT a credential field — should pass."""
        queries = [{"query": "TokenType:bearer"}]
        result = self._extract(queries)
        self.assertIn("TokenType", result)

    def test_short_ltai_not_filtered(self):
        """LTAI followed by < 12 chars should not be filtered."""
        queries = [{"query": "SomeField:LTAIshort"}]
        result = self._extract(queries)
        self.assertIn("SomeField", result)
        self.assertIn("LTAIshort", result["SomeField"])


if __name__ == "__main__":
    unittest.main()
