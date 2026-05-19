#!/usr/bin/env python3
"""
Unit tests for validate_step.py.

All test data is synthetic — no real project names, logstore names, or
dataset-specific content. Each test constructs temporary directories and
files, then calls the corresponding validate_* function directly.

Run:
    python3 scripts/tests/test_validate_step.py
"""

import json
import os
import sys
import tempfile
import unittest

# Add parent dir (scripts/) to path for imports
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from validate_step import validate_fields, validate_pipeline, validate_annotations


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_logstore_dir():
    """Create a temp logstore dir with parsed/ and fragments/ subdirs."""
    base = tempfile.mkdtemp()
    os.makedirs(os.path.join(base, "parsed"))
    os.makedirs(os.path.join(base, "fragments"))
    return base


def _write_json(path, data):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def _write_text(path, text):
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def _error_types(errors):
    """Extract check names from formatted error strings."""
    types = []
    for e in errors:
        # Format: [ERROR] check_name | location | detail
        parts = e.split(" | ")
        if parts:
            types.append(parts[0].replace("[ERROR] ", ""))
    return types


# ===========================================================================
# TestValidateFields
# ===========================================================================

class TestValidateFields(unittest.TestCase):
    """Tests for validate_fields() — after Step 5 (JSON annotations)."""

    def _make_fields_env(self, fields, annotations):
        """Create a temp logstore dir with fields.json + field_annotations.json."""
        d = _make_logstore_dir()
        _write_json(os.path.join(d, "parsed", "fields.json"), fields)
        _write_json(os.path.join(d, "parsed", "field_annotations.json"), annotations)
        return d

    def test_pass_normal(self):
        """Normal annotations with distinct descriptions passes."""
        fields = [
            {"field": "status", "alias": "状态码", "type": "long"},
            {"field": "method", "alias": "请求方法", "type": "text"},
            {"field": "latency", "alias": "延迟", "type": "double"},
        ]
        annotations = [
            {"field": "status", "desc": "HTTP 响应状态码"},
            {"field": "method", "desc": "HTTP 请求方法"},
            {"field": "latency", "desc": "请求处理耗时（毫秒）"},
        ]
        d = self._make_fields_env(fields, annotations)
        errors = validate_fields(d)
        self.assertEqual(len(errors), 0)

    def test_check1_missing_annotation(self):
        """Check 1: field with no annotation entry."""
        fields = [
            {"field": "status", "alias": "", "type": "long"},
            {"field": "body", "alias": "", "type": "text"},
        ]
        annotations = [
            {"field": "status", "desc": "HTTP 状态码"},
        ]
        d = self._make_fields_env(fields, annotations)
        errors = validate_fields(d)
        types = _error_types(errors)
        self.assertIn("missing_annotation", types)

    def test_check1_empty_desc(self):
        """Check 1: annotation exists but desc is empty."""
        fields = [{"field": "status", "alias": "", "type": "long"}]
        annotations = [{"field": "status", "desc": ""}]
        d = self._make_fields_env(fields, annotations)
        errors = validate_fields(d)
        types = _error_types(errors)
        self.assertIn("empty_desc", types)

    def test_check2_desc_is_type(self):
        """Check 2: desc equals the field's type value."""
        fields = [
            {"field": "count", "alias": "", "type": "long"},
            {"field": "rate", "alias": "", "type": "double"},
        ]
        annotations = [
            {"field": "count", "desc": "long"},
            {"field": "rate", "desc": "请求速率"},
        ]
        d = self._make_fields_env(fields, annotations)
        errors = validate_fields(d)
        types = _error_types(errors)
        self.assertEqual(types.count("desc_is_type"), 1)

    def test_check3_desc_equals_field_name(self):
        """Check 3: desc is identical to field name."""
        fields = [{"field": "APIVersion", "alias": "", "type": "text"}]
        annotations = [{"field": "APIVersion", "desc": "APIVersion"}]
        d = self._make_fields_env(fields, annotations)
        errors = validate_fields(d)
        types = _error_types(errors)
        self.assertIn("desc_equals_field_name", types)

    def test_check4_duplicate_descriptions(self):
        """Check 4: >30% of fields share the same description."""
        fields = [{"field": f"f{i}", "alias": "", "type": "text"} for i in range(10)]
        annotations = []
        for i in range(10):
            desc = "相同的描述" if i < 5 else f"独特描述{i}"
            annotations.append({"field": f"f{i}", "desc": desc})
        d = self._make_fields_env(fields, annotations)
        errors = validate_fields(d)
        types = _error_types(errors)
        self.assertIn("duplicate_description", types)

    def test_boundary_empty_fields(self):
        """Boundary: empty fields.json and annotations passes."""
        d = self._make_fields_env([], [])
        errors = validate_fields(d)
        self.assertEqual(len(errors), 0)

    def test_desc_not_type_when_semantic(self):
        """Semantic desc that differs from type passes Check 2."""
        fields = [{"field": "count", "alias": "", "type": "long"}]
        annotations = [{"field": "count", "desc": "请求次数"}]
        d = self._make_fields_env(fields, annotations)
        errors = validate_fields(d)
        types = _error_types(errors)
        self.assertNotIn("desc_is_type", types)


