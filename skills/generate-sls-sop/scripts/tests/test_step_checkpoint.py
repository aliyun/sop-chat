#!/usr/bin/env python3
"""
Unit tests for step-level checkpoint functionality in update_status.py.

Tests cover:
- step_progress.json load/save
- Output file validation (STEP_OUTPUT_FILES)
- Conditional step skipping (CONDITIONAL_STEPS)
- Resume point determination (determine_resume_point)
- mark_step command

Run:
    python3 scripts/tests/test_step_checkpoint.py
"""

import json
import os
import sys
import tempfile
import unittest

# Add parent dir (scripts/) to path for imports
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from update_status import (
    STEP_OUTPUT_FILES,
    CONDITIONAL_STEPS,
    load_step_progress,
    save_step_progress,
    load_skill_options,
    validate_step_outputs,
    is_step_skipped,
    determine_resume_point,
    mark_status,
    mark_failed,
    mark_step,
    resume_check,
)


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


# ===========================================================================
# TestLoadSaveStepProgress
# ===========================================================================

class TestLoadSaveStepProgress(unittest.TestCase):
    """Tests for load_step_progress and save_step_progress."""

    def test_load_nonexistent_returns_none(self):
        """load_step_progress returns None when file doesn't exist."""
        d = _make_logstore_dir()
        result = load_step_progress(d)
        self.assertIsNone(result)

    def test_save_and_load(self):
        """save_step_progress writes JSON, load_step_progress reads it back."""
        d = _make_logstore_dir()
        data = {
            "last_completed_step": 7,
            "steps": {
                "5": {"timestamp": "2026-01-01T10:00:00Z"},
                "6": {"timestamp": "2026-01-01T10:01:00Z"},
                "7": {"timestamp": "2026-01-01T10:02:00Z"},
            }
        }
        save_step_progress(d, data)
        loaded = load_step_progress(d)
        self.assertEqual(loaded["last_completed_step"], 7)
        self.assertIn("5", loaded["steps"])

    def test_save_atomic_creates_file(self):
        """save_step_progress creates file if it doesn't exist."""
        d = _make_logstore_dir()
        path = os.path.join(d, "step_progress.json")
        self.assertFalse(os.path.exists(path))
        save_step_progress(d, {"last_completed_step": 5, "steps": {}})
        self.assertTrue(os.path.exists(path))


# ===========================================================================
# TestLoadSkillOptions
# ===========================================================================

class TestLoadSkillOptions(unittest.TestCase):
    """Tests for load_skill_options."""

    def test_load_nonexistent_returns_empty_dict(self):
        """load_skill_options returns {} when file doesn't exist."""
        d = _make_logstore_dir()
        result = load_skill_options(d)
        self.assertEqual(result, {})

    def test_load_existing_returns_content(self):
        """load_skill_options returns content when file exists."""
        d = _make_logstore_dir()
        _write_json(os.path.join(d, "skill_options.json"), {
            "reference_source": "docs/ref.md",
            "validate_queries": True,
            "output_path": "sop/overview.md",
        })
        result = load_skill_options(d)
        self.assertEqual(result["reference_source"], "docs/ref.md")
        self.assertTrue(result["validate_queries"])


# ===========================================================================
# TestIsStepSkipped
# ===========================================================================

class TestIsStepSkipped(unittest.TestCase):
    """Tests for is_step_skipped."""

    def test_non_conditional_step_not_skipped(self):
        """Non-conditional steps (5, 6, 7, 9, 10, 11) are never skipped."""
        for step in [5, 6, 7, 9, 10, 11]:
            self.assertFalse(is_step_skipped(step, {}))
            self.assertFalse(is_step_skipped(step, {"reference_source": "x"}))

    def test_step4_skipped_without_reference_source(self):
        """Step 4 is skipped when reference_source is absent or falsy."""
        self.assertTrue(is_step_skipped(4, {}))
        self.assertTrue(is_step_skipped(4, {"reference_source": ""}))
        self.assertTrue(is_step_skipped(4, {"reference_source": None}))

    def test_step4_not_skipped_with_reference_source(self):
        """Step 4 is NOT skipped when reference_source is truthy."""
        self.assertFalse(is_step_skipped(4, {"reference_source": "docs/ref.md"}))

    def test_step8_skipped_without_validate_queries(self):
        """Step 8 is skipped when validate_queries is absent or falsy."""
        self.assertTrue(is_step_skipped(8, {}))
        self.assertTrue(is_step_skipped(8, {"validate_queries": False}))

    def test_step8_not_skipped_with_validate_queries(self):
        """Step 8 is NOT skipped when validate_queries is truthy."""
        self.assertFalse(is_step_skipped(8, {"validate_queries": True}))


