#!/usr/bin/env python3
"""
Manage per-logstore processing status in step_progress.json.

Provides commands for the resume/recovery workflow:

  --mark-in-progress <logstore>   Set status to "in_progress"
  --mark-completed <logstore>     Set status to "completed"
  --mark-failed <logstore>        Set status to "failed" (requires --step)
  --resume-check                  Recover interrupted runs + recommend next action

Provides commands for step-level checkpoint workflow (Phase B Steps 4-11):

  --step-resume-check <logstore>  Detect resume point for a single logstore
  --mark-step <logstore> --step N Mark step N as completed

Provides commands for Phase D audit workflow:

  --audit-check                       Check audit status and recommend next action
  --mark-audit-in-progress <logstore> Set audit_status to "in_progress"
  --mark-audited <logstore>           Set audit_status to "audited"
  --mark-audit-completed              Mark entire project audit as completed

Status state machine: pending -> in_progress -> completed | failed
Audit state machine: (none) -> in_progress -> audited

The --mark-failed command additionally accepts:
  --step <N>                      Which step failed (4-11)
  --errors-file <path>            Path to JSON array of error strings (written by validate_step.py)

The --resume-check command performs two operations in one call:
  1. Summary: count logstores by status (pending, in_progress, completed, failed)
  2. Action:  recommend next step (first_run / resume_phase_b / all_completed)

The --step-resume-check command:
  1. Loads step_progress.json from the logstore directory
  2. Validates output files for completed steps
  3. Returns {"resume_from": N} where N is the step to continue from (-1=failed, 4-12)

The --audit-check command performs similar operations for audit workflow:
  1. Recover: reset all audit "in_progress" back to "pending"
  2. Summary: count logstores by audit_status
  3. Action:  recommend next step (audit_not_started / resume_audit / audit_completed)

Usage:
    update_status.py <project_dir> --mark-in-progress <logstore>
    update_status.py <project_dir> --mark-completed <logstore>
    update_status.py <project_dir> --mark-failed <logstore> --step <N> [--errors-file <path>]
    update_status.py <project_dir> --resume-check
    update_status.py <project_dir> --step-resume-check <logstore>
    update_status.py <project_dir> --mark-step <logstore> --step <N>
    update_status.py <project_dir> --audit-check
    update_status.py <project_dir> --mark-audit-in-progress <logstore>
    update_status.py <project_dir> --mark-audited <logstore>
    update_status.py <project_dir> --mark-audit-completed

stdout: JSON result (for --resume-check, --step-resume-check, --audit-check)
stderr: progress messages
"""

import argparse
import json
import os
import sys
import tempfile
from datetime import datetime, timezone


MANIFEST_NAME = "selected_logstores.json"
STEP_PROGRESS_NAME = "step_progress.json"

# Step output files (relative to <logstore_dir>)
STEP_OUTPUT_FILES = {
    4: ["parsed/reference_queries.json"],
    5: ["parsed/field_annotations.json", "fragments/fields_table.md"],
    6: ["parsed/query_selection.json", "parsed/query_pipeline.json"],
    7: ["parsed/query_pipeline.json"],
    8: ["parsed/query_validation.json"],
    9: ["parsed/query_annotations.json"],
    10: ["fragments/queries_selected.md", "parsed/query_report.md"],
    # 11: special handling via skill_options.output_path
}

# Conditional steps (skip if skill_options.json field is falsy)
CONDITIONAL_STEPS = {4: "reference_source", 8: "validate_queries"}

# Valid state transitions for logstore status
VALID_TRANSITIONS = {
    None: {"in_progress"},
    "pending": {"in_progress"},
    "in_progress": {"completed", "failed"},
    "failed": {"in_progress"},  # Can only restart, not directly complete
    "completed": set(),  # Terminal state, no transitions allowed
}


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


