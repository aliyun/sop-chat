#!/usr/bin/env python3
"""
Unit tests for prepare_validation.py — derive_executable() and main() output.

All test data is synthetic. Tests cover:
  - <var;default> replacement
  - <var> replacement (bare and inside quotes)
  - SQL comparison operators not mismatched as placeholders
  - Chinese variable names (Unicode \\w)
  - No-placeholder passthrough
  - main() output schema and residual placeholder splitting

Run:
    python3 scripts/tests/test_prepare_validation.py
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from prepare_validation import derive_executable, main as prepare_main
from placeholder_re import RE_PLACEHOLDER


class TestDeriveExecutable(unittest.TestCase):
    """Tests for derive_executable()."""

    # --- <var;default> replacement ---

    def test_var_with_default_bare(self):
        """<var;default> outside quotes → replaced with default value."""
        self.assertEqual(
            derive_executable("status = <status;200>"),
            "status = 200",
        )

    def test_var_with_default_inside_quotes(self):
        """'<var;default>' inside quotes → quotes preserved, default injected."""
        self.assertEqual(
            derive_executable("hostname = '<hostname;node01>'"),
            "hostname = 'node01'",
        )

    def test_var_with_default_colon_in_name(self):
        """Variable name with colon (SLS tag field)."""
        self.assertEqual(
            derive_executable("<__tag__:__hostname__;worker-1>"),
            "worker-1",
        )

    def test_var_with_default_dot_in_name(self):
        """Variable name with dot."""
        self.assertEqual(
            derive_executable("latency > <config.threshold;500>"),
            "latency > 500",
        )

    def test_var_with_empty_default(self):
        """<var;> with empty default → replaced with empty string."""
        self.assertEqual(
            derive_executable("prefix = '<tag;>'"),
            "prefix = ''",
        )

    # --- <var> replacement (no default) ---

    def test_var_no_default_bare(self):
        """<var> without quotes → replaced with xxx."""
        self.assertEqual(
            derive_executable("msg has <queryId>"),
            "msg has xxx",
        )

    def test_var_no_default_inside_quotes(self):
        """'<var>' inside quotes → 'xxx', valid SQL."""
        self.assertEqual(
            derive_executable("hostname = '<hostname>'"),
            "hostname = 'xxx'",
        )

    def test_var_no_default_in_has_clause(self):
        """msg has '<var>' → msg has 'xxx'."""
        self.assertEqual(
            derive_executable("msg has '<queryId>'"),
            "msg has 'xxx'",
        )

    def test_multiple_vars(self):
        """Multiple <var> placeholders replaced independently."""
        self.assertEqual(
            derive_executable(
                "host = '<host>' and msg has '<keyword>'"
            ),
            "host = 'xxx' and msg has 'xxx'",
        )

    # --- Mixed <var;default> and <var> ---

    def test_mixed_with_and_without_default(self):
        """Both forms in the same query."""
        self.assertEqual(
            derive_executable(
                "status:<status;200> and project:<project>"
            ),
            "status:200 and project:xxx",
        )

    # --- SQL comparison operators must NOT be matched ---

    def test_sql_less_than_not_matched(self):
        """SQL 'allocated<avg' — not a placeholder."""
        q = "(allocated<avg or flag like 'true') con1"
        self.assertEqual(derive_executable(q), q)

    def test_sql_greater_than_not_matched(self):
        """SQL 'freeMem/31.0>0.1' — not a placeholder."""
        q = "(freeMem/31.0>0.1) con2"
        self.assertEqual(derive_executable(q), q)

    def test_sql_comparison_full_context(self):
        """The exact pattern from q12 that caused the false positive."""
        q = (
            "(allocated<avg or isNodeGreen like 'true') con1, "
            "(freeMem/31.0>0.1) con2"
        )
        self.assertEqual(derive_executable(q), q)

    def test_sql_angle_brackets_with_spaces(self):
        """SQL with spaces around < > operators."""
        q = "x < 100 and y > 200"
        self.assertEqual(derive_executable(q), q)

    # --- Chinese variable names (Unicode \\w) ---

    def test_chinese_var_with_default(self):
        r"""Chinese variable name: \w matches Unicode in Python."""
        self.assertEqual(
            derive_executable("<任务起始时间;1700000000>"),
            "1700000000",
        )

    def test_chinese_var_without_default(self):
        """Chinese variable name without default."""
        self.assertEqual(
            derive_executable("<目标项目>"),
            "xxx",
        )

    # --- Passthrough (no placeholders) ---

    def test_no_placeholder_passthrough(self):
        """Query without any placeholders returned unchanged."""
        q = "* | select count(1) from log where level = 'ERROR'"
        self.assertEqual(derive_executable(q), q)

    def test_empty_string(self):
        """Empty string returns empty string."""
        self.assertEqual(derive_executable(""), "")


class TestREPlaceholder(unittest.TestCase):
    """Verify the RE_PLACEHOLDER regex pattern directly."""

    def test_matches_simple_var(self):
        m = RE_PLACEHOLDER.search("<hostname>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "hostname")
        self.assertIsNone(m.group(2))

    def test_matches_var_with_default(self):
        m = RE_PLACEHOLDER.search("<status;200>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "status")
        self.assertEqual(m.group(2), "200")

    def test_matches_colon_dot_var(self):
        m = RE_PLACEHOLDER.search("<__tag__:__hostname__.sub;val>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "__tag__:__hostname__.sub")
        self.assertEqual(m.group(2), "val")

    def test_no_match_sql_comparison(self):
        """SQL comparison 'allocated<avg' should not match."""
        m = RE_PLACEHOLDER.search("allocated<avg or flag>")
        self.assertIsNone(m)

    def test_no_match_long_content(self):
        """Content with spaces between < > should not match."""
        m = RE_PLACEHOLDER.search("<this is not a var>")
        self.assertIsNone(m)


def _write_json(path, data):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def _read_json(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


class TestMainOutputSchema(unittest.TestCase):
    """Tests for main() output file schema and residual splitting."""

    EXPECTED_FIELDS = {"id", "title", "source_type", "dashboard_name", "source", "executable_query"}

    def _run_main(self, pipeline):
        """Run main() with a temp directory and return (main_entries, llm_entries_or_None)."""
        base = tempfile.mkdtemp()
        parsed = os.path.join(base, "parsed")
        os.makedirs(parsed)
        _write_json(os.path.join(parsed, "query_pipeline.json"), pipeline)

        old_argv = sys.argv
        sys.argv = ["prepare_validation.py", base]
        try:
            prepare_main()
        finally:
            sys.argv = old_argv

        main_path = os.path.join(parsed, "query_validation.json")
        llm_path = os.path.join(parsed, "query_validation_LLM.json")

        main_entries = _read_json(main_path)
        llm_entries = _read_json(llm_path) if os.path.exists(llm_path) else None
        return main_entries, llm_entries

    def test_output_schema_no_extra_fields(self):
        """Output entries contain exactly the expected fields."""
        pipeline = {
            "selected": [
                {"id": "q0", "display_name": "Test", "query": "* | SELECT 1",
                 "normalized_query": "* | SELECT 1",
                 "dashboard_name": "D", "source": "s.json", "source_type": "alert"},
            ],
            "extra": [],
        }
        main_entries, llm_entries = self._run_main(pipeline)
        self.assertEqual(len(main_entries), 1)
        self.assertEqual(set(main_entries[0].keys()), self.EXPECTED_FIELDS)
        self.assertIsNone(llm_entries)

    def test_no_query_or_normalized_query_fields(self):
        """Output must NOT contain 'query' or 'normalized_query'."""
        pipeline = {
            "selected": [
                {"id": "q0", "display_name": "Test", "query": "raw query",
                 "normalized_query": "norm query",
                 "dashboard_name": "D", "source": "s.json", "source_type": "alert"},
            ],
            "extra": [],
        }
        main_entries, _ = self._run_main(pipeline)
        self.assertNotIn("query", main_entries[0])
        self.assertNotIn("normalized_query", main_entries[0])

    def test_standard_placeholder_goes_to_main(self):
        """Entries with standard <var> placeholders go to query_validation.json."""
        pipeline = {
            "selected": [
                {"id": "q0", "display_name": "T", "query": "ProjectName: <用户project>",
                 "normalized_query": "ProjectName: <用户project>",
                 "dashboard_name": "", "source": "", "source_type": "reference"},
            ],
            "extra": [],
        }
        main_entries, llm_entries = self._run_main(pipeline)
        self.assertEqual(len(main_entries), 1)
        self.assertEqual(main_entries[0]["executable_query"], "ProjectName: xxx")
        self.assertIsNone(llm_entries)

    def test_residual_placeholder_goes_to_llm(self):
        """Entries with non-standard <...> placeholders go to query_validation_LLM.json."""
        pipeline = {
            "selected": [
                {"id": "r17", "display_name": "错误排查",
                 "query": "Method: <对应的Method, 写入一般是PostLogStoreLogs>",
                 "normalized_query": "Method: <对应的Method, 写入一般是PostLogStoreLogs>",
                 "dashboard_name": "错误", "source": "ref.yaml", "source_type": "reference"},
            ],
            "extra": [],
        }
        main_entries, llm_entries = self._run_main(pipeline)
        self.assertEqual(len(main_entries), 0)
        self.assertIsNotNone(llm_entries)
        self.assertEqual(len(llm_entries), 1)
        self.assertEqual(llm_entries[0]["id"], "r17")
        self.assertIn("<对应的Method", llm_entries[0]["executable_query"])

    def test_mixed_standard_and_residual(self):
        """Standard and residual entries split correctly."""
        pipeline = {
            "selected": [
                {"id": "q0", "display_name": "Normal",
                 "query": "host: <host>", "normalized_query": "host: <host>",
                 "dashboard_name": "", "source": "", "source_type": "alert"},
                {"id": "r17", "display_name": "Residual",
                 "query": "Method: <写入的Method, 逗号分隔>",
                 "normalized_query": "Method: <写入的Method, 逗号分隔>",
                 "dashboard_name": "", "source": "", "source_type": "reference"},
            ],
            "extra": [],
        }
        main_entries, llm_entries = self._run_main(pipeline)
        self.assertEqual(len(main_entries), 1)
        self.assertEqual(main_entries[0]["id"], "q0")
        self.assertIsNotNone(llm_entries)
        self.assertEqual(len(llm_entries), 1)
        self.assertEqual(llm_entries[0]["id"], "r17")


if __name__ == "__main__":
    unittest.main()