# ===========================================================================
# TestValidateStepOutputs
# ===========================================================================

class TestValidateStepOutputs(unittest.TestCase):
    """Tests for validate_step_outputs."""

    def test_skipped_step_always_valid(self):
        """Skipped conditional step returns True (valid)."""
        d = _make_logstore_dir()
        # Step 4 without reference_source should be skipped, hence valid
        self.assertTrue(validate_step_outputs(4, d, {}))
        # Step 8 without validate_queries should be skipped, hence valid
        self.assertTrue(validate_step_outputs(8, d, {}))

    def test_step5_invalid_when_files_missing(self):
        """Step 5 invalid when required files are missing."""
        d = _make_logstore_dir()
        self.assertFalse(validate_step_outputs(5, d, {}))

    def test_step5_valid_when_files_exist(self):
        """Step 5 valid when required files exist and are non-empty."""
        d = _make_logstore_dir()
        _write_json(os.path.join(d, "parsed", "field_annotations.json"), [{"field": "x"}])
        _write_text(os.path.join(d, "fragments", "fields_table.md"), "# Fields\n")
        self.assertTrue(validate_step_outputs(5, d, {}))

    def test_step5_invalid_when_file_empty(self):
        """Step 5 invalid when a required file is empty."""
        d = _make_logstore_dir()
        _write_json(os.path.join(d, "parsed", "field_annotations.json"), [])
        _write_text(os.path.join(d, "fragments", "fields_table.md"), "")  # empty
        self.assertFalse(validate_step_outputs(5, d, {}))

    def test_step11_valid_with_output_path(self):
        """Step 11 valid when skill_options.output_path file exists."""
        d = _make_logstore_dir()
        # output_path is relative to workspace (not logstore_dir) in production
        # In tests, we use the full path within the temp directory
        output_path = os.path.join(d, "sop", "overview.md")
        os.makedirs(os.path.dirname(output_path), exist_ok=True)
        _write_text(output_path, "# Overview\n")
        options = {"output_path": output_path}
        self.assertTrue(validate_step_outputs(11, d, options))

    def test_step11_invalid_without_output_path(self):
        """Step 11 invalid when output_path is not set."""
        d = _make_logstore_dir()
        self.assertFalse(validate_step_outputs(11, d, {}))

    def test_step11_invalid_when_output_file_missing(self):
        """Step 11 invalid when output_path file doesn't exist."""
        d = _make_logstore_dir()
        options = {"output_path": "sop/nonexistent.md"}
        self.assertFalse(validate_step_outputs(11, d, options))


# ===========================================================================
# TestDetermineResumePoint
# ===========================================================================

