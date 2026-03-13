#!/usr/bin/env python3
"""
Apply validation results to query_pipeline.json.

Removes failed queries, backfills selected from extra (FIFO),
and updates stats/validation metadata.

Usage:
    python apply_validation.py <input_dir>

Input files (read from <input_dir>/parsed/):
    - query_pipeline.json (required)
    - query_validation.json (required — with pass/error fields from validate_queries.py)
    - query_validation_LLM.json (optional — LLM-processed queries, also with pass/error)

Output files (written to <input_dir>/parsed/):
    - query_pipeline.json (updated in place)

Exit codes:
    0 — success
    1 — error
"""

import json
import os
import sys


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


def load_json(path: str):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_json(data, path: str):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def main():
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <input_dir>", file=sys.stderr)
        sys.exit(1)

    input_dir = sys.argv[1]
    parsed_dir = os.path.join(input_dir, "parsed")

    pipeline_path = os.path.join(parsed_dir, "query_pipeline.json")
    main_val_path = os.path.join(parsed_dir, "query_validation.json")
    llm_val_path = os.path.join(parsed_dir, "query_validation_LLM.json")

    if not os.path.exists(pipeline_path):
        log(f"ERROR: {pipeline_path} not found")
        sys.exit(1)
    if not os.path.exists(main_val_path):
        log(f"ERROR: {main_val_path} not found")
        sys.exit(1)

    pipeline = load_json(pipeline_path)

    validation_results = load_json(main_val_path)
    if os.path.exists(llm_val_path):
        validation_results += load_json(llm_val_path)

    # --- Build set of failed query IDs ---
    failed_ids = set()
    for vr in validation_results:
        if not vr.get("pass", True):
            failed_ids.add(vr.get("id", ""))

    total_validated = len(validation_results)
    total_pass = total_validated - len(failed_ids)

    if not failed_ids:
        log(f"All {total_validated} queries passed validation, no changes needed")
        # Still add validation metadata
        pipeline["validation"] = {
            "total": total_validated,
            "pass": total_validated,
            "fail": 0,
            "backfilled": 0,
        }
        save_json(pipeline, pipeline_path)
        return

    # --- Remove failed queries ---
    def is_failed(entry: dict) -> bool:
        """Check if entry failed validation by id."""
        entry_id = entry.get("id", "")
        return entry_id != "" and entry_id in failed_ids

    selected = pipeline.get("selected", [])
    extra = pipeline.get("extra", [])

    # Filter selected, track how many were removed
    new_selected = []
    selected_removed = 0
    for entry in selected:
        if is_failed(entry):
            selected_removed += 1
        else:
            new_selected.append(entry)

    # Filter extra
    new_extra = []
    extra_removed = 0
    for entry in extra:
        if is_failed(entry):
            extra_removed += 1
        else:
            new_extra.append(entry)

    total_fail = selected_removed + extra_removed
    log(f"Validation: {total_fail} failed (selected: {selected_removed}, extra: {extra_removed})")

    # --- Backfill selected from extra (FIFO: preserves LLM's semantic ranking) ---
    slots_to_fill = selected_removed
    backfilled = 0
    while slots_to_fill > 0 and new_extra:
        moved = new_extra.pop(0)
        moved["backfilled"] = True
        new_selected.append(moved)
        backfilled += 1
        slots_to_fill -= 1

    log(f"Backfilled {backfilled} entries from extra to selected")

    # --- Update pipeline ---
    pipeline["selected"] = new_selected
    pipeline["extra"] = new_extra
    pipeline["stats"]["selected"] = len(new_selected)
    pipeline["stats"]["extra"] = len(new_extra)
    # stats.input stays unchanged

    pipeline["validation"] = {
        "total": total_validated,
        "pass": total_validated - total_fail,
        "fail": total_fail,
        "backfilled": backfilled,
    }

    save_json(pipeline, pipeline_path)
    log(f"Updated query_pipeline.json: selected={len(new_selected)}, extra={len(new_extra)}")


if __name__ == "__main__":
    main()
