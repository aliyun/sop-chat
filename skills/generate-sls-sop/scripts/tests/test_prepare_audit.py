#!/usr/bin/env python3
"""
Unit tests for prepare_audit.py.

Covers scoring formula, sampling logic, and boundary conditions.

Run:
    python3 scripts/tests/test_prepare_audit.py
"""

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from prepare_audit import calculate_score, prepare_audit, build_audit_context


def _create_project_summary(logstores: list[dict]) -> dict:
    """Create project_summary.json content."""
    return {
        "logstores": logstores,
        "skipped": [],
        "errors": [],
    }


def _create_selected_logstores(
    names: list[str],
    output_path_template: str = "sop-docs/test/{name}/overview.md",
) -> dict:
    """Create selected_logstores.json content."""
    return {
        "logstores": [
            {
                "name": name,
                "output_path": output_path_template.format(name=name),
            }
            for name in names
        ],
    }


def _create_overview_file(project_dir: Path, output_path: str) -> None:
    """Create a stub overview.md so prepare_audit does not skip the logstore."""
    full_path = project_dir / output_path
    full_path.parent.mkdir(parents=True, exist_ok=True)
    full_path.write_text("# stub\n", encoding="utf-8")


class TestCalculateScore(unittest.TestCase):
    """Test the scoring formula for high-value logstore selection."""

    def test_max_score(self):
        """Logstore with high queries, high fields, alert, and reference gets max score."""
        info = {
            "deduped_queries_count": 100,  # > 50, capped at 1.0
            "fields_count": 50,            # > 30, capped at 1.0
            "deduped_source_dist": {"alert": 10},
            "has_reference": True,
        }
        score = calculate_score(info)
        # 0.4 * 1.0 + 0.3 * 1.0 + 0.2 * 1.0 + 0.1 * 1.0 = 1.0
        self.assertAlmostEqual(score, 1.0, places=4)

    def test_min_score(self):
        """Logstore with no queries, no fields, no alert, no reference gets 0."""
        info = {
            "deduped_queries_count": 0,
            "fields_count": 0,
            "deduped_source_dist": {},
            "has_reference": False,
        }
        score = calculate_score(info)
        self.assertAlmostEqual(score, 0.0, places=4)

    def test_partial_score(self):
        """Logstore with partial values gets weighted score."""
        info = {
            "deduped_queries_count": 25,   # 25/50 = 0.5
            "fields_count": 15,            # 15/30 = 0.5
            "deduped_source_dist": {"dashboard": 10},  # no alert
            "has_reference": False,
        }
        score = calculate_score(info)
        # 0.4 * 0.5 + 0.3 * 0.5 + 0.2 * 0 + 0.1 * 0 = 0.35
        self.assertAlmostEqual(score, 0.35, places=4)

    def test_alert_bonus(self):
        """Alert presence adds 0.2 to score."""
        base_info = {
            "deduped_queries_count": 0,
            "fields_count": 0,
            "deduped_source_dist": {},
            "has_reference": False,
        }
        with_alert = {
            **base_info,
            "deduped_source_dist": {"alert": 1},
        }
        base_score = calculate_score(base_info)
        alert_score = calculate_score(with_alert)
        self.assertAlmostEqual(alert_score - base_score, 0.2, places=4)