class TestDetermineResumePoint(unittest.TestCase):
    """Tests for determine_resume_point."""

    def test_no_progress_starts_at_step5_without_reference(self):
        """No step_progress.json + no reference_source -> resume from step 5 (skip step 4)."""
        d = _make_logstore_dir()
        # Without reference_source, step 4 is skipped
        self.assertEqual(determine_resume_point(d), 5)

    def test_no_progress_starts_at_step4_with_reference(self):
        """No step_progress.json + reference_source -> resume from step 4."""
        d = _make_logstore_dir()
        _write_json(os.path.join(d, "skill_options.json"), {"reference_source": "docs/ref.md"})
        self.assertEqual(determine_resume_point(d), 4)

    def test_no_progress_explicit_skip_step4(self):
        """No progress + empty reference_source -> resume from step 5."""
        d = _make_logstore_dir()
        _write_json(os.path.join(d, "skill_options.json"), {"reference_source": ""})
        self.assertEqual(determine_resume_point(d), 5)

    def test_resume_after_step7(self):
        """After step 7 completed -> resume from step 8."""
        d = _make_logstore_dir()
        # Create required output files for steps 5, 6, 7
        _write_json(os.path.join(d, "parsed", "field_annotations.json"), [{"x": 1}])
        _write_text(os.path.join(d, "fragments", "fields_table.md"), "# F\n")
        _write_json(os.path.join(d, "parsed", "query_selection.json"), {"selected": []})
        _write_json(os.path.join(d, "parsed", "query_pipeline.json"), {"selected": []})
        # Save progress
        save_step_progress(d, {"last_completed_step": 7, "steps": {}})
        # Should resume from step 8, but step 8 is conditional
        # Without validate_queries, step 8 is skipped -> resume from 9
        self.assertEqual(determine_resume_point(d), 9)

    def test_resume_includes_step8_when_validate_queries(self):
        """After step 7 + validate_queries=True -> resume from step 8."""
        d = _make_logstore_dir()
        # Create required output files for steps 5, 6, 7
        _write_json(os.path.join(d, "parsed", "field_annotations.json"), [{"x": 1}])
        _write_text(os.path.join(d, "fragments", "fields_table.md"), "# F\n")
        _write_json(os.path.join(d, "parsed", "query_selection.json"), {"selected": []})
        _write_json(os.path.join(d, "parsed", "query_pipeline.json"), {"selected": []})
        _write_json(os.path.join(d, "skill_options.json"), {"validate_queries": True})
        save_step_progress(d, {"last_completed_step": 7, "steps": {}})
        self.assertEqual(determine_resume_point(d), 8)

    def test_completed_returns_12(self):
        """All steps completed -> returns 12."""
        d = _make_logstore_dir()
        # Create all required output files
        _write_json(os.path.join(d, "parsed", "field_annotations.json"), [{"x": 1}])
        _write_text(os.path.join(d, "fragments", "fields_table.md"), "# F\n")
        _write_json(os.path.join(d, "parsed", "query_selection.json"), {"selected": []})
        _write_json(os.path.join(d, "parsed", "query_pipeline.json"), {"selected": []})
        _write_json(os.path.join(d, "parsed", "query_annotations.json"), [])
        _write_text(os.path.join(d, "fragments", "queries_selected.md"), "# Q\n")
        _write_text(os.path.join(d, "parsed", "query_report.md"), "# R\n")
        # output_path is relative to workspace (full path in tests)
        output_path = os.path.join(d, "sop", "overview.md")
        os.makedirs(os.path.dirname(output_path), exist_ok=True)
        _write_text(output_path, "# O\n")
        _write_json(os.path.join(d, "skill_options.json"), {"output_path": output_path})
        save_step_progress(d, {"last_completed_step": 11, "steps": {}})
        self.assertEqual(determine_resume_point(d), 12)

    def test_damaged_file_restarts_from_damaged_step(self):
        """Damaged output file -> restart from that step."""
        d = _make_logstore_dir()
        # Create output for step 5 only
        _write_json(os.path.join(d, "parsed", "field_annotations.json"), [{"x": 1}])
        _write_text(os.path.join(d, "fragments", "fields_table.md"), "# F\n")
        # Progress says step 7 completed, but step 6 files are missing
        save_step_progress(d, {"last_completed_step": 7, "steps": {}})
        # Should detect damage at step 6 and restart from there
        self.assertEqual(determine_resume_point(d), 6)


# ===========================================================================
# TestStepOutputFilesConstant
# ===========================================================================

