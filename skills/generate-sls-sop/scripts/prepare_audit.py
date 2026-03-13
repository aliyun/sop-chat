#!/usr/bin/env python3
"""
Prepare audit plan for Phase D audit.

Determines which logstores to audit based on project size and mode:
- full: Audit all completed logstores
- sample: Sample 20% (min 10, max 30) using high-value scoring
- targeted: Audit specific logstores specified by user

Usage:
    prepare_audit.py <project_dir> [--mode full|sample|targeted] [--logstores <ls1,ls2,...>]

Output:
    <project_dir>/_audit/audit_plan.json
    <project_dir>/_audit/<logstore>/audit_context.json  (per logstore, if not skipped)

stdout: JSON summary of audit plan
stderr: progress messages
"""

import argparse
import json
import os
import sys
from typing import Any


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


def calculate_score(logstore_info: dict) -> float:
    """
    Calculate high-value score for a logstore.
    
    Scoring formula:
    - query_score: min(queries_count / 50, 1.0) * 0.4
    - field_score: min(fields_count / 30, 1.0) * 0.3
    - alert_score: 1.0 if has alert else 0 * 0.2
    - reference_score: 1.0 if has reference else 0 * 0.1
    """
    queries_count = logstore_info.get("deduped_queries_count", 0)
    fields_count = logstore_info.get("fields_count", 0)
    source_dist = logstore_info.get("deduped_source_dist", {})
    has_alert = source_dist.get("alert", 0) > 0
    has_reference = logstore_info.get("has_reference", False)
    
    # Normalize
    query_score = min(queries_count / 50, 1.0)
    field_score = min(fields_count / 30, 1.0)
    alert_score = 1.0 if has_alert else 0
    reference_score = 1.0 if has_reference else 0
    
    # Weighted sum
    W1, W2, W3, W4 = 0.4, 0.3, 0.2, 0.1
    return W1 * query_score + W2 * field_score + W3 * alert_score + W4 * reference_score


def load_project_summary(project_dir: str) -> dict | None:
    """Load project_summary.json from project directory."""
    path = os.path.join(project_dir, "project_summary.json")
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def load_selected_logstores(project_dir: str) -> dict | None:
    """Load selected_logstores.json from project directory."""
    path = os.path.join(project_dir, "selected_logstores.json")
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def get_completed_logstores(project_dir: str, selected: dict) -> list[str]:
    """Get list of logstore names with status 'completed' from step_progress.json."""
    completed = []
    for entry in selected.get("logstores", []):
        # logstore_dir in manifest is already a full path (relative to workspace)
        logstore_dir = entry.get("logstore_dir", os.path.join(project_dir, entry["name"]))
        progress_path = os.path.join(logstore_dir, "step_progress.json")
        if os.path.exists(progress_path):
            with open(progress_path, "r", encoding="utf-8") as f:
                progress = json.load(f)
            if progress.get("status") == "completed":
                completed.append(entry["name"])
    return completed


def get_logstore_output_path(selected: dict, name: str) -> str | None:
    """Get output_path for a logstore from selected_logstores.json."""
    for entry in selected.get("logstores", []):
        if entry.get("name") == name:
            return entry.get("output_path")
    return None


def find_overview_path(project_dir: str, output_path: str) -> str | None:
    """Resolve output_path to an existing overview.md file. Returns path if found else None."""
    if not output_path or not output_path.endswith("overview.md"):
        return None
    abs_project = os.path.abspath(project_dir)
    candidates = [
        os.path.join(abs_project, output_path),
        os.path.join(os.path.dirname(abs_project), output_path),
        os.path.join(os.path.dirname(os.path.dirname(abs_project)), output_path),
    ]
    for p in candidates:
        if os.path.isfile(p):
            return p
    return None


def load_json(path: str) -> dict | list | None:
    """Load JSON file, return None if not exists or invalid."""
    if not os.path.exists(path):
        return None
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, IOError):
        return None


def get_logstore_dir_from_manifest(selected: dict, logstore_name: str, project_dir: str) -> str:
    """Get logstore_dir from manifest, with fallback to project_dir/logstore_name."""
    for entry in selected.get("logstores", []):
        if entry.get("name") == logstore_name:
            return entry.get("logstore_dir", os.path.join(project_dir, logstore_name))
    return os.path.join(project_dir, logstore_name)