class TestPrepareAuditModes(unittest.TestCase):
    """Test audit mode selection based on project size."""

    def test_small_project_full_audit(self):
        """Projects with ≤10 logstores get full audit regardless of mode."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "small_project"
            project_dir.mkdir()

            names = [f"logstore_{i}" for i in range(5)]
            logstores = [
                {"name": name, "deduped_queries_count": 10, "fields_count": 5,
                 "deduped_source_dist": {}, "has_reference": False}
                for name in names
            ]
            selected = _create_selected_logstores(names)

            (project_dir / "project_summary.json").write_text(
                json.dumps(_create_project_summary(logstores), ensure_ascii=False)
            )
            (project_dir / "selected_logstores.json").write_text(
                json.dumps(selected, ensure_ascii=False)
            )
            for entry in selected["logstores"]:
                _create_overview_file(project_dir, entry["output_path"])
                # Create step_progress.json with status=completed to match new logic
                logstore_dir = project_dir / entry["name"]
                logstore_dir.mkdir(exist_ok=True)
                (logstore_dir / "step_progress.json").write_text(
                    json.dumps({"status": "completed"}, ensure_ascii=False)
                )

            audit_plan = prepare_audit(str(project_dir), "sample", None)

            self.assertEqual(audit_plan["mode"], "full")
            self.assertEqual(audit_plan["audit_count"], 5)
            self.assertEqual(len(audit_plan["audit_logstores"]), 5)

    def test_large_project_sample_audit(self):
        """Projects with >10 logstores sample 20% (min 10, max 30)."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "large_project"
            project_dir.mkdir()

            names = [f"logstore_{i}" for i in range(50)]
            logstores = [
                {"name": name, "deduped_queries_count": i * 2, "fields_count": i,
                 "deduped_source_dist": {"alert": 1} if i % 5 == 0 else {},
                 "has_reference": i % 10 == 0}
                for i, name in enumerate(names)
            ]
            selected = _create_selected_logstores(names)

            (project_dir / "project_summary.json").write_text(
                json.dumps(_create_project_summary(logstores), ensure_ascii=False)
            )
            (project_dir / "selected_logstores.json").write_text(
                json.dumps(selected, ensure_ascii=False)
            )
            for entry in selected["logstores"]:
                _create_overview_file(project_dir, entry["output_path"])
                # Create step_progress.json with status=completed to match new logic
                logstore_dir = project_dir / entry["name"]
                logstore_dir.mkdir(exist_ok=True)
                (logstore_dir / "step_progress.json").write_text(
                    json.dumps({"status": "completed"}, ensure_ascii=False)
                )

            audit_plan = prepare_audit(str(project_dir), "sample", None)

            self.assertEqual(audit_plan["mode"], "sample")
            self.assertEqual(audit_plan["audit_count"], 10)

    def test_very_large_project_max_30(self):
        """Projects with >150 logstores cap at 30 samples."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "huge_project"
            project_dir.mkdir()

            names = [f"logstore_{i}" for i in range(200)]
            logstores = [
                {"name": name, "deduped_queries_count": 10, "fields_count": 5,
                 "deduped_source_dist": {}, "has_reference": False}
                for name in names
            ]
            selected = _create_selected_logstores(names)

            (project_dir / "project_summary.json").write_text(
                json.dumps(_create_project_summary(logstores), ensure_ascii=False)
            )
            (project_dir / "selected_logstores.json").write_text(
                json.dumps(selected, ensure_ascii=False)
            )
            for entry in selected["logstores"]:
                _create_overview_file(project_dir, entry["output_path"])
                # Create step_progress.json with status=completed to match new logic
                logstore_dir = project_dir / entry["name"]
                logstore_dir.mkdir(exist_ok=True)
                (logstore_dir / "step_progress.json").write_text(
                    json.dumps({"status": "completed"}, ensure_ascii=False)
                )

            audit_plan = prepare_audit(str(project_dir), "sample", None)

            self.assertEqual(audit_plan["audit_count"], 30)

    def test_targeted_mode(self):
        """Targeted mode audits only specified logstores."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "targeted_project"
            project_dir.mkdir()

            names = [f"logstore_{i}" for i in range(20)]
            logstores = [
                {"name": name, "deduped_queries_count": 10, "fields_count": 5,
                 "deduped_source_dist": {}, "has_reference": False}
                for name in names
            ]
            selected = _create_selected_logstores(names)

            (project_dir / "project_summary.json").write_text(
                json.dumps(_create_project_summary(logstores), ensure_ascii=False)
            )
            (project_dir / "selected_logstores.json").write_text(
                json.dumps(selected, ensure_ascii=False)
            )
            for entry in selected["logstores"]:
                _create_overview_file(project_dir, entry["output_path"])
                # Create step_progress.json with status=completed to match new logic
                logstore_dir = project_dir / entry["name"]
                logstore_dir.mkdir(exist_ok=True)
                (logstore_dir / "step_progress.json").write_text(
                    json.dumps({"status": "completed"}, ensure_ascii=False)
                )

            audit_plan = prepare_audit(
                str(project_dir), "targeted", "logstore_0,logstore_5,logstore_10"
            )

            self.assertEqual(audit_plan["mode"], "targeted")
            self.assertEqual(audit_plan["audit_count"], 3)
            audit_names = [ls["name"] for ls in audit_plan["audit_logstores"]]
            self.assertIn("logstore_0", audit_names)
            self.assertIn("logstore_5", audit_names)
            self.assertIn("logstore_10", audit_names)

    def test_full_mode_explicit(self):
        """Explicit full mode audits all logstores even for large projects."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "full_project"
            project_dir.mkdir()

            names = [f"logstore_{i}" for i in range(20)]
            logstores = [
                {"name": name, "deduped_queries_count": 10, "fields_count": 5,
                 "deduped_source_dist": {}, "has_reference": False}
                for name in names
            ]
            selected = _create_selected_logstores(names)

            (project_dir / "project_summary.json").write_text(
                json.dumps(_create_project_summary(logstores), ensure_ascii=False)
            )
            (project_dir / "selected_logstores.json").write_text(
                json.dumps(selected, ensure_ascii=False)
            )
            for entry in selected["logstores"]:
                _create_overview_file(project_dir, entry["output_path"])
                # Create step_progress.json with status=completed to match new logic
                logstore_dir = project_dir / entry["name"]
                logstore_dir.mkdir(exist_ok=True)
                (logstore_dir / "step_progress.json").write_text(
                    json.dumps({"status": "completed"}, ensure_ascii=False)
                )

            audit_plan = prepare_audit(str(project_dir), "full", None)

            self.assertEqual(audit_plan["mode"], "full")
            self.assertEqual(audit_plan["audit_count"], 20)


class TestHighValueSorting(unittest.TestCase):
    """Test that sample mode selects highest-value logstores."""

    def test_high_value_logstores_selected_first(self):
        """Sample mode should select logstores with highest scores."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "sorted_project"
            project_dir.mkdir()

            logstores = [
                {"name": "high_value", "deduped_queries_count": 100, "fields_count": 50,
                 "deduped_source_dist": {"alert": 10}, "has_reference": True},
                {"name": "medium_value", "deduped_queries_count": 30, "fields_count": 20,
                 "deduped_source_dist": {"alert": 5}, "has_reference": False},
                {"name": "low_value", "deduped_queries_count": 5, "fields_count": 3,
                 "deduped_source_dist": {}, "has_reference": False},
            ]
            for i in range(15):
                logstores.append({
                    "name": f"filler_{i}", "deduped_queries_count": 1, "fields_count": 1,
                    "deduped_source_dist": {}, "has_reference": False,
                })

            names = [ls["name"] for ls in logstores]
            selected = _create_selected_logstores(names)

            (project_dir / "project_summary.json").write_text(
                json.dumps(_create_project_summary(logstores), ensure_ascii=False)
            )
            (project_dir / "selected_logstores.json").write_text(
                json.dumps(selected, ensure_ascii=False)
            )
            for entry in selected["logstores"]:
                _create_overview_file(project_dir, entry["output_path"])
                # Create step_progress.json with status=completed to match new logic
                logstore_dir = project_dir / entry["name"]
                logstore_dir.mkdir(exist_ok=True)
                (logstore_dir / "step_progress.json").write_text(
                    json.dumps({"status": "completed"}, ensure_ascii=False)
                )

            audit_plan = prepare_audit(str(project_dir), "sample", None)

            self.assertEqual(audit_plan["audit_logstores"][0]["name"], "high_value")
            self.assertEqual(audit_plan["audit_logstores"][1]["name"], "medium_value")
            scores = [ls["score"] for ls in audit_plan["audit_logstores"]]
            self.assertEqual(scores, sorted(scores, reverse=True))