class TestStepOutputFilesConstant(unittest.TestCase):
    """Tests for STEP_OUTPUT_FILES constant correctness."""

    def test_steps_4_to_10_defined(self):
        """Steps 4-10 should be defined in STEP_OUTPUT_FILES."""
        for step in range(4, 11):
            self.assertIn(step, STEP_OUTPUT_FILES, f"Step {step} missing")

    def test_step_11_not_in_constant(self):
        """Step 11 uses skill_options.output_path, not in STEP_OUTPUT_FILES."""
        self.assertNotIn(11, STEP_OUTPUT_FILES)


# ===========================================================================
# TestConditionalStepsConstant
# ===========================================================================

class TestConditionalStepsConstant(unittest.TestCase):
    """Tests for CONDITIONAL_STEPS constant correctness."""

    def test_step4_conditional(self):
        """Step 4 is conditional on reference_source."""
        self.assertIn(4, CONDITIONAL_STEPS)
        self.assertEqual(CONDITIONAL_STEPS[4], "reference_source")

    def test_step8_conditional(self):
        """Step 8 is conditional on validate_queries."""
        self.assertIn(8, CONDITIONAL_STEPS)
        self.assertEqual(CONDITIONAL_STEPS[8], "validate_queries")

    def test_only_two_conditional_steps(self):
        """Only steps 4 and 8 are conditional."""
        self.assertEqual(len(CONDITIONAL_STEPS), 2)


