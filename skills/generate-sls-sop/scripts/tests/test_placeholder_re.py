#!/usr/bin/env python3
"""
Unit tests for placeholder_re.py — shared regex patterns.

Covers:
  - RE_PLACEHOLDER: matches <var> and <var;default>, rejects SQL operators
  - RE_WITH_DEFAULT: only matches <var;default>
  - RE_SEMICOLON_DEFAULT: detects <var;default> presence (no capture groups)

Run:
    python3 scripts/tests/test_placeholder_re.py
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from placeholder_re import RE_PLACEHOLDER, RE_WITH_DEFAULT, RE_SEMICOLON_DEFAULT


class TestREPlaceholder(unittest.TestCase):
    """Tests for RE_PLACEHOLDER — <var> or <var;default>."""

    def test_simple_var(self):
        m = RE_PLACEHOLDER.search("<hostname>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "hostname")
        self.assertIsNone(m.group(2))

    def test_var_with_default(self):
        m = RE_PLACEHOLDER.search("<status;200>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "status")
        self.assertEqual(m.group(2), "200")

    def test_colon_dot_var(self):
        m = RE_PLACEHOLDER.search("<__tag__:__hostname__.sub;val>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "__tag__:__hostname__.sub")
        self.assertEqual(m.group(2), "val")

    def test_chinese_var(self):
        m = RE_PLACEHOLDER.search("<任务起始时间;1700000000>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "任务起始时间")
        self.assertEqual(m.group(2), "1700000000")

    def test_chinese_var_no_default(self):
        m = RE_PLACEHOLDER.search("<目标项目>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "目标项目")
        self.assertIsNone(m.group(2))

    def test_empty_default(self):
        m = RE_PLACEHOLDER.search("<tag;>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "tag")
        self.assertEqual(m.group(2), "")

    def test_regex_default(self):
        m = RE_PLACEHOLDER.search("<project;^.*>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "project")
        self.assertEqual(m.group(2), "^.*")

    def test_no_match_sql_comparison(self):
        m = RE_PLACEHOLDER.search("allocated<avg or flag>")
        self.assertIsNone(m)

    def test_no_match_spaces(self):
        m = RE_PLACEHOLDER.search("<this is not a var>")
        self.assertIsNone(m)

    def test_findall_multiple(self):
        matches = RE_PLACEHOLDER.findall("host:<host;*> and project:<project>")
        self.assertEqual(len(matches), 2)
        self.assertEqual(matches[0], ("host", "*"))
        self.assertEqual(matches[1], ("project", ""))


class TestREWithDefault(unittest.TestCase):
    """Tests for RE_WITH_DEFAULT — only <var;default>."""

    def test_matches_with_default(self):
        m = RE_WITH_DEFAULT.search("<host;*>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "host")

    def test_matches_regex_default(self):
        m = RE_WITH_DEFAULT.search("<project;^.*>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "project")

    def test_matches_chinese_with_default(self):
        m = RE_WITH_DEFAULT.search("<任务起始时间;1700000000>")
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "任务起始时间")

    def test_no_match_without_default(self):
        m = RE_WITH_DEFAULT.search("<hostname>")
        self.assertIsNone(m)

    def test_no_match_sql(self):
        m = RE_WITH_DEFAULT.search("x < 100 and y > 200")
        self.assertIsNone(m)

    def test_sub_strips_default(self):
        result = RE_WITH_DEFAULT.sub(r'<\1>', "<host;*> and <project;^.*>")
        self.assertEqual(result, "<host> and <project>")

    def test_sub_preserves_bare_var(self):
        result = RE_WITH_DEFAULT.sub(r'<\1>', "<host> and <project;^.*>")
        self.assertEqual(result, "<host> and <project>")


class TestRESemicolonDefault(unittest.TestCase):
    """Tests for RE_SEMICOLON_DEFAULT — detection only (no capture groups)."""

    def test_detects_default(self):
        m = RE_SEMICOLON_DEFAULT.search("<host;*>")
        self.assertIsNotNone(m)

    def test_detects_long_default(self):
        m = RE_SEMICOLON_DEFAULT.search("<project;^.*>")
        self.assertIsNotNone(m)

    def test_no_match_empty_default(self):
        """Empty default (;>) has 0 chars after ;, minimum is 1."""
        m = RE_SEMICOLON_DEFAULT.search("<tag;>")
        self.assertIsNone(m)

    def test_no_match_bare_var(self):
        m = RE_SEMICOLON_DEFAULT.search("<hostname>")
        self.assertIsNone(m)

    def test_no_match_sql(self):
        m = RE_SEMICOLON_DEFAULT.search('x < a and y: ";" and z > c')
        self.assertIsNone(m)

    def test_findall_count(self):
        text = "<a;1> and <b> and <c;hello>"
        matches = RE_SEMICOLON_DEFAULT.findall(text)
        self.assertEqual(len(matches), 2)


if __name__ == "__main__":
    unittest.main()