# ===========================================================================
# TestValidatePipeline
# ===========================================================================

class TestValidatePipeline(unittest.TestCase):
    """Tests for validate_pipeline() — after Step 6."""

    def _make_pipeline(self, n_selected=5, n_extra=10, dup_ids=None):
        """Helper to create a pipeline JSON with given counts."""
        d = _make_logstore_dir()
        selected = [{"id": f"sel-{i}", "query": f"q{i}"} for i in range(n_selected)]
        extra = [{"id": f"ext-{i}", "query": f"q{i}"} for i in range(n_extra)]
        if dup_ids:
            for eid in dup_ids:
                extra.append({"id": eid, "query": "dup"})
        _write_json(os.path.join(d, "parsed", "query_pipeline.json"),
                     {"selected": selected, "extra": extra})
        return d

    def test_pass_normal(self):
        """Normal pipeline with 5 selected and 10 extra passes."""
        d = self._make_pipeline(5, 10)
        errors = validate_pipeline(d)
        self.assertEqual(len(errors), 0)

    def test_check1_selected_exceeds_limit(self):
        """Check 1: selected count > 20 triggers error."""
        d = self._make_pipeline(25, 5)
        errors = validate_pipeline(d)
        types = _error_types(errors)
        self.assertIn("selected_limit", types)

    def test_check2_extra_exceeds_limit(self):
        """Check 2: extra count > 40 triggers error."""
        d = self._make_pipeline(5, 45)
        errors = validate_pipeline(d)
        types = _error_types(errors)
        self.assertIn("extra_limit", types)

    def test_check3_duplicate_ids(self):
        """Check 3: duplicate IDs across selected+extra triggers error."""
        d = _make_logstore_dir()
        selected = [
            {"id": "q-001", "query": "SELECT 1"},
            {"id": "q-002", "query": "SELECT 2"},
        ]
        extra = [
            {"id": "q-001", "query": "SELECT 1 dup"},
            {"id": "q-003", "query": "SELECT 3"},
        ]
        _write_json(os.path.join(d, "parsed", "query_pipeline.json"),
                     {"selected": selected, "extra": extra})
        errors = validate_pipeline(d)
        types = _error_types(errors)
        self.assertIn("duplicate_id", types)
        self.assertEqual(types.count("duplicate_id"), 1)

    def test_boundary_empty_selected_extra(self):
        """Boundary: empty selected and extra passes."""
        d = self._make_pipeline(0, 0)
        errors = validate_pipeline(d)
        self.assertEqual(len(errors), 0)


# ===========================================================================
# TestValidateAnnotations
# ===========================================================================