class TestNewStatusFunctionality(unittest.TestCase):
    """Tests for new status functionality using step_progress.json."""
    
    def test_mark_status_writes_to_step_progress(self):
        """mark_status should write status to step_progress.json following state machine."""
        import tempfile
        import os
        import json
        
        with tempfile.TemporaryDirectory() as tmp:
            # Create a minimal selected_logstores.json with one logstore
            project_dir = tmp
            selected_path = os.path.join(project_dir, "selected_logstores.json")
            selected_data = {
                "logstores": [{"name": "test_logstore", "logstore_dir": os.path.join(project_dir, "test_logstore")}]
            }
            with open(selected_path, "w", encoding="utf-8") as f:
                json.dump(selected_data, f, ensure_ascii=False, indent=2)
            
            # Create logstore directory
            logstore_dir = os.path.join(project_dir, "test_logstore")
            os.makedirs(logstore_dir, exist_ok=True)
            
            # Follow valid state transitions: None -> in_progress -> completed
            mark_status(project_dir, "test_logstore", "in_progress")
            mark_status(project_dir, "test_logstore", "completed")
            
            # Check that step_progress.json was created with correct status
            progress = load_step_progress(logstore_dir)
            self.assertIsNotNone(progress)
            self.assertEqual(progress["status"], "completed")
    
    def test_mark_failed_writes_to_step_progress(self):
        """mark_failed should write failed status and error details to step_progress.json."""
        import tempfile
        import os
        
        with tempfile.TemporaryDirectory() as tmp:
            # Create a minimal selected_logstores.json with one logstore
            project_dir = tmp
            selected_path = os.path.join(project_dir, "selected_logstores.json")
            selected_data = {
                "logstores": [{"name": "test_logstore", "logstore_dir": os.path.join(project_dir, "test_logstore")}]
            }
            with open(selected_path, "w", encoding="utf-8") as f:
                import json
                json.dump(selected_data, f, ensure_ascii=False, indent=2)
            
            # Create logstore directory
            logstore_dir = os.path.join(project_dir, "test_logstore")
            os.makedirs(logstore_dir, exist_ok=True)
            
            # Create errors file
            errors_file = os.path.join(tmp, "errors.json")
            with open(errors_file, "w", encoding="utf-8") as f:
                json.dump(["error1", "error2"], f, ensure_ascii=False)
            
            # Call mark_failed with numeric step (Step 6)
            mark_failed(project_dir, "test_logstore", 6, errors_file)
            
            # Check that step_progress.json was created with correct status and error details
            progress = load_step_progress(logstore_dir)
            self.assertIsNotNone(progress)
            self.assertEqual(progress["status"], "failed")
            self.assertEqual(progress["failed_step"], 6)
            self.assertEqual(progress["errors"], ["error1", "error2"])
            # Check success: false in steps dict
            self.assertIn("6", progress["steps"])
            self.assertEqual(progress["steps"]["6"]["success"], False)

    def test_mark_step_adds_success_true(self):
        """mark_step should add success: true to step entry."""
        import tempfile
        import os
        import json
        
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = tmp
            selected_path = os.path.join(project_dir, "selected_logstores.json")
            selected_data = {
                "logstores": [{"name": "test_logstore", "logstore_dir": os.path.join(project_dir, "test_logstore")}]
            }
            with open(selected_path, "w", encoding="utf-8") as f:
                json.dump(selected_data, f, ensure_ascii=False, indent=2)
            
            logstore_dir = os.path.join(project_dir, "test_logstore")
            os.makedirs(logstore_dir, exist_ok=True)
            
            # Call mark_step
            mark_step(project_dir, "test_logstore", 5)
            
            progress = load_step_progress(logstore_dir)
            self.assertIn("5", progress["steps"])
            self.assertEqual(progress["steps"]["5"]["success"], True)
            self.assertIn("timestamp", progress["steps"]["5"])

    def test_invalid_transition_failed_to_completed(self):
        """Transitioning from failed to completed should fail."""
        import tempfile
        import os
        import json
        
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = tmp
            selected_path = os.path.join(project_dir, "selected_logstores.json")
            selected_data = {
                "logstores": [{"name": "test_logstore", "logstore_dir": os.path.join(project_dir, "test_logstore")}]
            }
            with open(selected_path, "w", encoding="utf-8") as f:
                json.dump(selected_data, f, ensure_ascii=False, indent=2)
            
            logstore_dir = os.path.join(project_dir, "test_logstore")
            os.makedirs(logstore_dir, exist_ok=True)
            
            # Set initial status to failed
            progress = {"steps": {}, "status": "failed", "failed_step": 6}
            with open(os.path.join(logstore_dir, "step_progress.json"), "w") as f:
                json.dump(progress, f)
            
            # Try to transition to completed - should exit with error
            with self.assertRaises(SystemExit) as cm:
                mark_status(project_dir, "test_logstore", "completed")
            self.assertEqual(cm.exception.code, 1)

    def test_valid_transition_failed_to_in_progress(self):
        """Transitioning from failed to in_progress should succeed."""
        import tempfile
        import os
        import json
        
        with tempfile.TemporaryDirectory() as tmp:
            project_dir = tmp
            selected_path = os.path.join(project_dir, "selected_logstores.json")
            selected_data = {
                "logstores": [{"name": "test_logstore", "logstore_dir": os.path.join(project_dir, "test_logstore")}]
            }
            with open(selected_path, "w", encoding="utf-8") as f:
                json.dump(selected_data, f, ensure_ascii=False, indent=2)
            
            logstore_dir = os.path.join(project_dir, "test_logstore")
            os.makedirs(logstore_dir, exist_ok=True)
            
            # Set initial status to failed
            progress = {"steps": {}, "status": "failed", "failed_step": 6}
            with open(os.path.join(logstore_dir, "step_progress.json"), "w") as f:
                json.dump(progress, f)
            
            # Transition to in_progress - should succeed
            mark_status(project_dir, "test_logstore", "in_progress")
            
            progress = load_step_progress(logstore_dir)
            self.assertEqual(progress["status"], "in_progress")

    def test_resume_returns_negative_for_failed_status(self):
        """determine_resume_point should return -1 for failed logstore."""
        import tempfile
        import os
        import json
        
        with tempfile.TemporaryDirectory() as tmp:
            logstore_dir = tmp
            
            # Set status to failed
            progress = {"steps": {"5": {"timestamp": "...", "success": True}}, 
                        "status": "failed", "failed_step": 6, "last_completed_step": 5}
            with open(os.path.join(logstore_dir, "step_progress.json"), "w") as f:
                json.dump(progress, f)
            
            resume_point = determine_resume_point(logstore_dir)
            self.assertEqual(resume_point, -1)


