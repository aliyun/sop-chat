#!/usr/bin/env python3
"""
Unit tests for render_fields.py.

Run:
    python3 scripts/tests/test_render_fields.py
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from render_fields import render_fields_table


class TestRenderFieldsTable(unittest.TestCase):

    def test_basic_render(self):
        """Renders a simple 2-field table with descriptions."""
        fields = [
            {"field": "status", "alias": "状态码", "type": "long"},
            {"field": "method", "alias": "请求方法", "type": "text"},
        ]
        annotations = [
            {"field": "status", "desc": "HTTP 响应状态码"},
            {"field": "method", "desc": "HTTP 请求方法"},
        ]
        md = render_fields_table(fields, annotations)
        self.assertIn("| method | 请求方法 | HTTP 请求方法 |", md)
        self.assertIn("| status | 状态码 | HTTP 响应状态码 |", md)
        self.assertNotIn("(long)", md)
        self.assertNotIn("(text)", md)

    def test_missing_annotation_falls_back_to_type(self):
        """Fields without annotations fall back to (type) placeholder."""
        fields = [
            {"field": "status", "alias": "", "type": "long"},
            {"field": "body", "alias": "", "type": "text"},
        ]
        annotations = [
            {"field": "status", "desc": "HTTP 状态码"},
        ]
        md = render_fields_table(fields, annotations)
        self.assertIn("| status | - | HTTP 状态码 |", md)
        self.assertIn("| body | - | (text) |", md)

    def test_nested_fields_quoted_and_rules_shown(self):
        """Nested fields are quoted and nested-field rules are included."""
        fields = [
            {"field": "metric.cpu", "alias": "cpu", "type": "double", "parent": "metric"},
            {"field": "name", "alias": "", "type": "text"},
        ]
        annotations = [
            {"field": "metric.cpu", "desc": "CPU 使用率"},
            {"field": "name", "desc": "名称"},
        ]
        md = render_fields_table(fields, annotations)
        self.assertIn('"metric.cpu"', md)
        self.assertIn("嵌套字段引用规则", md)
        self.assertIn("| name | - | 名称 |", md)

    def test_no_nested_fields_no_rules(self):
        """No nested fields means no nested-field rules section."""
        fields = [{"field": "level", "alias": "", "type": "text"}]
        annotations = [{"field": "level", "desc": "日志级别"}]
        md = render_fields_table(fields, annotations)
        self.assertNotIn("嵌套字段引用规则", md)

    def test_sorted_alphabetically(self):
        """Fields are sorted alphabetically in output."""
        fields = [
            {"field": "zebra", "alias": "", "type": "text"},
            {"field": "alpha", "alias": "", "type": "text"},
        ]
        annotations = [
            {"field": "zebra", "desc": "Z 字段"},
            {"field": "alpha", "desc": "A 字段"},
        ]
        md = render_fields_table(fields, annotations)
        alpha_pos = md.index("alpha")
        zebra_pos = md.index("zebra")
        self.assertLess(alpha_pos, zebra_pos)

    def test_empty_fields(self):
        """Empty fields list produces header-only table."""
        md = render_fields_table([], [])
        self.assertIn("## 字段参考", md)
        self.assertIn("| 字段 | 别名 | 说明 |", md)

    def test_alias_dash_when_empty(self):
        """Empty alias renders as '-'."""
        fields = [{"field": "x", "alias": "", "type": "text"}]
        annotations = [{"field": "x", "desc": "测试"}]
        md = render_fields_table(fields, annotations)
        self.assertIn("| x | - | 测试 |", md)


if __name__ == "__main__":
    unittest.main()