class TestValidateAnnotations(unittest.TestCase):
    """Tests for validate_annotations() — after Step 9."""

    def _make_annotation_env(self, annotations, selected=None, extra=None):
        """Helper to create pipeline + annotations JSON files."""
        d = _make_logstore_dir()
        if selected is None:
            selected = [{"id": a["id"], "query": "q"} for a in annotations
                        if "id" in a]
        if extra is None:
            extra = []
        _write_json(os.path.join(d, "parsed", "query_pipeline.json"),
                     {"selected": selected, "extra": extra})
        _write_json(os.path.join(d, "parsed", "query_annotations.json"),
                     annotations)
        return d

    def test_pass_normal(self):
        """Normal annotations with all required fields pass."""
        annotations = [
            {"id": "q-1", "title": "请求延迟分析", "category": "性能",
             "cleaned_query": "* | SELECT avg(latency) GROUP BY method"},
            {"id": "q-2", "title": "错误码统计", "category": "错误",
             "cleaned_query": "status >= 400 | SELECT count(1) GROUP BY status"},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        self.assertEqual(len(errors), 0)

    def test_check1_missing_required_fields(self):
        """Check 1: missing required fields detected."""
        annotations = [
            {"id": "q-1", "title": "测试"},  # missing category, cleaned_query
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("schema", types)

    def test_check2_count_mismatch(self):
        """Check 2: annotation count != pipeline count."""
        selected = [
            {"id": "q-1", "query": "q1"},
            {"id": "q-2", "query": "q2"},
        ]
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C", "cleaned_query": "q1"},
        ]
        d = self._make_annotation_env(annotations, selected=selected)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("count_mismatch", types)

    def test_check3_id_not_in_pipeline(self):
        """Check 3: annotation ID not found in pipeline."""
        selected = [{"id": "q-1", "query": "q1"}]
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C", "cleaned_query": "q1"},
            {"id": "q-999", "title": "T2", "category": "C", "cleaned_query": "q2"},
        ]
        d = self._make_annotation_env(annotations, selected=selected)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("id_not_in_pipeline", types)

    def test_check4_unstripped_default_detected(self):
        """Check 4: unstripped ;default placeholders in cleaned_query."""
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "project: <project;^.*> | SELECT count(1)"},
            {"id": "q-2", "title": "T2", "category": "C",
             "cleaned_query": "* | SELECT * WHERE region = '<region;cn-example>'"},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertEqual(types.count("unstripped_default"), 2)

    def test_check4_no_false_positive_sql_operators(self):
        """Check 4: SQL with < > and ; does NOT trigger false positive."""
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": 'x < a and y: ";" and z > c'},
            {"id": "q-2", "title": "T2", "category": "C",
             "cleaned_query": "latency < 1000 AND count > 5"},
            {"id": "q-3", "title": "T3", "category": "C",
             "cleaned_query": "* | SELECT CASE WHEN a < b THEN 'x;y' ELSE 'z' END"},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertNotIn("unstripped_default", types)

    def test_check4_stripped_placeholder_passes(self):
        """Check 4: properly stripped <var> (no ;default) passes."""
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "project: <目标project> | SELECT count(1)"},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertNotIn("unstripped_default", types)

    def test_check5_duplicate_titles(self):
        """Check 5: duplicate titles detected."""
        annotations = [
            {"id": "q-1", "title": "同名标题", "category": "C",
             "cleaned_query": "SELECT 1"},
            {"id": "q-2", "title": "同名标题", "category": "C",
             "cleaned_query": "SELECT 2"},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("duplicate_title", types)

    def test_check6_ak_leakage(self):
        """Check 6: AK leakage detected with synthetic key."""
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "accessKeyId: LTAI1234567890AB AND status: 200"},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("ak_leakage", types)

    def test_check7_empty_cleaned_query(self):
        """Check 7: empty cleaned_query detected."""
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C", "cleaned_query": ""},
            {"id": "q-2", "title": "T2", "category": "C", "cleaned_query": "   "},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertEqual(types.count("empty_cleaned_query"), 2)

    def test_check7_nonempty_query_passes(self):
        """Check 7: non-empty cleaned_query passes."""
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "SELECT 1"},
        ]
        d = self._make_annotation_env(annotations)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertNotIn("empty_cleaned_query", types)

    # ----- PRE_CLEANED sentinel tests -----

    def _make_annotation_env_with_pipeline(self, annotations, pipeline_entries):
        """Helper: create env with explicit pipeline entries (with pre_cleaned_query)."""
        d = _make_logstore_dir()
        selected = pipeline_entries
        _write_json(os.path.join(d, "parsed", "query_pipeline.json"),
                     {"selected": selected, "extra": []})
        _write_json(os.path.join(d, "parsed", "query_annotations.json"),
                     annotations)
        return d

    def test_no_change_sentinel_resolves_to_pre_cleaned(self):
        """PRE_CLEANED sentinel resolves to pre_cleaned_query for validation."""
        pipeline = [
            {"id": "q-1", "query": "original",
             "pre_cleaned_query": "* | SELECT count(1) FROM log",
             "normalized_query": "* | SELECT count(1) FROM log"},
        ]
        annotations = [
            {"id": "q-1", "title": "日志计数", "category": "统计",
             "cleaned_query": "PRE_CLEANED"},
        ]
        d = self._make_annotation_env_with_pipeline(annotations, pipeline)
        errors = validate_annotations(d)
        # Should pass — pre_cleaned_query is valid
        self.assertEqual(len(errors), 0)

    def test_no_change_sentinel_empty_pre_cleaned_triggers_empty(self):
        """PRE_CLEANED with empty pre_cleaned_query triggers empty_cleaned_query."""
        pipeline = [
            {"id": "q-1", "query": "original",
             "pre_cleaned_query": "",
             "normalized_query": ""},
        ]
        annotations = [
            {"id": "q-1", "title": "空查询", "category": "其他",
             "cleaned_query": "PRE_CLEANED"},
        ]
        d = self._make_annotation_env_with_pipeline(annotations, pipeline)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("empty_cleaned_query", types)

    def test_no_change_sentinel_fallback_to_normalized(self):
        """PRE_CLEANED falls back to normalized_query when pre_cleaned_query absent."""
        pipeline = [
            {"id": "q-1", "query": "original",
             "normalized_query": "status:200 | SELECT avg(latency)"},
        ]
        annotations = [
            {"id": "q-1", "title": "延迟分析", "category": "性能",
             "cleaned_query": "PRE_CLEANED"},
        ]
        d = self._make_annotation_env_with_pipeline(annotations, pipeline)
        errors = validate_annotations(d)
        self.assertEqual(len(errors), 0)

    def test_no_change_unstripped_default_in_pre_cleaned(self):
        """PRE_CLEANED: if pre_cleaned_query has unstripped ;default, it is flagged."""
        pipeline = [
            {"id": "q-1", "query": "original",
             "pre_cleaned_query": "project:<project;^.*> | SELECT 1"},
        ]
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "PRE_CLEANED"},
        ]
        d = self._make_annotation_env_with_pipeline(annotations, pipeline)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("unstripped_default", types)

    def test_no_change_ak_leakage_in_pre_cleaned(self):
        """PRE_CLEANED: AK leakage in pre_cleaned_query is still detected."""
        pipeline = [
            {"id": "q-1", "query": "original",
             "pre_cleaned_query": "accessKeyId: LTAI1234567890AB AND status: 200"},
        ]
        annotations = [
            {"id": "q-1", "title": "T1", "category": "C",
             "cleaned_query": "PRE_CLEANED"},
        ]
        d = self._make_annotation_env_with_pipeline(annotations, pipeline)
        errors = validate_annotations(d)
        types = _error_types(errors)
        self.assertIn("ak_leakage", types)

    def test_check8_redundant_override_no_error(self):
        """Check 8: redundant override is a WARNING, not an ERROR — no errors returned."""
        pipeline = [
            {"id": "q-1", "query": "original",
             "pre_cleaned_query": "SELECT count(1) FROM log"},
        ]
        annotations = [
            {"id": "q-1", "title": "日志计数", "category": "统计",
             "cleaned_query": "SELECT count(1) FROM log"},  # same as pre_cleaned_query
        ]
        d = self._make_annotation_env_with_pipeline(annotations, pipeline)
        errors = validate_annotations(d)
        # Check 8 is a WARNING, not an ERROR — should not appear in errors list
        self.assertEqual(len(errors), 0)

    def test_no_change_passes_all_checks(self):
        """PRE_CLEANED with clean pre_cleaned_query passes all validation checks."""
        pipeline = [
            {"id": "q-1", "query": "raw1",
             "pre_cleaned_query": "* | SELECT avg(<latency>) GROUP BY <method>",
             "normalized_query": "* | SELECT avg(<latency;0>) GROUP BY <method;GET>"},
            {"id": "q-2", "query": "raw2",
             "pre_cleaned_query": "status >= 400 | SELECT count(1)",
             "normalized_query": "status >= 400 | SELECT count(1)"},
        ]
        annotations = [
            {"id": "q-1", "title": "延迟分析", "category": "性能",
             "cleaned_query": "PRE_CLEANED"},
            {"id": "q-2", "title": "错误计数", "category": "错误",
             "cleaned_query": "PRE_CLEANED"},
        ]
        d = self._make_annotation_env_with_pipeline(annotations, pipeline)
        errors = validate_annotations(d)
        self.assertEqual(len(errors), 0)


if __name__ == "__main__":
    unittest.main()