def load_manifest(project_dir: str) -> dict | None:
    path = os.path.join(project_dir, MANIFEST_NAME)
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_manifest(project_dir: str, data: dict):
    """Atomic write: write to temp file then rename."""
    path = os.path.join(project_dir, MANIFEST_NAME)
    dir_name = os.path.dirname(path) or "."
    fd, tmp_path = tempfile.mkstemp(dir=dir_name, suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
            f.write("\n")
        os.replace(tmp_path, path)
    except Exception:
        os.unlink(tmp_path)
        raise


def find_logstore(manifest: dict, name: str) -> dict | None:
    for entry in manifest.get("logstores", []):
        if entry.get("name") == name:
            return entry
    return None


def get_logstore_dir(project_dir: str, logstore_name: str) -> str:
    """Get logstore directory path from manifest (with fallback)."""
    manifest = load_manifest(project_dir)
    if manifest is None:
        log(f"ERROR: {MANIFEST_NAME} not found in {project_dir}")
        sys.exit(1)
    entry = find_logstore(manifest, logstore_name)
    if entry is None:
        log(f"ERROR: logstore '{logstore_name}' not found in {MANIFEST_NAME}")
        sys.exit(1)
    # logstore_dir in manifest is already a full path (relative to workspace)
    return entry.get("logstore_dir", os.path.join(project_dir, logstore_name))


def load_errors(errors_file: str | None) -> list:
    """Load errors from JSON file."""
    if errors_file and os.path.exists(errors_file):
        with open(errors_file, "r", encoding="utf-8") as f:
            return json.load(f)
    return []


def mark_status(project_dir: str, logstore_name: str, status: str):
    """Set logstore status in step_progress.json."""
    logstore_dir = get_logstore_dir(project_dir, logstore_name)
    progress = load_step_progress(logstore_dir) or {"steps": {}}
    current = progress.get("status")

    # Validate state transition
    allowed = VALID_TRANSITIONS.get(current, set())
    if status not in allowed:
        log(f"ERROR: Invalid transition {current} -> {status}")
        log(f"       Allowed transitions: {allowed or 'none'}")
        sys.exit(1)

    progress["status"] = status
    # Clear failed-specific fields when transitioning to non-failed status
    if status != "failed":
        progress.pop("failed_step", None)
        progress.pop("errors", None)
    save_step_progress(logstore_dir, progress)
    log(f"{logstore_name}: status -> {status}")
    if status == "in_progress":
        log("Phase B: 单任务内仅处理此 logstore，禁止交替执行。")


def mark_failed(project_dir: str, logstore_name: str, step: int,
                errors_file: str | None):
    """Set logstore status to 'failed' with step and error details in step_progress.json."""
    logstore_dir = get_logstore_dir(project_dir, logstore_name)
    progress = load_step_progress(logstore_dir) or {"steps": {}}
    progress["status"] = "failed"
    progress["failed_step"] = step
    progress["errors"] = load_errors(errors_file)
    # Record failure in steps dict
    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    progress["steps"][str(step)] = {"timestamp": timestamp, "success": False}
    save_step_progress(logstore_dir, progress)
    log(f"{logstore_name}: status -> failed (step: {step}, errors: {len(progress['errors'])})")


def resume_check(project_dir: str):
    """Check status by aggregating from each logstore's step_progress.json."""
    manifest = load_manifest(project_dir)

    # No manifest -> first_run
    if manifest is None:
        result = {
            "action": "first_run",
            "summary": {},
            "pending_logstores": [],
            "in_progress_logstores": [],
            "failed_logstores": [],
        }
        print(json.dumps(result, ensure_ascii=False))
        return

    logstores = manifest.get("logstores", [])
    counts = {"pending": 0, "in_progress": 0, "completed": 0, "failed": 0}
    pending_list = []
    in_progress_list = []
    failed_list = []

    for entry in logstores:
        # logstore_dir in manifest is already a full path (relative to workspace)
        logstore_dir = entry.get("logstore_dir", os.path.join(project_dir, entry["name"]))
        progress = load_step_progress(logstore_dir)

        if progress is None:
            status = "pending"
        else:
            status = progress.get("status", "pending")

        counts[status] = counts.get(status, 0) + 1
        if status == "pending":
            pending_list.append(entry["name"])
        elif status == "in_progress":
            in_progress_list.append(entry["name"])
        elif status == "failed":
            failed_list.append({
                "name": entry["name"],
                "failed_step": progress.get("failed_step", "") if progress else "",
            })

    total = len(logstores)

    # Action
    if counts["pending"] > 0:
        action = "resume_phase_b"
    elif counts["completed"] + counts["failed"] == total:
        action = "all_completed"
    else:
        action = "resume_phase_b"

    result = {
        "action": action,
        "summary": {"total": total, **counts},
        "pending_logstores": pending_list,
        "in_progress_logstores": in_progress_list,
        "failed_logstores": failed_list,
    }
    print(json.dumps(result, ensure_ascii=False))


# ============== Step-level checkpoint functions ==============

def load_step_progress(logstore_dir: str) -> dict | None:
    """Load step_progress.json from logstore directory."""
    path = os.path.join(logstore_dir, STEP_PROGRESS_NAME)
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_step_progress(logstore_dir: str, data: dict):
    """Atomic write step_progress.json to logstore directory."""
    path = os.path.join(logstore_dir, STEP_PROGRESS_NAME)
    dir_name = os.path.dirname(path) or "."
    fd, tmp_path = tempfile.mkstemp(dir=dir_name, suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
            f.write("\n")
        os.replace(tmp_path, path)
    except Exception:
        os.unlink(tmp_path)
        raise


def load_skill_options(logstore_dir: str) -> dict:
    """Load skill_options.json from logstore directory."""
    path = os.path.join(logstore_dir, "skill_options.json")
    if not os.path.exists(path):
        return {}
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def validate_step_outputs(step: int, logstore_dir: str, options: dict) -> bool:
    """
    Check if output files for a step exist and are non-empty.
    Returns True if step is valid (or should be skipped).
    """
    # Check if this is a conditional step that should be skipped
    if step in CONDITIONAL_STEPS:
        condition_field = CONDITIONAL_STEPS[step]
        if not options.get(condition_field):
            return True  # Skipped step is considered valid

    # Step 11: special handling via skill_options.output_path
    if step == 11:
        output_path = options.get("output_path")
        if not output_path:
            return False
        # output_path is already a full path relative to workspace, don't join with logstore_dir
        return os.path.exists(output_path) and os.path.getsize(output_path) > 0

    # Step 10: special handling for conditional output
    # query_report.md is always required, queries_selected.md is conditional
    if step == 10:
        report_path = os.path.join(logstore_dir, "parsed", "query_report.md")
        if not os.path.exists(report_path) or os.path.getsize(report_path) == 0:
            return False
        # queries_selected.md is conditional (only when selected/extra non-empty)
        # If query_report.md exists, step 10 is considered complete
        return True

    # Normal steps: check files in STEP_OUTPUT_FILES
    files = STEP_OUTPUT_FILES.get(step, [])
    for rel_path in files:
        full_path = os.path.join(logstore_dir, rel_path)
        if not os.path.exists(full_path) or os.path.getsize(full_path) == 0:
            return False
    return True


def is_step_skipped(step: int, options: dict) -> bool:
    """Check if a step should be skipped based on skill_options."""
    if step in CONDITIONAL_STEPS:
        condition_field = CONDITIONAL_STEPS[step]
        return not options.get(condition_field)
    return False


def determine_resume_point(logstore_dir: str) -> int:
    """
    Determine which step to resume from.
    Returns: -1 = failed (needs manual intervention);
             4-11 = resume from that step;
             12 = all completed.
    """
    progress = load_step_progress(logstore_dir)
    options = load_skill_options(logstore_dir)

    if progress is None:
        # No progress file, start from beginning
        # But skip Step 4 if no reference_source
        if is_step_skipped(4, options):
            return 5
        return 4

    # Check for terminal states
    status = progress.get("status")
    if status == "completed":
        return 12
    if status == "failed":
        failed_step = progress.get("failed_step", "unknown")
        log(f"WARNING: Logstore is in FAILED state (step: {failed_step})")
        log(f"         Use --mark-in-progress to reset and retry")
        return -1

    last = progress.get("last_completed_step")
    if last is None:
        if is_step_skipped(4, options):
            return 5
        return 4

    # Validate output files for all completed steps
    for step in range(4, last + 1):
        if is_step_skipped(step, options):
            continue
        if not validate_step_outputs(step, logstore_dir, options):
            return step  # Restart from damaged step

    # Calculate next step
    next_step = last + 1

    # Handle conditional steps
    if next_step == 4 and is_step_skipped(4, options):
        next_step = 5
    if next_step == 8 and is_step_skipped(8, options):
        next_step = 9

    return min(next_step, 12)  # 12 = all completed


def step_resume_check(project_dir: str, logstore_name: str):
    """Check step-level resume point for a single logstore."""
    logstore_dir = get_logstore_dir(project_dir, logstore_name)
    if not os.path.isdir(logstore_dir):
        log(f"ERROR: logstore directory '{logstore_dir}' not found")
        sys.exit(1)

    resume_from = determine_resume_point(logstore_dir)
    result = {"resume_from": resume_from}
    print(json.dumps(result, ensure_ascii=False))


def mark_step(project_dir: str, logstore_name: str, step: int):
    """Mark a step as completed for a logstore."""
    logstore_dir = get_logstore_dir(project_dir, logstore_name)
    if not os.path.isdir(logstore_dir):
        log(f"ERROR: logstore directory '{logstore_dir}' not found")
        sys.exit(1)

    if step < 4 or step > 11:
        log(f"ERROR: step must be between 4 and 11, got {step}")
        sys.exit(1)

    # Load existing progress or create new
    progress = load_step_progress(logstore_dir) or {"steps": {}}

    # Update progress
    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    progress["last_completed_step"] = step
    progress["steps"][str(step)] = {"timestamp": timestamp, "success": True}

    save_step_progress(logstore_dir, progress)
    log(f"{logstore_name}: step {step} marked as completed")


# ============== Audit-related functions ==============

def mark_audit_status(project_dir: str, logstore_name: str, status: str):
    """Set audit_status in step_progress.json for a specific logstore."""
    logstore_dir = get_logstore_dir(project_dir, logstore_name)
    progress = load_step_progress(logstore_dir) or {"steps": {}}
    progress["audit_status"] = status
    save_step_progress(logstore_dir, progress)
    log(f"{logstore_name}: audit_status -> {status}")
    if status == "in_progress":
        log("Phase D: 单任务内仅处理此 logstore，禁止交替执行。")


def mark_audit_completed(project_dir: str):
    """Mark the entire project audit as completed."""
    manifest = load_manifest(project_dir)
    if manifest is None:
        log(f"ERROR: {MANIFEST_NAME} not found in {project_dir}")
        sys.exit(1)

    manifest["audit_completed"] = True
    save_manifest(project_dir, manifest)
    log("Project audit marked as completed")


def audit_check(project_dir: str):
    """Check audit status by aggregating from each logstore's step_progress.json."""
    manifest = load_manifest(project_dir)

    # No manifest -> audit cannot start
    if manifest is None:
        result = {
            "action": "no_manifest",
            "recovered": 0,
            "summary": {},
            "next_logstores": [],
        }
        print(json.dumps(result, ensure_ascii=False))
        return

    # Check if audit_plan.json exists (created by prepare_audit.py)
    audit_plan_path = os.path.join(project_dir, "_audit", "audit_plan.json")
    if not os.path.exists(audit_plan_path):
        result = {
            "action": "audit_not_started",
            "recovered": 0,
            "summary": {},
            "next_logstores": [],
            "message": "Run prepare_audit.py first to create audit plan",
        }
        print(json.dumps(result, ensure_ascii=False))
        return

    # Load audit plan to get the list of logstores to audit
    with open(audit_plan_path, "r", encoding="utf-8") as f:
        audit_plan = json.load(f)

    audit_logstore_names = [ls["name"] if isinstance(ls, dict) else ls
                           for ls in audit_plan.get("audit_logstores", [])]

    logstores = manifest.get("logstores", [])

    # Summary: count audit statuses for logstores in audit plan (from step_progress.json)
    counts = {"pending": 0, "in_progress": 0, "audited": 0}
    pending_list = []
    recovered = 0

    for entry in logstores:
        name = entry.get("name")
        if name not in audit_logstore_names:
            continue

        # logstore_dir in manifest is already a full path (relative to workspace)
        logstore_dir = entry.get("logstore_dir", os.path.join(project_dir, name))
        progress = load_step_progress(logstore_dir)

        if progress is None:
            audit_status = None
        else:
            audit_status = progress.get("audit_status")

        # Recover: audit in_progress -> reset (remove audit_status)
        if audit_status == "in_progress":
            if progress is None:
                progress = {"steps": {}}
            progress.pop("audit_status", None)
            save_step_progress(logstore_dir, progress)
            audit_status = None
            recovered += 1

        if audit_status is None:
            counts["pending"] += 1
            pending_list.append(name)
        else:
            counts[audit_status] = counts.get(audit_status, 0) + 1

    if recovered > 0:
        log(f"Recovered {recovered} audit in_progress -> (reset)")

    total = len(audit_logstore_names)

    # Action
    if manifest.get("audit_completed"):
        action = "audit_completed"
    elif counts["pending"] > 0:
        action = "resume_audit"
    elif counts["audited"] == total:
        action = "audit_ready_to_complete"
    else:
        action = "resume_audit"

    result = {
        "action": action,
        "recovered": recovered,
        "summary": {"total": total, **counts},
        "next_logstores": pending_list,
        "audit_plan": audit_plan,
    }
    print(json.dumps(result, ensure_ascii=False))


def main():
    parser = argparse.ArgumentParser(
        description="Manage per-logstore status in step_progress.json"
    )
    parser.add_argument("project_dir", help="Project input directory")

    group = parser.add_mutually_exclusive_group(required=True)
    # Phase B status commands
    group.add_argument("--mark-in-progress", metavar="LOGSTORE",
                       help="Set logstore status to in_progress")
    group.add_argument("--mark-completed", metavar="LOGSTORE",
                       help="Set logstore status to completed")
    group.add_argument("--mark-failed", metavar="LOGSTORE",
                       help="Set logstore status to failed (requires --failed-step)")
    group.add_argument("--resume-check", action="store_true",
                       help="Recover interrupted runs and recommend next action")
    # Step-level checkpoint commands
    group.add_argument("--step-resume-check", metavar="LOGSTORE",
                       help="Detect resume point for a single logstore")
    group.add_argument("--mark-step", metavar="LOGSTORE",
                       help="Mark a step as completed (requires --step)")
    # Phase D audit commands
    group.add_argument("--audit-check", action="store_true",
                       help="Check audit status and recommend next action")
    group.add_argument("--mark-audit-in-progress", metavar="LOGSTORE",
                       help="Set logstore audit_status to in_progress")
    group.add_argument("--mark-audited", metavar="LOGSTORE",
                       help="Set logstore audit_status to audited")
    group.add_argument("--mark-audit-completed", action="store_true",
                       help="Mark entire project audit as completed")

    parser.add_argument("--failed-step", metavar="STEP",
                        help="[DEPRECATED] Use --step instead")
    parser.add_argument("--errors-file", metavar="PATH",
                        help="Path to JSON error array from validate_step.py (used with --mark-failed)")
    parser.add_argument("--step", type=int, metavar="N",
                        help="Step number 4-11 (used with --mark-step or --mark-failed)")

    args = parser.parse_args()

    if args.mark_in_progress:
        mark_status(args.project_dir, args.mark_in_progress, "in_progress")
    elif args.mark_completed:
        mark_status(args.project_dir, args.mark_completed, "completed")
    elif args.mark_failed:
        if args.step is None:
            parser.error("--mark-failed requires --step")
        mark_failed(args.project_dir, args.mark_failed, args.step,
                     args.errors_file)
    elif args.resume_check:
        resume_check(args.project_dir)
    # Step-level checkpoint commands
    elif args.step_resume_check:
        step_resume_check(args.project_dir, args.step_resume_check)
    elif args.mark_step:
        if args.step is None:
            parser.error("--mark-step requires --step")
        mark_step(args.project_dir, args.mark_step, args.step)
    # Audit commands
    elif args.audit_check:
        audit_check(args.project_dir)
    elif args.mark_audit_in_progress:
        mark_audit_status(args.project_dir, args.mark_audit_in_progress, "in_progress")
    elif args.mark_audited:
        mark_audit_status(args.project_dir, args.mark_audited, "audited")
    elif args.mark_audit_completed:
        mark_audit_completed(args.project_dir)


if __name__ == "__main__":
    main()