def build_audit_context(project_dir: str, logstore: str, selected_manifest: dict | None = None) -> dict[str, Any] | None:
    """
    Build audit_context.json for a logstore.
    Merges query_pipeline, query_annotations, query_selection, queries.
    """
    # Get logstore_dir from manifest if available
    if selected_manifest:
        logstore_dir = get_logstore_dir_from_manifest(selected_manifest, logstore, project_dir)
    else:
        # Fallback: load manifest
        manifest = load_selected_logstores(project_dir)
        if manifest:
            logstore_dir = get_logstore_dir_from_manifest(manifest, logstore, project_dir)
        else:
            logstore_dir = os.path.join(project_dir, logstore)
    parsed_dir = os.path.join(logstore_dir, "parsed")

    pipeline = load_json(os.path.join(parsed_dir, "query_pipeline.json"))
    if not pipeline:
        return None

    annotations = load_json(os.path.join(parsed_dir, "query_annotations.json"))
    selection = load_json(os.path.join(parsed_dir, "query_selection.json"))
    queries = load_json(os.path.join(parsed_dir, "queries.json"))

    selected_ids = set(selection.get("selected", [])) if selection else set()
    extra_ids = set(selection.get("extra", [])) if selection else set()

    ann_map = {}
    if annotations:
        for a in annotations if isinstance(annotations, list) else annotations.get("annotations", []):
            if "id" in a:
                ann_map[a["id"]] = a

    selected = pipeline.get("selected", [])
    extra = pipeline.get("extra", [])
    pipeline_ids = {e["id"] for e in selected + extra}

    # candidates_not_selected: from queries.json, ids not in pipeline
    candidates_not_selected = []
    if isinstance(queries, list):
        for q in queries:
            qid = q.get("id")
            if qid and qid not in pipeline_ids:
                candidates_not_selected.append({
                    "id": qid,
                    "display_name": q.get("display_name", ""),
                    "source_type": q.get("source_type", ""),
                })

    # auditable_queries: merge pipeline + annotations
    auditable_queries = []
    for entry in selected + extra:
        eid = entry.get("id", "")
        ann = ann_map.get(eid, {})
        sel = "selected" if eid in selected_ids else "extra"
        cq = ann.get("cleaned_query", "")
        if cq == "PRE_CLEANED":
            cq = entry.get("pre_cleaned_query", entry.get("normalized_query", ""))
        auditable_queries.append({
            "id": eid,
            "selection": sel,
            "source_type": entry.get("source_type", ""),
            "display_name": entry.get("display_name", ""),
            "dashboard_name": entry.get("dashboard_name", ""),
            "query": entry.get("query", ""),
            "pre_cleaned_query": entry.get("pre_cleaned_query", entry.get("normalized_query", "")),
            "title": ann.get("title", ""),
            "category": ann.get("category", ""),
            "cleaned_query": cq,
        })

    stats = pipeline.get("stats", {})
    validation = pipeline.get("validation")
    raw_candidates = stats.get("input") or (len(candidates_not_selected) + len(auditable_queries) if (candidates_not_selected or auditable_queries) else 0)

    # pipeline_summary: raw_candidates, validation stats, effective_candidates
    if validation is not None:
        val_pass = validation.get("pass", 0)
        val_fail = validation.get("fail", 0)
        effective_candidates = raw_candidates - val_fail
        pipeline_summary = {
            "raw_candidates": raw_candidates,
            "validation_passed": val_pass,
            "validation_failed": val_fail,
            "effective_candidates": effective_candidates,
        }
    else:
        pipeline_summary = {
            "raw_candidates": raw_candidates,
            "validation_passed": None,
            "validation_failed": None,
            "effective_candidates": raw_candidates,
        }

    # validation_failures: from query_validation.json + query_validation_LLM.json
    validation_failures = []
    for val_path in ("query_validation.json", "query_validation_LLM.json"):
        val_data = load_json(os.path.join(parsed_dir, val_path))
        if isinstance(val_data, list):
            for vr in val_data:
                if not vr.get("pass", True):
                    validation_failures.append({
                        "id": vr.get("id", ""),
                        "error": (vr.get("error") or "")[:500],
                    })

    categories = sorted(set(a.get("category", "") for a in ann_map.values() if a.get("category")))

    return {
        "logstore": logstore,
        "raw_candidates_count": raw_candidates,
        "effective_candidates_count": pipeline_summary["effective_candidates"],
        "candidates_count": pipeline_summary["effective_candidates"],
        "selected_count": len(selected_ids),
        "extra_count": len(extra_ids),
        "categories": categories,
        "pipeline_summary": pipeline_summary,
        "validation_failures": validation_failures,
        "candidates_not_selected": candidates_not_selected,
        "auditable_queries": auditable_queries,
    }


