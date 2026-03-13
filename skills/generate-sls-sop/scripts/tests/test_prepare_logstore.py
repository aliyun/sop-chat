#!/usr/bin/env python3
"""
Unit tests for prepare_logstore.py.

Covers cross-logstore filtering: queries from alerts/dashboards that target
other logstores must be filtered out so queries.json only contains queries
for the current logstore.

Run:
    python3 scripts/tests/test_prepare_logstore.py
"""

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from prepare_logstore import prepare


def _minimal_index() -> dict:
    """Minimal index.json for prepare to succeed."""
    return {"keys": {"__time__": {"type": "long"}, "content": {"type": "text"}}}


def _alert_multi_logstore(logstore_a: str, logstore_b: str) -> dict:
    """Desensitized alert with queryList targeting multiple logstores."""
    return {
        "name": "alert-mock-001",
        "displayName": "Mock alert",
        "configuration": {
            "queryList": [
                {"query": "level:ERROR | select count(1)", "store": logstore_a},
                {"query": "level:FATAL | select count(1)", "store": logstore_b},
            ]
        },
    }


def _dashboard_multi_logstore(logstore_a: str, logstore_b: str) -> dict:
    """Desensitized dashboard with chartQueries targeting multiple logstores."""
    return {
        "displayName": "Mock dashboard",
        "charts": [
            {
                "search": {
                    "query": "",
                    "logstore": logstore_a,
                    "chartQueries": [
                        {"query": "query_a | select 1", "logstore": logstore_a},
                        {"query": "query_b | select 1", "logstore": logstore_b},
                    ],
                },
                "display": {"displayName": "Chart"},
            }
        ],
    }


class TestPrepareLogstoreCrossLogstoreFilter(unittest.TestCase):
    """Test that prepare filters out queries targeting other logstores."""

    def test_alert_cross_logstore_filtered(self):
        """Queries from alert queryList targeting other logstores are filtered out."""
        logstore_a = "logstore_a"
        logstore_b = "logstore_b"
        project = "project_mock"

        with tempfile.TemporaryDirectory() as tmp:
            ls_dir = Path(tmp) / project / logstore_a
            ls_dir.mkdir(parents=True)

            (ls_dir / "index.json").write_text(
                json.dumps(_minimal_index(), ensure_ascii=False, indent=2)
            )
            alerts_dir = ls_dir / "alerts"
            alerts_dir.mkdir()
            (alerts_dir / "alert-mock-001.json").write_text(
                json.dumps(_alert_multi_logstore(logstore_a, logstore_b), ensure_ascii=False, indent=2)
            )

            summary = prepare(str(ls_dir))

            queries_path = ls_dir / "parsed" / "queries.json"
            self.assertTrue(queries_path.exists(), "queries.json should exist")
            with open(queries_path) as f:
                queries = json.load(f)

            for q in queries:
                self.assertEqual(
                    q["logstore"],
                    logstore_a,
                    f"All queries must target {logstore_a}, got logstore={q['logstore']} in {q}",
                )

            self.assertEqual(len(queries), 1, "Only logstore_a query should remain")
            self.assertEqual(summary.get("queries_filtered_by_logstore", 0), 1)

    def test_dashboard_cross_logstore_filtered(self):
        """Queries from dashboard chartQueries targeting other logstores are filtered out."""
        logstore_a = "logstore_a"
        logstore_b = "logstore_b"
        project = "project_mock"

        with tempfile.TemporaryDirectory() as tmp:
            ls_dir = Path(tmp) / project / logstore_a
            ls_dir.mkdir(parents=True)

            (ls_dir / "index.json").write_text(
                json.dumps(_minimal_index(), ensure_ascii=False, indent=2)
            )
            dashboards_dir = ls_dir / "dashboards"
            dashboards_dir.mkdir()
            (dashboards_dir / "dashboard-mock.json").write_text(
                json.dumps(_dashboard_multi_logstore(logstore_a, logstore_b), ensure_ascii=False, indent=2)
            )

            summary = prepare(str(ls_dir))

            queries_path = ls_dir / "parsed" / "queries.json"
            self.assertTrue(queries_path.exists(), "queries.json should exist")
            with open(queries_path) as f:
                queries = json.load(f)

            for q in queries:
                self.assertEqual(
                    q["logstore"],
                    logstore_a,
                    f"All queries must target {logstore_a}, got logstore={q['logstore']} in {q}",
                )

            self.assertEqual(len(queries), 1, "Only logstore_a chartQuery should remain")
            self.assertEqual(summary.get("queries_filtered_by_logstore", 0), 1)

    def test_empty_logstore_preserved(self):
        """Queries with empty logstore are preserved (compatibility for unannotated resources)."""
        logstore_a = "logstore_a"
        project = "project_mock"

        with tempfile.TemporaryDirectory() as tmp:
            ls_dir = Path(tmp) / project / logstore_a
            ls_dir.mkdir(parents=True)

            (ls_dir / "index.json").write_text(
                json.dumps(_minimal_index(), ensure_ascii=False, indent=2)
            )
            alerts_dir = ls_dir / "alerts"
            alerts_dir.mkdir()
            alert_with_empty = {
                "name": "alert-mock-002",
                "displayName": "Mock alert",
                "configuration": {
                    "queryList": [
                        {"query": "level:ERROR | select 1", "store": logstore_a},
                        {"query": "level:ERROR | select 2", "store": ""},
                    ]
                },
            }
            (alerts_dir / "alert-mock-002.json").write_text(
                json.dumps(alert_with_empty, ensure_ascii=False, indent=2)
            )

            summary = prepare(str(ls_dir))

            queries_path = ls_dir / "parsed" / "queries.json"
            with open(queries_path) as f:
                queries = json.load(f)

            logstores = [q["logstore"] for q in queries]
            self.assertIn(logstore_a, logstores)
            self.assertIn("", logstores, "Empty logstore query should be preserved")

    def test_matching_logstore_all_kept(self):
        """Queries with logstore matching directory name are all kept."""
        logstore_a = "logstore_a"
        project = "project_mock"

        with tempfile.TemporaryDirectory() as tmp:
            ls_dir = Path(tmp) / project / logstore_a
            ls_dir.mkdir(parents=True)

            (ls_dir / "index.json").write_text(
                json.dumps(_minimal_index(), ensure_ascii=False, indent=2)
            )
            alerts_dir = ls_dir / "alerts"
            alerts_dir.mkdir()
            alert_same_logstore = {
                "name": "alert-mock-003",
                "displayName": "Mock alert",
                "configuration": {
                    "queryList": [
                        {"query": "q1", "store": logstore_a},
                        {"query": "q2", "store": logstore_a},
                    ]
                },
            }
            (alerts_dir / "alert-mock-003.json").write_text(
                json.dumps(alert_same_logstore, ensure_ascii=False, indent=2)
            )

            summary = prepare(str(ls_dir))

            queries_path = ls_dir / "parsed" / "queries.json"
            with open(queries_path) as f:
                queries = json.load(f)

            self.assertEqual(len(queries), 2)
            self.assertEqual(summary.get("queries_filtered_by_logstore", 0), 0)


if __name__ == "__main__":
    unittest.main()