class TestResumeCheck(unittest.TestCase):
    """Tests for resume_check function."""

    def _setup_project(self, logstores_config):
        """
        Helper to create project structure with manifest and logstores.
        
        logstores_config: list of dicts with keys:
            - name: logstore name
            - status: None (pending), 'in_progress', 'completed', 'failed'
            - failed_step: int (only if status='failed')
        """
        project_dir = tempfile.mkdtemp()
        manifest_data = {"logstores": []}
        
        for config in logstores_config:
            logstore_name = config["name"]
            logstore_dir = os.path.join(project_dir, logstore_name)
            os.makedirs(logstore_dir, exist_ok=True)
            
            manifest_data["logstores"].append({
                "name": logstore_name,
                "logstore_dir": logstore_dir
            })
            
            status = config.get("status")
            if status:
                progress = {"steps": {}, "status": status}
                if status == "failed":
                    progress["failed_step"] = config.get("failed_step", 6)
                    progress["errors"] = ["test error"]
                _write_json(os.path.join(logstore_dir, "step_progress.json"), progress)
        
        _write_json(os.path.join(project_dir, "selected_logstores.json"), manifest_data)
        return project_dir

    def _capture_resume_check(self, project_dir):
        """Call resume_check and capture JSON output."""
        import io
        import sys
        captured = io.StringIO()
        old_stdout = sys.stdout
        try:
            sys.stdout = captured
            resume_check(project_dir)
        finally:
            sys.stdout = old_stdout
        return json.loads(captured.getvalue())

    def test_pending_logstore_in_pending_list(self):
        """Pending logstore should appear in pending_logstores."""
        project_dir = self._setup_project([
            {"name": "ls_pending", "status": None},  # No step_progress.json = pending
        ])
        result = self._capture_resume_check(project_dir)
        self.assertIn("ls_pending", result["pending_logstores"])
        self.assertNotIn("ls_pending", result.get("in_progress_logstores", []))

    def test_in_progress_logstore_in_separate_list(self):
        """In-progress logstore should appear in in_progress_logstores, not pending_logstores."""
        project_dir = self._setup_project([
            {"name": "ls_in_progress", "status": "in_progress"},
        ])
        result = self._capture_resume_check(project_dir)
        self.assertIn("ls_in_progress", result["in_progress_logstores"])
        self.assertNotIn("ls_in_progress", result["pending_logstores"])

    def test_in_progress_not_reset_to_pending(self):
        """In-progress status should remain in_progress, not reset to pending."""
        project_dir = self._setup_project([
            {"name": "ls_in_progress", "status": "in_progress"},
        ])
        result = self._capture_resume_check(project_dir)
        
        # Check that status is still in_progress after resume_check
        logstore_dir = os.path.join(project_dir, "ls_in_progress")
        progress = load_step_progress(logstore_dir)
        self.assertEqual(progress["status"], "in_progress")
        
        # Also verify in summary
        self.assertEqual(result["summary"]["in_progress"], 1)
        self.assertEqual(result["summary"]["pending"], 0)

    def test_failed_logstores_no_errors_field(self):
        """Failed logstores entries should not contain 'errors' field."""
        project_dir = self._setup_project([
            {"name": "ls_failed", "status": "failed", "failed_step": 6},
        ])
        result = self._capture_resume_check(project_dir)
        
        self.assertEqual(len(result["failed_logstores"]), 1)
        failed_entry = result["failed_logstores"][0]
        self.assertIn("name", failed_entry)
        self.assertIn("failed_step", failed_entry)
        self.assertNotIn("errors", failed_entry)

    def test_output_no_recovered_field(self):
        """Output should not contain 'recovered' field."""
        project_dir = self._setup_project([
            {"name": "ls1", "status": "in_progress"},
            {"name": "ls2", "status": None},
        ])
        result = self._capture_resume_check(project_dir)
        self.assertNotIn("recovered", result)


if __name__ == "__main__":
    unittest.main()