class TestOverviewSkip(unittest.TestCase):
    """Test that logstores are skipped when overview.md is missing."""

    def test_skipped_when_overview_missing(self):
        """Logstores without overview.md are in skipped list."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "skip_project"
            project_dir.mkdir()

            names = ["logstore_a", "logstore_b"]
            logstores = [
                {"name": name, "deduped_queries_count": 10, "fields_count": 5,
                 "deduped_source_dist": {}, "has_reference": False}
                for name in names
            ]
            selected = _create_selected_logstores(names)
            # Do NOT create overview files, but DO create step_progress.json with completed status
            # to simulate completed logstores that lack overview files

            (project_dir / "project_summary.json").write_text(
                json.dumps(_create_project_summary(logstores), ensure_ascii=False)
            )
            (project_dir / "selected_logstores.json").write_text(
                json.dumps(selected, ensure_ascii=False)
            )
            # Create step_progress.json files with completed status but no overview files
            for entry in selected["logstores"]:
                logstore_dir = project_dir / entry["name"]
                logstore_dir.mkdir(exist_ok=True)
                (logstore_dir / "step_progress.json").write_text(
                    json.dumps({"status": "completed"}, ensure_ascii=False)
                )

            audit_plan = prepare_audit(str(project_dir), "full", None)

            self.assertEqual(audit_plan["audit_count"], 0)
            self.assertEqual(len(audit_plan["audit_logstores"]), 0)
            self.assertEqual(len(audit_plan.get("skipped", [])), 2)
            self.assertEqual(audit_plan["skipped"][0]["reason"], "overview.md not found")


class TestBuildAuditContext(unittest.TestCase):
    """Test audit_context.json generation."""

    def test_build_audit_context_structure(self):
        """audit_context has required fields."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "ctx_project"
            logstore_dir = project_dir / "test_ls"
            parsed_dir = logstore_dir / "parsed"
            parsed_dir.mkdir(parents=True)

            pipeline = {
                "stats": {"input": 5, "selected": 2, "extra": 1},
                "selected": [
                    {"id": "q1", "display_name": "Q1", "source_type": "dashboard",
                     "query": "x", "pre_cleaned_query": "x", "normalized_query": "x"},
                    {"id": "q2", "display_name": "Q2", "source_type": "alert",
                     "query": "y", "pre_cleaned_query": "y", "normalized_query": "y"},
                ],
                "extra": [
                    {"id": "q3", "display_name": "Q3", "source_type": "dashboard",
                     "query": "z", "pre_cleaned_query": "z", "normalized_query": "z"},
                ],
            }
            annotations = [
                {"id": "q1", "title": "T1", "category": "C1", "cleaned_query": "PRE_CLEANED"},
                {"id": "q2", "title": "T2", "category": "C1", "cleaned_query": "PRE_CLEANED"},
                {"id": "q3", "title": "T3", "category": "C2", "cleaned_query": "PRE_CLEANED"},
            ]
            selection = {"selected": ["q1", "q2"], "extra": ["q3"]}
            queries = [
                {"id": "q0", "display_name": "Q0", "source_type": "dashboard"},
                {"id": "q4", "display_name": "Q4", "source_type": "alert"},
            ]

            (parsed_dir / "query_pipeline.json").write_text(
                json.dumps(pipeline, ensure_ascii=False)
            )
            (parsed_dir / "query_annotations.json").write_text(
                json.dumps(annotations, ensure_ascii=False)
            )
            (parsed_dir / "query_selection.json").write_text(
                json.dumps(selection, ensure_ascii=False)
            )
            (parsed_dir / "queries.json").write_text(
                json.dumps(queries, ensure_ascii=False)
            )

            ctx = build_audit_context(str(project_dir), "test_ls")

            self.assertIsNotNone(ctx)
            self.assertEqual(ctx["logstore"], "test_ls")
            self.assertEqual(ctx["raw_candidates_count"], 5)
            self.assertEqual(ctx["effective_candidates_count"], 5)
            self.assertEqual(ctx["candidates_count"], 5)
            self.assertEqual(ctx["selected_count"], 2)
            self.assertEqual(ctx["extra_count"], 1)
            self.assertEqual(len(ctx["auditable_queries"]), 3)
            self.assertEqual(len(ctx["candidates_not_selected"]), 2)
            self.assertIn("C1", ctx["categories"])
            self.assertIn("C2", ctx["categories"])

            # PRE_CLEANED expansion: annotations had PRE_CLEANED, should be expanded to pre_cleaned_query
            q1_audit = next(a for a in ctx["auditable_queries"] if a["id"] == "q1")
            self.assertEqual(q1_audit["cleaned_query"], "x", "PRE_CLEANED should expand to pre_cleaned_query")

            # No validation: pipeline_summary has nulls, validation_failures empty
            ps = ctx["pipeline_summary"]
            self.assertEqual(ps["raw_candidates"], 5)
            self.assertIsNone(ps["validation_passed"])
            self.assertIsNone(ps["validation_failed"])
            self.assertEqual(ps["effective_candidates"], 5)
            self.assertEqual(ctx["validation_failures"], [])

    def test_pre_cleaned_expansion(self):
        """PRE_CLEANED sentinel in annotations is expanded to pre_cleaned_query."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "pre_cleaned_project"
            logstore_dir = project_dir / "test_ls"
            parsed_dir = logstore_dir / "parsed"
            parsed_dir.mkdir(parents=True)

            pipeline = {
                "stats": {"input": 1, "selected": 1, "extra": 0},
                "selected": [
                    {"id": "q0", "display_name": "Q0", "source_type": "alert",
                     "query": "a:b", "pre_cleaned_query": "a:<b>", "normalized_query": "a:<b>"},
                ],
                "extra": [],
            }
            annotations = [{"id": "q0", "title": "T0", "category": "C", "cleaned_query": "PRE_CLEANED"}]
            selection = {"selected": ["q0"], "extra": []}
            queries = []

            (parsed_dir / "query_pipeline.json").write_text(json.dumps(pipeline, ensure_ascii=False))
            (parsed_dir / "query_annotations.json").write_text(json.dumps(annotations, ensure_ascii=False))
            (parsed_dir / "query_selection.json").write_text(json.dumps(selection, ensure_ascii=False))
            (parsed_dir / "queries.json").write_text(json.dumps(queries, ensure_ascii=False))

            ctx = build_audit_context(str(project_dir), "test_ls")
            self.assertIsNotNone(ctx)
            q0 = ctx["auditable_queries"][0]
            self.assertEqual(q0["cleaned_query"], "a:<b>", "PRE_CLEANED must expand to pre_cleaned_query")

    def test_validation_failures_injected(self):
        """When validation fails, validation_failures and effective_candidates are correct."""
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = Path(tmp) / "validation_fail_project"
            logstore_dir = project_dir / "test_ls"
            parsed_dir = logstore_dir / "parsed"
            parsed_dir.mkdir(parents=True)

            pipeline = {
                "stats": {"input": 2, "selected": 0, "extra": 0},
                "selected": [],
                "extra": [],
                "validation": {"total": 2, "pass": 0, "fail": 2, "backfilled": 0},
            }
            annotations = []
            selection = {"selected": [], "extra": []}
            queries = []
            validation_results = [
                {"id": "q0", "pass": False, "error": "SDKError: ProjectNotExist"},
                {"id": "q1", "pass": False, "error": "Timeout"},
            ]

            (parsed_dir / "query_pipeline.json").write_text(json.dumps(pipeline, ensure_ascii=False))
            (parsed_dir / "query_annotations.json").write_text(json.dumps(annotations, ensure_ascii=False))
            (parsed_dir / "query_selection.json").write_text(json.dumps(selection, ensure_ascii=False))
            (parsed_dir / "queries.json").write_text(json.dumps(queries, ensure_ascii=False))
            (parsed_dir / "query_validation.json").write_text(
                json.dumps(validation_results, ensure_ascii=False)
            )

            ctx = build_audit_context(str(project_dir), "test_ls")
            self.assertIsNotNone(ctx)
            ps = ctx["pipeline_summary"]
            self.assertEqual(ps["raw_candidates"], 2)
            self.assertEqual(ps["validation_passed"], 0)
            self.assertEqual(ps["validation_failed"], 2)
            self.assertEqual(ps["effective_candidates"], 0)
            self.assertEqual(ctx["effective_candidates_count"], 0)
            self.assertEqual(len(ctx["validation_failures"]), 2)
            self.assertEqual(ctx["validation_failures"][0]["id"], "q0")
            self.assertIn("ProjectNotExist", ctx["validation_failures"][0]["error"])


if __name__ == "__main__":
    unittest.main()