def save_audit_context(project_dir: str, logstore: str, context: dict):
    """Save audit_context.json to _audit/<logstore>/."""
    audit_dir = os.path.join(project_dir, "_audit", logstore)
    os.makedirs(audit_dir, exist_ok=True)
    path = os.path.join(audit_dir, "audit_context.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(context, f, ensure_ascii=False, indent=2)
        f.write("\n")
    log(f"Saved {path}")


def prepare_audit(
    project_dir: str,
    mode: str,
    logstores_arg: str | None
) -> dict[str, Any]:
    """
    Prepare audit plan based on mode and project size.
    
    Args:
        project_dir: Path to project directory
        mode: "full", "sample", or "targeted"
        logstores_arg: Comma-separated logstore names (for targeted mode)
    
    Returns:
        Audit plan dictionary
    """
    # Load project data
    project_summary = load_project_summary(project_dir)
    if project_summary is None:
        log("ERROR: project_summary.json not found")
        sys.exit(1)
    
    selected_manifest = load_selected_logstores(project_dir)
    if selected_manifest is None:
        log("ERROR: selected_logstores.json not found")
        sys.exit(1)
    
    # Get completed logstores
    completed = get_completed_logstores(project_dir, selected_manifest)
    if not completed:
        log("WARNING: No completed logstores found")
        return {
            "mode": mode,
            "total_logstores": 0,
            "audit_logstores": [],
            "audit_count": 0,
            "reason": "No completed logstores to audit",
        }
    
    total = len(completed)
    log(f"Found {total} completed logstores")
    
    # Build logstore info map from project_summary
    logstore_info_map = {
        ls["name"]: ls
        for ls in project_summary.get("logstores", [])
    }
    
    # Determine audit scope based on mode
    if mode == "targeted":
        # Use user-specified logstores
        if not logstores_arg:
            log("ERROR: --logstores required for targeted mode")
            sys.exit(1)
        
        targeted_names = [s.strip() for s in logstores_arg.split(",") if s.strip()]
        # Validate all targeted logstores are completed
        invalid = [name for name in targeted_names if name not in completed]
        if invalid:
            log(f"WARNING: Skipping non-completed logstores: {invalid}")
        
        audit_names = [name for name in targeted_names if name in completed]
        reason = f"用户指定 {len(audit_names)} 个 logstore"
        
    elif mode == "full" or total <= 10:
        # Full audit for all completed
        audit_names = completed
        if total <= 10:
            reason = f"logstore 数量 ≤ 10，全量审计"
            mode = "full"  # Override mode
        else:
            reason = f"全量审计 {total} 个 logstore"
        
    else:
        # Sample mode: 20%, min 10, max 30
        sample_count = max(10, min(30, int(total * 0.2)))
        
        # Calculate scores for all completed logstores
        scored = []
        for name in completed:
            info = logstore_info_map.get(name, {})
            score = calculate_score(info)
            scored.append({
                "name": name,
                "score": round(score, 4),
                "info": info,
            })
        
        # Sort by score descending
        scored.sort(key=lambda x: x["score"], reverse=True)
        
        # Take top N
        audit_names = [item["name"] for item in scored[:sample_count]]
        reason = f"抽样 20%，按高价值评分排序选取 top {sample_count}"
    
    # Build audit logstores with scores; check overview.md and skip if missing
    audit_logstores = []
    skipped = []
    for rank, name in enumerate(audit_names, 1):
        info = logstore_info_map.get(name, {})
        score = calculate_score(info)

        output_path = get_logstore_output_path(selected_manifest, name)
        overview_path = find_overview_path(project_dir, output_path) if output_path else None
        if not overview_path:
            skipped.append({
                "name": name,
                "reason": "overview.md not found",
                "output_path": output_path or "(none)",
            })
            log(f"Skipping {name}: overview.md not found (output_path={output_path})")
            continue

        audit_logstores.append({
            "name": name,
            "score": round(score, 4),
            "rank": rank,
        })

    # Build audit plan
    audit_plan = {
        "mode": mode,
        "total_logstores": total,
        "audit_logstores": audit_logstores,
        "audit_count": len(audit_logstores),
        "reason": reason,
        "skipped": skipped,
        "score_weights": {
            "query": 0.4,
            "field": 0.3,
            "alert": 0.2,
            "reference": 0.1,
        },
    }

    return audit_plan


def save_audit_plan(project_dir: str, audit_plan: dict):
    """Save audit plan to _audit/audit_plan.json."""
    audit_dir = os.path.join(project_dir, "_audit")
    os.makedirs(audit_dir, exist_ok=True)

    path = os.path.join(audit_dir, "audit_plan.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(audit_plan, f, ensure_ascii=False, indent=2)
        f.write("\n")

    log(f"Audit plan saved to {path}")


def generate_audit_contexts(project_dir: str, audit_plan: dict):
    """Generate audit_context.json for each audit logstore."""
    # Load manifest once for efficiency
    selected_manifest = load_selected_logstores(project_dir)
    for item in audit_plan.get("audit_logstores", []):
        name = item.get("name")
        if not name:
            continue
        context = build_audit_context(project_dir, name, selected_manifest)
        if context:
            save_audit_context(project_dir, name, context)
        else:
            log(f"WARNING: Skipped audit_context for {name} (missing query_pipeline.json)")


def main():
    parser = argparse.ArgumentParser(
        description="Prepare audit plan for Phase D audit"
    )
    parser.add_argument("project_dir", help="Project input directory")
    parser.add_argument(
        "--mode",
        choices=["full", "sample", "targeted"],
        default="sample",
        help="Audit mode: full (all), sample (20%%), targeted (user-specified)"
    )
    parser.add_argument(
        "--logstores",
        metavar="LIST",
        help="Comma-separated logstore names (for targeted mode)"
    )
    
    args = parser.parse_args()
    
    # Prepare audit plan
    audit_plan = prepare_audit(args.project_dir, args.mode, args.logstores)

    # Save audit plan
    save_audit_plan(args.project_dir, audit_plan)

    # Generate audit_context.json for each audit logstore
    generate_audit_contexts(args.project_dir, audit_plan)

    # Output JSON to stdout
    print(json.dumps(audit_plan, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
